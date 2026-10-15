// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"testing"
)

func TestSwitchRefusals(t *testing.T) {
	tests := []struct {
		name   string
		kind   Kind
		value  uint16
		setup  func(*testing.T, *Model)
		reason string
	}{
		{"breaker local open", Breaker, 1, func(t *testing.T, m *Model) { m.truth.RemoteEnabled = false }, "not_remote"},
		{"breaker local close", Breaker, 2, func(t *testing.T, m *Model) { m.truth.RemoteEnabled = false }, "not_remote"},
		{"breaker lockout", Breaker, 2, func(t *testing.T, m *Model) { measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true }) }, "lockout"},
		{"breaker unknown", Breaker, 2, nil, "no_measurement"},
		{"breaker live-live", Breaker, 2, func(t *testing.T, m *Model) { measure(t, m, func(i *MeasurementInput) { i.SourceLive = true }) }, "live_live"},
		{"rmu local close", RMU, 2, func(t *testing.T, m *Model) { action(t, m, "set_local") }, "not_remote"},
		{"rmu local open", RMU, 1, func(t *testing.T, m *Model) { action(t, m, "set_local") }, "not_remote"},
		{"rmu earth", RMU, 2, func(t *testing.T, m *Model) {
			measure(t, m, nil)
			write(t, m, 0, 1)
			measure(t, m, func(i *MeasurementInput) { i.SectionLive = false })
			action(t, m, "earth_close")
		}, "earth_closed"},
		{"rmu unknown", RMU, 2, nil, "no_measurement"},
		{"rmu unknown open", RMU, 1, nil, "no_measurement"},
		{"rmu live-live", RMU, 2, func(t *testing.T, m *Model) { measure(t, m, func(i *MeasurementInput) { i.SourceLive = true }) }, "live_live"},
		{"rmu fault-current", RMU, 1, func(t *testing.T, m *Model) {
			measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
			measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
		}, "fault_current"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, tt.kind)
			if tt.setup != nil {
				tt.setup(t, m)
			}
			before := m.Truth()
			result := m.WriteHolding(context.Background(), 0, tt.value, testActor)
			if result.Accepted || result.Reason != tt.reason || m.Truth() != before {
				t.Fatalf("result=%+v truth=%+v", result, m.Truth())
			}
			entries, _, _ := m.AuditAfter(0)
			last := entries[len(entries)-1]
			if last.Context != testActor || last.Result != "rejected" || last.Detail != tt.reason {
				t.Fatalf("audit=%+v", last)
			}
		})
	}
}

func TestSwitchSuccessAndNoOp(t *testing.T) {
	for _, kind := range []Kind{Breaker, RMU} {
		t.Run(string(kind), func(t *testing.T) {
			m, _ := newTestModel(t, kind)
			measure(t, m, nil)
			write(t, m, 0, 1)
			measure(t, m, nil)
			write(t, m, 0, 2)
			if m.Truth().Position != PositionClosed || m.Snapshot().Measurements.Valid {
				t.Fatal("close did not invalidate solved truth")
			}
			measure(t, m, nil)
			before := m.Truth()
			if r := write(t, m, 0, 2); r.Changed || m.Truth() != before {
				t.Fatal("no-op changed truth")
			}
		})
	}
}

func TestRMUOpenAfterFPILatch(t *testing.T) {
	m, _ := newTestModel(t, RMU)
	latched := measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
	if !latched.FaultPassage || latched.Measurements.Valid || latched.Position != PositionClosed {
		t.Fatalf("FPI did not latch and invalidate measurements: %+v", latched)
	}
	for _, tt := range []struct {
		name         string
		deliver      bool
		faultCurrent bool
		reason       string
	}{
		{"immediate open after latch", false, false, "no_measurement"},
		{"valid fault current", true, true, "fault_current"},
		{"valid without fault current", true, false, ""},
	} {
		if !t.Run(tt.name, func(t *testing.T) {
			if tt.deliver {
				measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = tt.faultCurrent })
			}
			before := m.Truth()
			_, cursor, _ := m.AuditAfter(0)
			result := m.WriteHolding(context.Background(), 0, 1, testActor)
			wantAccepted := tt.reason == ""
			if result.Accepted != wantAccepted || result.Reason != tt.reason || result.Changed != wantAccepted {
				t.Fatalf("open result=%+v, want refusal=%q", result, tt.reason)
			}
			after := m.Truth()
			if wantAccepted {
				if after.Position != PositionOpen || after.Revision != before.Revision+1 || !after.FaultPassage {
					t.Fatalf("open did not preserve FPI and change position: %+v", after)
				}
			} else if after != before {
				t.Fatal("refused open changed truth")
			}
			entries, _, _ := m.AuditAfter(*cursor)
			wantResult := "rejected"
			if wantAccepted {
				wantResult = "accepted"
			}
			if len(entries) != 1 || entries[0].Operation != "open" ||
				entries[0].Result != wantResult || entries[0].Detail != tt.reason ||
				entries[0].Context != testActor {
				t.Fatalf("open audit lost outcome/reason/peer: %+v", entries)
			}
		}) {
			return // Later rows depend on the preceding refusal leaving the way closed.
		}
	}
}

func TestBreakerOpenDoesNotRequireMeasurements(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(*testing.T, *Model, *fakeClock)
	}{
		{"unknown", func(t *testing.T, m *Model, c *fakeClock) {}},
		{"protection-invalidated", func(t *testing.T, m *Model, c *fakeClock) {
			measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
		}},
		{"expired", func(t *testing.T, m *Model, c *fakeClock) {
			measure(t, m, nil)
			c.advance(MeasurementValidity)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, c := newTestModel(t, Breaker)
			tt.setup(t, m, c)
			if m.Snapshot().Measurements.Valid {
				t.Fatal("test requires invalid measurements")
			}
			write(t, m, 0, 1)
			if m.Truth().Position != PositionOpen {
				t.Fatal("remote breaker open failed")
			}
		})
	}
}

func TestProtectionAndFPILatchOnMeasurement(t *testing.T) {
	for _, kind := range []Kind{Breaker, RMU} {
		t.Run(string(kind), func(t *testing.T) {
			m, _ := newTestModel(t, kind)
			// Local authority cannot disable protection or FPI.
			m.truth.RemoteEnabled = false
			before := m.Truth()
			s := measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
			if s.Revision != before.Revision+1 || s.EventSeq != before.EventSeq+1 || s.Measurements.Valid {
				t.Fatalf("effect=%+v", s)
			}
			if kind == Breaker && (s.Position != PositionOpen || !s.Lockout || !s.ProtectionTripped) {
				t.Fatal("breaker did not trip")
			}
			if kind == RMU && (!s.FaultPassage || s.Position != PositionClosed) {
				t.Fatal("FPI did not latch independently")
			}
			entries, _, _ := m.AuditAfter(0)
			last := entries[len(entries)-1]
			if last.Context.Protocol != "local-protection" || last.Context.Zone != "field" {
				t.Fatalf("protection actor=%+v", last.Context)
			}
			tripped := s.Revision
			s = measure(t, m, func(i *MeasurementInput) { i.FaultCurrent = true })
			if !s.Measurements.Valid || s.Revision != tripped {
				t.Fatal("latched indication repeatedly bumped truth")
			}
			s = measure(t, m, nil)
			if kind == Breaker {
				if !s.Lockout || !s.ProtectionTripped {
					t.Fatal("clear current cleared lockout")
				}
				s = action(t, m, "reset_lockout")
				if s.Lockout || s.ProtectionTripped || s.Position != PositionOpen {
					t.Fatal("reset_lockout reclosed breaker")
				}
			} else {
				if !s.FaultPassage {
					t.Fatal("FPI failed to latch after current cleared")
				}
				if action(t, m, "reset_fault_passage").FaultPassage {
					t.Fatal("FPI reset failed")
				}
			}
			s = action(t, m, "reset")
			if s.Position != PositionClosed || !s.RemoteEnabled {
				t.Fatal("reset failed")
			}
		})
	}
}

func TestEarthCloseRefusalsAndActions(t *testing.T) {
	for _, tt := range []struct {
		name              string
		open, known, live bool
		reason            ActionError
	}{
		{"closed", false, false, false, "way_closed"}, {"unknown", true, false, false, "no_measurement"}, {"live", true, true, true, "section_live"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, RMU)
			if tt.open {
				measure(t, m, nil)
				write(t, m, 0, 1)
			}
			if tt.known {
				measure(t, m, func(i *MeasurementInput) { i.SectionLive = tt.live })
			}
			before := m.Truth()
			if _, err := m.Action(context.Background(), "earth_close", labActor); err != tt.reason || m.Truth() != before {
				t.Fatalf("earth close err=%v", err)
			}
		})
	}
	m, _ := newTestModel(t, RMU)
	measure(t, m, nil)
	write(t, m, 0, 1)
	measure(t, m, func(i *MeasurementInput) { i.SectionLive = false })
	if !action(t, m, "earth_close").EarthClosed || action(t, m, "earth_open").EarthClosed {
		t.Fatal("earth actions")
	}
	if action(t, m, "set_local").RemoteEnabled || !action(t, m, "set_remote").RemoteEnabled {
		t.Fatal("selector actions")
	}
	for _, kind := range []Kind{Breaker, RMU, OLTC} {
		m, _ := newTestModel(t, kind)
		for _, name := range []string{"inject_fault", "clear_fault", "invalid"} {
			if _, err := m.Action(context.Background(), name, labActor); err != ActionUnknown {
				t.Fatalf("%s accepted %s", kind, name)
			}
		}
	}
}
