// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"math"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/modbus"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func (r *RTU) applyPoint(i int, regs []uint16) {
	p, s := r.pm.Points[i], &r.points[i]
	base := r.pm.Devices[p.DeviceIndex].PollStart
	address := p.Address - base
	s.valueInvalid = false
	switch p.Format {
	case pointmap.DP:
		s.dp = uint8(regs[address] & 3)
	case pointmap.Bit:
		s.sp = (regs[address]>>p.Bit)&1 != 0
	case pointmap.F32:
		s.f = modbus.RegistersToFloat(regs[address], regs[address+1])
	case pointmap.U16:
		s.f = float32(float64(regs[address]) * p.Scale)
	case pointmap.I16:
		s.f = float32(float64(int16(regs[address])) * p.Scale)
	case pointmap.Step:
		value := int(int16(regs[address]))
		s.valueInvalid = value < -64 || value > 63
		if !s.valueInvalid {
			s.step = value
		}
		s.transient = (regs[p.TransientAddress-base]>>p.TransientBit)&1 != 0
	}
	if p.Kind == pointmap.Float && (math.IsNaN(float64(s.f)) || math.IsInf(float64(s.f), 0)) {
		s.valueInvalid, s.f = true, 0
	}
}

func (r *RTU) pollDevice(d int, master Master) modbus.Status {
	dev := r.pm.Devices[d]
	started := r.opts.Now()
	var regs []uint16
	var result modbus.Result
	if dev.PollTable == pointmap.Input {
		regs, result = master.ReadInput(dev.PollStart, dev.PollCount, millis(dev.TimeoutMS))
	} else {
		regs, result = master.ReadHolding(dev.PollStart, dev.PollCount, millis(dev.TimeoutMS))
	}
	if result.Status != modbus.OK {
		if result.Status == modbus.Timeout || result.Status == modbus.IOError {
			master.Close()
		}
		return result.Status
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := &r.devices[d]
	s.lastOK, s.pollStart = r.opts.Now(), started
	s.pollSeq++
	if dev.HasHeartbeat {
		hb := regs[dev.HeartbeatAddress-dev.PollStart]
		if s.heartbeatChanged == 0 || hb != s.heartbeat {
			s.heartbeat, s.heartbeatChanged = hb, s.lastOK
		}
	}
	valid := true
	if dev.HasMeasurementsValid {
		valid = (regs[dev.MVAddress-dev.PollStart]>>dev.MVBit)&1 != 0
	}
	for i, p := range r.pm.Points {
		if p.DeviceIndex == d {
			r.applyPoint(i, regs)
			if p.Measurement {
				r.points[i].measurementInvalid = !valid
			}
		}
	}
	return modbus.OK
}

func (r *RTU) deviceWorker(ctx context.Context, d int) {
	dev := r.pm.Devices[d]
	master := r.opts.NewMaster(byte(dev.Unit))
	defer master.Close()
	var nextPoll, nextConnect int64
	resolved := false
	initialBackoff := min(r.pm.ReconnectIntervalMS, 250)
	backoff := initialBackoff
	for ctx.Err() == nil {
		now := r.opts.Now()
		if !master.IsConnected() && now >= nextConnect {
			st := modbus.OK
			if !resolved {
				st = master.Resolve(ctx, dev.Host, dev.Port)
				resolved = st == modbus.OK
			}
			if st == modbus.OK {
				st = master.Connect(250 * time.Millisecond)
			}
			if st == modbus.OK {
				backoff = initialBackoff
			} else {
				nextConnect = r.opts.Now() + int64(backoff)
				if backoff < r.pm.ReconnectIntervalMS/2 {
					backoff *= 2
				} else {
					backoff = r.pm.ReconnectIntervalMS
				}
			}
		}
		if master.IsConnected() {
			r.writeCommand(d, master)
			if r.opts.Now() >= nextPoll {
				start := r.opts.Now()
				if r.pollDevice(d, master) != modbus.OK {
					nextConnect = r.opts.Now() + int64(backoff)
				}
				nextPoll = start + int64(dev.PollMS)
			}
		}
		if !pause(ctx, 10*time.Millisecond) {
			return
		}
	}
}
