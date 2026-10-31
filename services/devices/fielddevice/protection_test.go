// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"math"
	"testing"
	"time"
)

func sampleAt(t *testing.T, m *Model, at time.Time, amps float64) Snapshot {
	t.Helper()
	return measure(t, m, func(i *MeasurementInput) {
		i.SolvedAt = at.UTC().Format(time.RFC3339Nano)
		i.CurrentA = amps
	})
}

func TestSampleQuantisedTrip(t *testing.T) {
	m, c := newTestModel(t, Breaker)
	at := c.Now()
	s := sampleAt(t, m, at, 400)
	if !s.Protection.PickedUp || s.Protection.PickedUpSince == nil || s.ProtectionTripped {
		t.Fatalf("first pickup: %+v", s)
	}
	c.advance(time.Second)
	s = sampleAt(t, m, at.Add(time.Second), 500)
	if !s.ProtectionTripped || !s.Lockout || s.Position != PositionOpen ||
		s.Protection.LastTripCurrentA == nil || *s.Protection.LastTripCurrentA != 500 {
		t.Fatalf("quantised trip: %+v", s)
	}
	revision := s.Revision
	s = sampleAt(t, m, at.Add(2*time.Second), 500)
	if s.Revision != revision {
		t.Fatal("already-open breaker tripped again")
	}
}

func TestInstantaneousTrip(t *testing.T) {
	m, c := newTestModel(t, Breaker)
	s := sampleAt(t, m, c.Now(), 3000)
	if !s.ProtectionTripped || *s.Protection.LastTripCurrentA != 3000 {
		t.Fatalf("instantaneous trip: %+v", s)
	}
}

func TestF2BreakerIndependentProtectionAndReset(t *testing.T) {
	for _, instantaneous := range []bool{false, true} {
		t.Run(map[bool]string{false: "delayed", true: "instantaneous"}[instantaneous], func(t *testing.T) {
			cb1, clock := newTestModel(t, Breaker)
			f2, err := NewModel(Config{Kind: Breaker, DeviceID: "f2-breaker", Generation: 43, Clock: clock})
			if err != nil {
				t.Fatal(err)
			}
			settings := f2.Snapshot().Protection
			if settings.PickupA != 400 || settings.DelayMS != 1000 || settings.InstantaneousA == nil || *settings.InstantaneousA != 2000 {
				t.Fatalf("F2 settings: %+v", settings)
			}
			at, amps := clock.Now(), 450.0
			if instantaneous {
				amps = 2000
			}
			tripped := sampleAt(t, f2, at, amps)
			if !instantaneous {
				if tripped.ProtectionTripped {
					t.Fatal("F2 tripped before its delay")
				}
				clock.advance(time.Second)
				tripped = sampleAt(t, f2, at.Add(time.Second), amps)
			}
			if tripped.DeviceID != "f2-breaker" || !tripped.ProtectionTripped || !tripped.Lockout || tripped.Position != PositionOpen {
				t.Fatalf("F2 did not trip: %+v", tripped)
			}
			if cb1.Truth().ProtectionTripped || cb1.Truth().Position != PositionClosed {
				t.Fatal("F2 trip affected CB1")
			}
			reset := action(t, f2, "reset")
			if reset.ProtectionTripped || reset.Lockout || reset.Position != PositionClosed || reset.Revision <= tripped.Revision {
				t.Fatalf("F2 instructor reset: %+v", reset)
			}
		})
	}
}

func TestPickupTimerResets(t *testing.T) {
	for _, cause := range []string{"drop-off", "epoch", "generation", "gap", "expiry", "revision"} {
		t.Run(cause, func(t *testing.T) {
			m, c := newTestModel(t, Breaker)
			at := c.Now()
			sampleAt(t, m, at, 450)
			next := at.Add(500 * time.Millisecond)
			switch cause {
			case "drop-off":
				sampleAt(t, m, next, 399)
				next = at.Add(time.Second)
			case "epoch":
				m.measurements.SolverEpoch = ptr("old-epoch")
			case "generation":
				m.truth.Generation++
			case "gap":
				next = at.Add(2501 * time.Millisecond)
			case "expiry":
				c.advance(MeasurementValidity)
				if m.Snapshot().Protection.PickedUpSince != nil {
					t.Fatal("expiry retained pickup")
				}
				next = c.Now()
			case "revision":
				// Revision invalidation alone is explicitly not a timer reset.
				m.changedLocked()
				next = at.Add(time.Second)
			}
			s := sampleAt(t, m, next, 450)
			if cause == "revision" {
				if !s.ProtectionTripped {
					t.Fatal("revision alone reset timer")
				}
			} else if s.ProtectionTripped || s.Protection.PickedUpSince == nil ||
				!s.Protection.PickedUpSince.Equal(next) {
				t.Fatalf("%s did not restart timer: %+v", cause, s)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestRejectedSamplesDoNotChangeTimer(t *testing.T) {
	m, c := newTestModel(t, Breaker)
	at := c.Now()
	sampleAt(t, m, at, 450)
	for _, cause := range []string{"sequence", "equal-time", "earlier-time", "stale-truth", "schema2", "nan-power"} {
		t.Run(cause, func(t *testing.T) {
			i := testMeasurement(m)
			i.SolvedAt = at.Add(time.Second).Format(time.RFC3339Nano)
			i.CurrentA = 0 // Would drop off if accepted.
			want := MeasurementReplay
			switch cause {
			case "sequence":
				i.SolveSeq = 1
			case "equal-time":
				i.SolvedAt = at.Format(time.RFC3339Nano)
			case "earlier-time":
				i.SolvedAt = at.Add(-time.Second).Format(time.RFC3339Nano)
			case "stale-truth":
				i.Revision++
				want = MeasurementStaleTruth
			case "schema2":
				i.Schema = 2
				want = MeasurementInvalid
			case "nan-power":
				i.PMW = math.NaN()
				want = MeasurementInvalid
			}
			s, err := m.ApplyMeasurements(context.Background(), i, testActor)
			if err != want || !s.Protection.PickedUp || !s.Protection.PickedUpSince.Equal(at) {
				t.Fatalf("rejection changed timer: err=%v %+v", err, s.Protection)
			}
		})
	}
	if !sampleAt(t, m, at.Add(time.Second), 450).ProtectionTripped {
		t.Fatal("rejections changed operate time")
	}
}

func TestEffectiveOLTCBlock(t *testing.T) {
	m, c := newTestModel(t, OLTC)
	s := sampleAt(t, m, c.Now(), 866)
	if s.Blocked || !s.Protection.PickedUp {
		t.Fatal("overcurrent changed manual truth block")
	}
	write(t, m, 1, 0)
	if r := write(t, m, 1, 101); r.Reason != "blocked" {
		t.Fatalf("PLC bypassed block: %+v", r)
	}
	write(t, m, 2, 1)
	if r := m.WriteHolding(context.Background(), 0, 2, testActor); r.Reason != "blocked" || r.Accepted {
		t.Fatalf("manual tap bypassed block: %+v", r)
	}
}

func TestPowerFactorAndInvalidPower(t *testing.T) {
	m, _ := newTestModel(t, OLTC)
	for _, tc := range []struct {
		p, q float64
		pf   *float64
	}{
		{0, 0, nil}, {0.0006, 0.0007, nil}, {-0.0006, -0.0008, ptr(0.6)},
		{-3, -4, ptr(0.6)}, {3, 4, ptr(0.6)},
	} {
		s := measure(t, m, func(i *MeasurementInput) { i.PMW, i.QMvar = tc.p, tc.q })
		if (s.Measurements.PF == nil) != (tc.pf == nil) ||
			(tc.pf != nil && math.Abs(*s.Measurements.PF-*tc.pf) > 1e-12) {
			t.Fatalf("PF for %v/%v: %+v", tc.p, tc.q, s.Measurements)
		}
	}
	s := action(t, m, "block")
	if s.Measurements.PMW != nil || s.Measurements.QMvar != nil || s.Measurements.PF != nil {
		t.Fatal("invalid measurements retained power")
	}
}

func TestFPIPickupThresholdAndLatch(t *testing.T) {
	m, _ := newTestModel(t, RMU)
	if measure(t, m, func(i *MeasurementInput) { i.CurrentA = 299 }).FaultPassage {
		t.Fatal("FPI latched below threshold")
	}
	s := measure(t, m, func(i *MeasurementInput) { i.CurrentA = 300 })
	if !s.FaultPassage || !s.Protection.PickedUp || s.Position != PositionClosed {
		t.Fatal("zero-delay FPI did not latch on pickup")
	}
	if !measure(t, m, nil).FaultPassage {
		t.Fatal("drop-off cleared FPI latch")
	}
}

func TestProtectionSettingsValidation(t *testing.T) {
	for _, edit := range []func(*ProtectionSettings){
		func(s *ProtectionSettings) { s.PickupA = math.NaN() },
		func(s *ProtectionSettings) { s.PickupA = 0 },
		func(s *ProtectionSettings) { s.DelayMS = -1 },
		func(s *ProtectionSettings) { s.DelayMS = 1<<63 - 1 },
		func(s *ProtectionSettings) { s.MaxGapMS = 1<<63 - 1 },
		func(s *ProtectionSettings) { s.MaxGapMS = 0 },
		func(s *ProtectionSettings) { s.InstantaneousA = ptr(math.Inf(1)) },
		func(s *ProtectionSettings) { s.InstantaneousA = ptr(399.0) },
	} {
		settings := DefaultProtection(Breaker)
		edit(&settings)
		if _, err := NewModel(Config{Kind: Breaker, DeviceID: "feeder-breaker", Protection: &settings}); err == nil {
			t.Fatalf("invalid settings accepted: %+v", settings)
		}
	}
}
