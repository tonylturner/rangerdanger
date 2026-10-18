// SPDX-License-Identifier: Apache-2.0
package modbus

import (
	"math"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
)

func inputRegisters(s fielddevice.Snapshot) []uint16 {
	count := 17
	if s.Kind == fielddevice.RMU {
		count = 13
	}
	if s.Kind == fielddevice.OLTC {
		count = 21
	}
	registers := make([]uint16, count)
	registers[0], _ = positionRegister(s.Position)
	setBit := func(bit uint16, on bool) {
		if on {
			registers[1] |= 1 << bit
		}
	}
	setBit(0, s.RemoteEnabled)
	pickedUp := s.Protection.PickedUp
	switch s.Kind {
	case fielddevice.Breaker:
		setBit(1, s.Lockout)
		setBit(2, pickedUp)
		setBit(3, s.ProtectionTripped)
	case fielddevice.RMU:
		setBit(1, s.EarthClosed)
		setBit(2, s.FaultPassage)
		setBit(3, pickedUp)
	case fielddevice.OLTC:
		registers[0] = uint16(s.Tap)
		setBit(1, s.Auto)
		setBit(2, s.Moving)
		setBit(3, s.Tap == -8)
		setBit(4, s.Tap == 8)
		setBit(5, s.Blocked || pickedUp)
		setBit(7, pickedUp)
		setBit(6, s.Measurements.SectionLive != nil && *s.Measurements.SectionLive)
		registers[11], registers[13], registers[14] = uint16(s.TargetTap), uint16(s.PLCRequestResult), s.Heartbeat
		if s.Measurements.Valid && s.Measurements.VoltageKV != nil {
			// Saturate values outside the u16 engineering range, never wrap a voltage.
			registers[12] = uint16(min(65535, math.Round(*s.Measurements.VoltageKV*1000)))
		}
	}
	if s.Measurements.Valid {
		registers[2] = 1
	}
	current, voltage := s.Measurements.CurrentA, s.Measurements.VoltageKV
	if s.Kind == fielddevice.OLTC {
		current, voltage = voltage, current
	}
	if current != nil {
		putFloat32(registers[3:5], *current)
	}
	if voltage != nil {
		putFloat32(registers[5:7], *voltage)
	}
	putUint32(registers[7:9], uint32(s.EventSeq))
	putUint32(registers[9:11], s.Generation)
	if s.Kind == fielddevice.RMU && s.Measurements.SourceKV != nil {
		putFloat32(registers[11:13], *s.Measurements.SourceKV)
	}
	if s.Kind != fielddevice.RMU {
		start := 11
		if s.Kind == fielddevice.OLTC {
			start = 15
		}
		for i, v := range []*float64{s.Measurements.PMW, s.Measurements.QMvar, s.Measurements.PF} {
			n := math.NaN()
			if s.Measurements.Valid && v != nil {
				n = *v
			}
			putFloat32(registers[start+2*i:start+2*i+2], n)
		}
	}
	return registers
}
func putFloat32(registers []uint16, value float64) {
	putUint32(registers, math.Float32bits(float32(value)))
}
func putUint32(registers []uint16, value uint32) {
	registers[0], registers[1] = uint16(value>>16), uint16(value)
}
