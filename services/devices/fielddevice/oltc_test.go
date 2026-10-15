// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func readyAuto(t *testing.T, m *Model)   { t.Helper(); write(t, m, 1, 0); measure(t, m, nil) }
func readyManual(t *testing.T, m *Model) { t.Helper(); write(t, m, 2, 1); measure(t, m, nil) }

func TestPLCRequestClassificationsAndAuditDedup(t *testing.T) {
	tests := []struct {
		name  string
		value uint16
		setup func(*testing.T, *Model)
		want  RequestResult
	}{
		{"unarmed", 101, func(t *testing.T, m *Model) { measure(t, m, nil) }, RequestUnarmed},
		{"local", 101, func(t *testing.T, m *Model) { action(t, m, "set_local") }, RequestNotRemote},
		{"manual", 101, func(t *testing.T, m *Model) { readyManual(t, m) }, RequestNotAuto},
		{"moving", 102, func(t *testing.T, m *Model) { readyAuto(t, m); write(t, m, 1, 101) }, RequestMoving},
		{"blocked", 101, func(t *testing.T, m *Model) { readyAuto(t, m); action(t, m, "block") }, RequestBlocked},
		{"invalid measurement", 101, func(t *testing.T, m *Model) { write(t, m, 1, 0) }, RequestNotEnergized},
		{"dead section", 101, func(t *testing.T, m *Model) {
			write(t, m, 1, 0)
			measure(t, m, func(i *MeasurementInput) { i.SectionLive = false })
		}, RequestNotEnergized},
		{"not adjacent", 102, func(t *testing.T, m *Model) { readyAuto(t, m) }, RequestNotAdjacent},
		{"accepted", 101, func(t *testing.T, m *Model) { readyAuto(t, m) }, RequestAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, OLTC)
			tt.setup(t, m)
			entries, _, _ := m.AuditAfter(0)
			count := len(entries)
			write(t, m, 1, tt.value)
			if m.Snapshot().PLCRequestResult != tt.want {
				t.Fatalf("result=%v want=%v", m.Snapshot().PLCRequestResult, tt.want)
			}
			entries, _, _ = m.AuditAfter(0)
			if len(entries) != count+1 || entries[len(entries)-1].Context != testActor {
				t.Fatal("request not audited once with peer")
			}
			for range 100 {
				write(t, m, 1, tt.value)
			}
			after, _, _ := m.AuditAfter(0)
			if len(after) != len(entries) {
				t.Fatal("repeated request flooded audit")
			}
			action(t, m, "reset") // Cancel timers, including real execution equivalents.
		})
	}
}

func TestPLCArmingAfterModeSelectorAndReset(t *testing.T) {
	for _, tt := range []struct {
		name       string
		transition func(*testing.T, *Model)
	}{
		{"re-enter auto", func(t *testing.T, m *Model) {
			write(t, m, 2, 1)
			write(t, m, 1, 101)
			write(t, m, 2, 2)
		}},
		{"re-enter remote", func(t *testing.T, m *Model) {
			action(t, m, "set_local")
			write(t, m, 1, 101)
			action(t, m, "set_remote")
		}},
		{"reset", func(t *testing.T, m *Model) {
			write(t, m, 1, 101)
			action(t, m, "reset")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, OLTC)
			readyAuto(t, m)
			tt.transition(t, m)
			if m.Truth().Armed {
				t.Fatal("handover left PLC armed")
			}
			measure(t, m, nil)
			// The same non-zero value existed before the handover/reset; a PLC
			// cyclic write must not turn it into a fresh movement request.
			for range 3 {
				write(t, m, 1, 101)
			}
			if m.Truth().Moving || m.Snapshot().PLCRequestResult != RequestUnarmed {
				t.Fatalf("retained request not blocked: %+v", m.Snapshot())
			}
			write(t, m, 1, 0)
			if !m.Truth().Armed {
				t.Fatal("zero did not arm")
			}
			measure(t, m, nil)
			write(t, m, 1, 101)
			if !m.Truth().Moving || m.Snapshot().PLCRequestResult != RequestAccepted {
				t.Fatal("armed adjacent request failed")
			}
		})
	}
}

func TestRetainedActualRequestAcrossReset(t *testing.T) {
	m, _ := newTestModel(t, OLTC)
	readyAuto(t, m)
	write(t, m, 1, 101)
	action(t, m, "reset")
	measure(t, m, nil)
	write(t, m, 1, 101)
	if m.Truth().Moving || m.Snapshot().PLCRequestResult != RequestUnarmed {
		t.Fatal("reset honored retained in-flight request")
	}
}

func TestNoOpHR1NeverFloodsAudit(t *testing.T) {
	for _, tt := range []struct {
		name   string
		value  uint16
		active bool
	}{
		{"zero", 0, false}, {"completed target", 100, false}, {"active target", 101, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, OLTC)
			readyAuto(t, m)
			if tt.active {
				write(t, m, 1, 101)
			}
			before := m.Truth()
			entries, _, _ := m.AuditAfter(0)
			for range 1000 {
				write(t, m, 1, tt.value)
			}
			after, _, _ := m.AuditAfter(0)
			if len(after) != len(entries) || m.Truth() != before {
				t.Fatal("no-op flooded audit or truth")
			}
		})
	}
	// Repeated zeros in local/manual cannot arm a later handover.
	m, _ := newTestModel(t, OLTC)
	action(t, m, "set_local")
	write(t, m, 1, 0)
	action(t, m, "set_remote")
	if m.Truth().Armed {
		t.Fatal("zero seen outside remote auto armed PLC")
	}
}

func TestIgnoredAuditOncePerChangeOfReason(t *testing.T) {
	m, _ := newTestModel(t, OLTC)
	measure(t, m, nil)
	write(t, m, 1, 101)
	write(t, m, 1, 102) // Same reason, different target: no extra audit.
	action(t, m, "block")
	write(t, m, 1, 101)
	action(t, m, "unblock")
	write(t, m, 1, 101)
	entries, _, _ := m.AuditAfter(0)
	reasons := []string{}
	for _, entry := range entries {
		if entry.Operation == "plc_request" {
			reasons = append(reasons, entry.Detail.(string))
		}
	}
	if len(reasons) != 3 || reasons[0] != "unarmed" || reasons[1] != "blocked" || reasons[2] != "unarmed" {
		t.Fatalf("reasons=%v", reasons)
	}
}

func TestManualStepRefusals(t *testing.T) {
	tests := []struct {
		name   string
		value  uint16
		setup  func(*testing.T, *Model)
		reason string
	}{
		{"local", 2, func(t *testing.T, m *Model) { action(t, m, "set_local") }, "not_remote"},
		{"auto", 2, nil, "not_manual"},
		{"moving", 2, func(t *testing.T, m *Model) { readyManual(t, m); write(t, m, 0, 2) }, "moving"},
		{"blocked", 2, func(t *testing.T, m *Model) { readyManual(t, m); action(t, m, "block") }, "blocked"},
		{"unknown", 2, func(t *testing.T, m *Model) { write(t, m, 2, 1) }, "not_energized"},
		{"dead", 2, func(t *testing.T, m *Model) {
			write(t, m, 2, 1)
			measure(t, m, func(i *MeasurementInput) { i.SectionLive = false })
		}, "not_energized"},
		{"upper limit", 2, func(t *testing.T, m *Model) { readyManual(t, m); m.truth.Tap, m.truth.TargetTap = 8, 8 }, "limit"},
		{"lower limit", 1, func(t *testing.T, m *Model) { readyManual(t, m); m.truth.Tap, m.truth.TargetTap = -8, -8 }, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, OLTC)
			if tt.setup != nil {
				tt.setup(t, m)
			}
			before := m.Truth()
			r := m.WriteHolding(context.Background(), 0, tt.value, testActor)
			if r.Accepted || r.Reason != tt.reason || m.Truth() != before {
				t.Fatalf("result=%+v truth=%+v", r, m.Truth())
			}
		})
	}
	m, _ := newTestModel(t, OLTC)
	action(t, m, "set_local")
	r := m.WriteHolding(context.Background(), 2, 1, testActor)
	if r.Accepted || r.Reason != "not_remote" {
		t.Fatal("mode accepted while local")
	}
}

func TestMotionTimingModeDuringMotionAndReset(t *testing.T) {
	for _, tt := range []struct {
		name   string
		value  uint16
		target int16
	}{{"lower", 1, -1}, {"raise", 2, 1}} {
		t.Run(tt.name, func(t *testing.T) {
			m, c := newTestModel(t, OLTC)
			readyManual(t, m)
			start := m.Truth()
			write(t, m, 0, tt.value)
			moving := m.Truth()
			if moving.Tap != 0 || moving.TargetTap != tt.target || !moving.Moving || moving.Revision != start.Revision+1 {
				t.Fatalf("moving=%+v", moving)
			}
			c.advance(MotionDuration - time.Nanosecond)
			if m.Truth().Tap != 0 || !m.Truth().Moving {
				t.Fatal("motion completed early")
			}
			write(t, m, 2, 2) // Accepted while moving, cannot cancel or replace the step.
			c.advance(time.Nanosecond)
			done := m.Truth()
			if done.Tap != tt.target || done.TargetTap != tt.target || done.Moving || !done.Auto || done.Armed || m.Snapshot().Measurements.Valid {
				t.Fatalf("done=%+v", done)
			}
			if done.Revision != moving.Revision+2 {
				t.Fatal("mode/complete revisions missing")
			}
			readyAuto(t, m)
			write(t, m, 1, uint16(100+done.Tap+1))
			beforeReset := m.Truth()
			queuedCallback := c.timers[len(c.timers)-1].call
			reset := action(t, m, "reset")
			if reset.Revision != beforeReset.Revision+1 || reset.Tap != 0 || reset.Moving || reset.Armed {
				t.Fatal("reset did not cancel and bump")
			}
			queuedCallback() // Stop may lose to an already-queued timer callback.
			c.advance(5 * time.Second)
			if m.Truth() != reset.Truth {
				t.Fatal("canceled timer changed reset truth")
			}
		})
	}
}

func TestInwardRequestsAtBothLimits(t *testing.T) {
	for _, limit := range []int16{-8, 8} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			m, c := newTestModel(t, OLTC)
			m.truth.Tap, m.truth.TargetTap = limit, limit
			readyAuto(t, m)
			target := limit + 1
			if limit == 8 {
				target = limit - 1
			}
			write(t, m, 1, uint16(100+target))
			c.advance(MotionDuration)
			if m.Truth().Tap != target {
				t.Fatal("inward request rejected")
			}
		})
	}
}

func TestHeartbeatQuarterSecondAndWrap(t *testing.T) {
	m, c := newTestModel(t, OLTC)
	for _, tt := range []struct {
		advance time.Duration
		want    uint16
	}{{249 * time.Millisecond, 0}, {time.Millisecond, 1}, {65535 * 250 * time.Millisecond, 0}, {250 * time.Millisecond, 1}} {
		c.advance(tt.advance)
		if m.Snapshot().Heartbeat != tt.want {
			t.Fatalf("heartbeat=%d want %d", m.Snapshot().Heartbeat, tt.want)
		}
	}
}
