// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"math"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/modbus"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

type fakeMaster struct {
	regs           []uint16
	result         modbus.Result
	writes, closes int
	writeAddress   int
	writeValue     uint16
	onWrite        func()
	holding        bool
}

func (*fakeMaster) Resolve(context.Context, string, int) modbus.Status { return modbus.OK }
func (*fakeMaster) Connect(time.Duration) modbus.Status                { return modbus.OK }
func (*fakeMaster) IsConnected() bool                                  { return true }
func (m *fakeMaster) Close()                                           { m.closes++ }
func (m *fakeMaster) ReadInput(int, int, time.Duration) ([]uint16, modbus.Result) {
	return m.regs, m.result
}
func (m *fakeMaster) ReadHolding(a, n int, d time.Duration) ([]uint16, modbus.Result) {
	m.holding = true
	return m.ReadInput(a, n, d)
}
func (m *fakeMaster) WriteSingle(a int, v uint16, _ time.Duration) modbus.Result {
	m.writes++
	m.writeAddress, m.writeValue = a, v
	if m.onWrite != nil {
		m.onWrite()
	}
	return m.result
}
func TestRegisterMapping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		format  pointmap.Format
		kind    pointmap.Kind
		regs    []uint16
		want    float32
		invalid bool
	}{
		{"dp", pointmap.DP, pointmap.DoublePoint, []uint16{6}, 2, false},
		{"bit", pointmap.Bit, pointmap.SinglePoint, []uint16{8}, 1, false},
		{"f32", pointmap.F32, pointmap.Float, []uint16{0x4148, 0}, 12.5, false},
		{"nan", pointmap.F32, pointmap.Float, []uint16{0x7fc0, 0}, 0, true},
		{"inf", pointmap.F32, pointmap.Float, []uint16{0x7f80, 0}, 0, true},
		{"u16", pointmap.U16, pointmap.Float, []uint16{65535}, 32767.5, false},
		{"i16", pointmap.I16, pointmap.Float, []uint16{65534}, -1, false},
		{"step low", pointmap.Step, pointmap.StepPosition, []uint16{65472, 8}, -64, false},
		{"step high", pointmap.Step, pointmap.StepPosition, []uint16{63, 8}, 63, false},
		{"step invalid", pointmap.Step, pointmap.StepPosition, []uint16{64, 8}, 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := newModel(t, 1)
			r.pm.Points[0] = pointmap.Point{Kind: tc.kind, Format: tc.format, Bit: 3, Scale: .5, TransientAddress: 1, TransientBit: 3}
			r.points[0].step = 5
			r.applyPoint(0, tc.regs)
			s := r.points[0]
			v := s.f
			switch tc.kind {
			case pointmap.DoublePoint:
				v = float32(s.dp)
			case pointmap.SinglePoint:
				v = float32(boolInt(s.sp))
			case pointmap.StepPosition:
				v = float32(s.step)
				if !s.transient {
					t.Fatal("lost transient")
				}
			}
			if v != tc.want || s.valueInvalid != tc.invalid {
				t.Fatalf("value=%g invalid=%t", v, s.valueInvalid)
			}
		})
	}
	// A finite scale can overflow the float32 result and must also set IV.
	r, _, _ := newModel(t, 1)
	r.pm.Points[0].Format = pointmap.U16
	r.pm.Points[0].Scale = math.MaxFloat64
	r.applyPoint(0, []uint16{65535})
	if !r.points[0].valueInvalid || r.points[0].f != 0 {
		t.Fatal("scaled overflow leaked")
	}
}
func TestPollAndQualityBoundaries(t *testing.T) {
	r, now, c := newModel(t, 2)
	r.pm.Points[0].Format = pointmap.U16
	r.pm.Points[0].Scale = 1
	r.pm.Points[1].Format = pointmap.U16
	r.pm.Points[1].Scale = 1
	r.pm.Points[1].Measurement = false
	d := &r.pm.Devices[0]
	d.PollTable = pointmap.Holding
	d.HasHeartbeat = true
	d.HeartbeatAddress = 1
	d.HeartbeatStaleMS = 1000
	d.HasMeasurementsValid = true
	d.MVAddress = 2
	m := &fakeMaster{regs: []uint16{12, 1, 0}}
	if st := r.pollDevice(0, m); st != modbus.OK || !m.holding {
		t.Fatal(st)
	}
	r.evaluate(*now)
	if pointQuality(r.points[0], r.pm.Points[0]) != iec.Invalid || pointQuality(r.points[1], r.pm.Points[1]) != 0 {
		t.Fatal("MV affected status/nonmeasurement")
	}
	m.regs[2] = 1
	*now += 999
	r.pollDevice(0, m)
	r.evaluate(*now)
	if pointQuality(r.points[0], r.pm.Points[0]) != 0 {
		t.Fatal("early heartbeat expiry")
	}
	*now++
	r.evaluate(*now)
	if pointQuality(r.points[1], r.pm.Points[1]) != iec.Invalid {
		t.Fatal("heartbeat boundary not inclusive")
	}
	m.regs[1]++
	r.pollDevice(0, m)
	r.evaluate(*now)
	if pointQuality(r.points[0], r.pm.Points[0]) != 0 {
		t.Fatal("heartbeat recovery failed")
	}
	d.HasHeartbeat = false
	*now += 3000
	r.evaluate(*now)
	if pointQuality(r.points[0], r.pm.Points[0]) != 0 {
		t.Fatal("stale boundary should be strict")
	}
	*now++
	r.evaluate(*now)
	if pointQuality(r.points[0], r.pm.Points[0]) != iec.Invalid|iec.NotTopical {
		t.Fatal("device expiry lost IV/NT")
	}
	before := r.devices[0].pollSeq
	m.result.Status = modbus.Exception
	r.pollDevice(0, m)
	if m.closes != 0 || r.devices[0].pollSeq != before {
		t.Fatal("exception altered transport/snapshot")
	}
	m.result.Status = modbus.Timeout
	r.pollDevice(0, m)
	if m.closes != 1 {
		t.Fatal("timeout did not close")
	}
	_ = c
}
