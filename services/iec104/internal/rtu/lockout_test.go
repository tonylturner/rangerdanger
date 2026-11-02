// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"testing"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func TestBreakerLockoutRegisterAndWire(t *testing.T) {
	pm, err := pointmap.Load("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(pm, Options{Clock: fixedClock{true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"feeder-lockout", "f2-lockout"} {
		t.Run(id, func(t *testing.T) {
			index := -1
			for i, p := range pm.Points {
				if p.ID == id {
					index = i
				}
			}
			if index < 0 {
				t.Fatal("lockout missing")
			}
			p := pm.Points[index]
			dev := pm.Devices[p.DeviceIndex]
			for _, flags := range []uint16{0, 2, 8, 10, 0} {
				regs := make([]uint16, dev.PollCount)
				regs[1-dev.PollStart] = flags
				r.pollDevice(p.DeviceIndex, &fakeMaster{regs: regs})
				r.evaluate(r.opts.Now())
				// Invalid measurements must not invalidate a separately observed latch.
				r.points[index].measurementInvalid = true
				for _, timed := range []bool{false, true} {
					typ, element, err := r.element(index, timed)
					if err != nil {
						t.Fatal(err)
					}
					wantType := iec.M_SP_NA_1
					if timed {
						wantType = iec.M_SP_TB_1
						element = element.(iec.Timed).Value
					}
					want := iec.SIQ((flags >> 1) & 1)
					if typ != wantType || element.(iec.SinglePoint).SIQ != want {
						t.Fatalf("flags=%d timed=%t type=%v element=%+v", flags, timed, typ, element)
					}
				}
			}
		})
	}
}
