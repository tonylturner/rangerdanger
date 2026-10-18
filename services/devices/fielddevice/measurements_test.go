// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestMeasurementsReplayStaleExpiryAndEpoch(t *testing.T) {
	m, c := newTestModel(t, Breaker)
	input := testMeasurement(m)
	measure(t, m, nil)
	for _, tt := range []struct {
		name string
		edit func(*MeasurementInput)
		want error
	}{
		{"replay", func(i *MeasurementInput) {}, MeasurementReplay},
		{"stale revision", func(i *MeasurementInput) { i.Revision++ }, MeasurementStaleTruth},
		{"stale generation", func(i *MeasurementInput) { i.Generation++ }, MeasurementStaleTruth},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invalid := input
			tt.edit(&invalid)
			if _, err := m.ApplyMeasurements(context.Background(), invalid, testActor); err != tt.want {
				t.Fatalf("err=%v want %v", err, tt.want)
			}
		})
	}
	c.advance(3*time.Second - time.Nanosecond)
	if !m.Snapshot().Measurements.Valid {
		t.Fatal("expired early")
	}
	c.advance(time.Nanosecond)
	s := m.Snapshot()
	if s.Measurements.Valid || s.Measurements.CurrentA == nil || *s.Measurements.CurrentA != 41.2 {
		t.Fatal("expiry lost last known or retained validity")
	}
	if valid, age := m.MeasurementStatus(); valid || age == nil || *age != 3000 {
		t.Fatal("age/validity wrong")
	}
	for range 3 {
		m.Snapshot()
	}
	entries, _, _ := m.AuditAfter(0)
	count := 0
	for _, e := range entries {
		if e.Operation == "measurements_invalid" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expiry audit count=%d", count)
	}
	input.SolverEpoch = "restarted"
	input.SolveSeq = 1
	if _, err := m.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}
	if !m.Snapshot().Measurements.Valid {
		t.Fatal("new epoch not accepted")
	}
}

func TestMeasurementInvalidValues(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*MeasurementInput)
	}{
		{"schema1", func(i *MeasurementInput) { i.Schema = 1 }},
		{"missing epoch", func(i *MeasurementInput) { i.SolverEpoch = "" }},
		{"zero sequence", func(i *MeasurementInput) { i.SolveSeq = 0 }},
		{"wrong identity", func(i *MeasurementInput) { i.DeviceID = "breaker-1" }},
		{"negative current", func(i *MeasurementInput) { i.CurrentA = -1 }},
		{"nan voltage", func(i *MeasurementInput) { i.VoltageKV = math.NaN() }},
		{"infinite source", func(i *MeasurementInput) { i.SourceKV = math.Inf(1) }},
		{"overflow", func(i *MeasurementInput) { i.CurrentA = math.MaxFloat64 }},
		{"nonUTC", func(i *MeasurementInput) { i.SolvedAt = "2026-10-15T10:00:00-04:00" }},
		{"UTC without Z", func(i *MeasurementInput) { i.SolvedAt = "2026-10-15T10:00:00+00:00" }},
		{"comma time", func(i *MeasurementInput) { i.SolvedAt = "2026-10-15T10:00:00,123Z" }},
		{"short hour", func(i *MeasurementInput) { i.SolvedAt = "2026-10-15T1:00:00Z" }},
		{"bad time", func(i *MeasurementInput) { i.SolvedAt = "no" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(t, Breaker)
			input := testMeasurement(m)
			tt.edit(&input)
			if _, err := m.ApplyMeasurements(context.Background(), input, testActor); err != MeasurementInvalid {
				t.Fatalf("err=%v", err)
			}
			if m.Snapshot().Measurements.Valid {
				t.Fatal("invalid measurement applied")
			}
		})
	}
}

func TestFirstSolvePerRevisionEvidence(t *testing.T) {
	m, _ := newTestModel(t, Breaker)
	measure(t, m, nil)
	measure(t, m, nil)
	write(t, m, 0, 1)
	measure(t, m, nil)
	entries, _, _ := m.AuditAfter(0)
	first := 0
	for _, e := range entries {
		if e.Operation != "first_solve" {
			continue
		}
		first++
		d := e.Detail.(map[string]any)
		if d["revision"] != e.After.Revision || d["solver_epoch"] != "solver-a" || d["current_a"] != float64(41.2) {
			t.Fatalf("first solve evidence=%+v", e)
		}
	}
	if first != 2 {
		t.Fatalf("first solves=%d", first)
	}
}

func keys(m map[string]json.RawMessage) []string {
	result := []string{}
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
func assertKeys(t *testing.T, m map[string]json.RawMessage, want string) {
	t.Helper()
	expected := strings.Fields(want)
	sort.Strings(expected)
	if !reflect.DeepEqual(keys(m), expected) {
		t.Fatalf("keys=%v want=%v", keys(m), expected)
	}
}
func TestExactSchemaThreeTruthAndStateKeys(t *testing.T) {
	common := "schema device_id kind generation revision event_seq changed_at remote_enabled "
	for _, tt := range []struct {
		kind  Kind
		extra string
	}{
		{Breaker, "position lockout protection_tripped"}, {RMU, "position earth_closed fault_passage"}, {OLTC, "tap target_tap moving auto blocked armed"},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			m, _ := newTestModel(t, tt.kind)
			for _, state := range []bool{false, true} {
				var body []byte
				var err error
				if state {
					body, err = json.Marshal(m.Snapshot())
				} else {
					body, err = json.Marshal(m.Truth())
				}
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				want := common + tt.extra
				if state {
					want += " measurements protection"
					if tt.kind == OLTC {
						want += " plc_request_result heartbeat"
					}
				}
				assertKeys(t, fields, want)
				if string(fields["schema"]) != "3" || !strings.HasSuffix(string(fields["changed_at"]), `Z"`) {
					t.Fatalf("schema/time=%s", body)
				}
				if state {
					var protection map[string]json.RawMessage
					if err := json.Unmarshal(fields["protection"], &protection); err != nil {
						t.Fatal(err)
					}
					assertKeys(t, protection, "function pickup_a delay_ms instantaneous_a picked_up picked_up_since last_trip_current_a")
					var values map[string]json.RawMessage
					if err := json.Unmarshal(fields["measurements"], &values); err != nil {
						t.Fatal(err)
					}
					assertKeys(t, values, "valid current_a voltage_kv_ll source_kv_ll section_live source_live p_mw q_mvar pf solver_epoch solve_seq solved_at received_at")
					for key, value := range values {
						if key != "valid" && string(value) != "null" {
							t.Fatalf("unknown %s=%s", key, value)
						}
					}
				}
			}
		})
	}
}

func TestCanceledOperationsAndIdentityValidation(t *testing.T) {
	m, _ := newTestModel(t, OLTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ApplyMeasurements(ctx, testMeasurement(m), testActor); err == nil {
		t.Fatal("canceled measurement accepted")
	}
	if _, err := m.Action(ctx, "reset", labActor); err == nil {
		t.Fatal("canceled action accepted")
	}
	if m.WriteHolding(ctx, 2, 1, testActor).Accepted {
		t.Fatal("canceled write accepted")
	}
	for _, cfg := range []Config{
		{}, {Kind: Breaker, DeviceID: "breaker-1"}, {Kind: RMU, DeviceID: "wrong", RMUNormal: PositionOpen},
		{Kind: RMU, DeviceID: "rmu-1"}, {Kind: OLTC, DeviceID: "wrong"},
	} {
		if _, err := NewModel(cfg); err == nil {
			t.Fatalf("bad config accepted=%+v", cfg)
		}
	}
	for _, normal := range []Position{PositionOpen, PositionClosed} {
		rmu, err := NewModel(Config{Kind: RMU, DeviceID: "rmu-2", RMUNormal: normal})
		if err != nil || rmu.Truth().Position != normal {
			t.Fatal("normal config failed")
		}
	}
}

func TestSnapshotDoesNotExposeOwnedMeasurementPointers(t *testing.T) {
	m, _ := newTestModel(t, Breaker)
	measure(t, m, func(i *MeasurementInput) { i.PMW = 3; i.QMvar = 4 })
	snapshot := m.Snapshot()
	*snapshot.Measurements.PMW = 999
	*snapshot.Measurements.QMvar = 999
	*snapshot.Measurements.PF = 999
	*snapshot.Protection.InstantaneousA = 999
	*snapshot.Measurements.CurrentA = 999
	*snapshot.Measurements.SectionLive = false
	*snapshot.Measurements.SolverEpoch = "mutated"
	current := m.Snapshot().Measurements
	if *current.PMW != 3 || *current.QMvar != 4 || *current.PF != 0.6 || *m.Snapshot().Protection.InstantaneousA != 3000 || *current.CurrentA != 41.2 || !*current.SectionLive || *current.SolverEpoch != "solver-a" {
		t.Fatal("snapshot mutated owned state")
	}
}
