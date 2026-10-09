// SPDX-License-Identifier: Apache-2.0
package feederbreaker

import (
	"context"
	"math"
	"testing"
	"time"
)

var testActor = AuditContext{Peer: "10.50.50.20:43123", Protocol: "process", Zone: "process"}

func newTestModel(t *testing.T, now func() time.Time) *Model {
	t.Helper()
	model, err := NewModel(Config{
		DeviceID:       "feeder-breaker",
		Generation:     42,
		Now:            now,
		LocalContext:   AuditContext{Peer: "protection", Protocol: "local-protection", Zone: "field"},
		ProcessContext: AuditContext{Peer: "measurement-expiry", Protocol: "process", Zone: "process"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func testMeasurement(model *Model, epoch string, seq uint64) MeasurementInput {
	truth := model.Truth()
	return MeasurementInput{
		Schema: 1, DeviceID: truth.DeviceID, Generation: truth.Generation,
		Revision: truth.Revision, SolverEpoch: epoch, SolveSeq: seq,
		SolvedAt: "2026-10-09T12:00:00.123456789Z",
		CurrentA: 28.9, VoltageKV: 19.9,
	}
}

func TestInitialTruthAndNoOpCommand(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }
	model := newTestModel(t, now)
	initial := model.Snapshot()
	if initial.Position != PositionClosed || !initial.RemoteEnabled || initial.Lockout ||
		initial.FaultActive || initial.ProtectionTripped || initial.Revision != 1 ||
		initial.Measurements.Valid || initial.Measurements.CurrentA != nil {
		t.Fatalf("initial snapshot = %+v", initial)
	}

	result := model.Command(context.Background(), CommandClose, testActor)
	if !result.Accepted || result.Changed {
		t.Fatalf("no-op close result = %+v", result)
	}
	after := model.Snapshot()
	if after.Revision != initial.Revision || after.EventSeq != initial.EventSeq {
		t.Fatalf("no-op changed truth: before=%+v after=%+v", initial.Truth, after.Truth)
	}
	entries, _, _ := model.AuditAfter(0)
	if len(entries) != 1 || entries[0].Operation != "close" || entries[0].Result != "accepted" || entries[0].Detail != "no_op" {
		t.Fatalf("no-op audit = %+v", entries)
	}
}

func TestRemoteAndCloseInterlocks(t *testing.T) {
	now := time.Now
	model := newTestModel(t, now)

	model.mu.Lock()
	model.remoteEnabled = false
	model.mu.Unlock()
	if result := model.Command(context.Background(), CommandOpen, testActor); result.Accepted || result.Reason != "remote_disabled" {
		t.Fatalf("open with remote disabled = %+v", result)
	}
	if result := model.Command(context.Background(), CommandClose, testActor); result.Accepted || result.Reason != "remote_disabled" {
		t.Fatalf("close with remote disabled = %+v", result)
	}
	model.mu.Lock()
	model.remoteEnabled = true
	model.mu.Unlock()

	if _, err := model.InjectFault(context.Background(), true, AuditContext{Peer: "instructor", Protocol: "instructor", Zone: "mgmt"}); err != nil {
		t.Fatal(err)
	}
	if result := model.Command(context.Background(), CommandClose, testActor); result.Accepted || result.Reason != "lockout" {
		t.Fatalf("close under fault lockout = %+v", result)
	}
	_, _ = model.ClearFault(context.Background(), AuditContext{Peer: "instructor", Protocol: "instructor", Zone: "mgmt"})
	if result := model.Command(context.Background(), CommandClose, testActor); result.Accepted || result.Reason != "lockout" {
		t.Fatalf("close after fault clear = %+v", result)
	}
	model.mu.Lock()
	model.lockout = false
	model.faultActive = true
	model.mu.Unlock()
	if result := model.Command(context.Background(), CommandClose, testActor); result.Accepted || result.Reason != "fault_active" {
		t.Fatalf("close under fault without lockout = %+v", result)
	}
}

func TestLocalProtectionLatchClearAndReset(t *testing.T) {
	model := newTestModel(t, time.Now)
	model.mu.Lock()
	model.remoteEnabled = false
	model.mu.Unlock()

	faultContext := AuditContext{Peer: "10.99.99.22:1234", Protocol: "instructor", Zone: "mgmt"}
	result, err := model.InjectFault(context.Background(), true, faultContext)
	if err != nil || !result.Accepted {
		t.Fatalf("fault injection result=%+v err=%v", result, err)
	}
	tripped := model.Snapshot()
	if tripped.Position != PositionOpen || !tripped.FaultActive || !tripped.Lockout || !tripped.ProtectionTripped || tripped.RemoteEnabled {
		t.Fatalf("fault did not trip and latch locally: %+v", tripped.Truth)
	}
	entries, _, _ := model.AuditAfter(0)
	if len(entries) != 2 || entries[1].Operation != "local_protection_trip" ||
		entries[1].Context.Protocol != "local-protection" || entries[1].Context.Zone != "field" {
		t.Fatalf("local-protection audit missing or misattributed: %+v", entries)
	}

	if _, err := model.ClearFault(context.Background(), faultContext); err != nil {
		t.Fatal(err)
	}
	cleared := model.Snapshot()
	if cleared.Position != PositionOpen || cleared.FaultActive || !cleared.Lockout || !cleared.ProtectionTripped {
		t.Fatalf("clearing fault changed the latch or reclosed: %+v", cleared.Truth)
	}

	beforeResetRevision := cleared.Revision
	if err := model.Reset(context.Background(), faultContext); err != nil {
		t.Fatal(err)
	}
	reset := model.Snapshot()
	if reset.Position != PositionClosed || !reset.RemoteEnabled || reset.Lockout || reset.FaultActive ||
		reset.ProtectionTripped || reset.Revision != beforeResetRevision+1 {
		t.Fatalf("reset did not restore initial truth and bump revision: %+v", reset.Truth)
	}
	if reset.EventSeq != cleared.EventSeq+1 {
		t.Fatalf("reset event sequence = %d, want %d", reset.EventSeq, cleared.EventSeq+1)
	}
}

func TestMeasurementReplayStaleTruthAndNewEpoch(t *testing.T) {
	now := time.Now
	model := newTestModel(t, now)
	input := testMeasurement(model, "solver-a", 8)
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatalf("initial measurement: %v", err)
	}
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != MeasurementReplay {
		t.Fatalf("duplicate solve error = %v, want replay", err)
	}

	next := input
	next.SolveSeq = 9
	next.SolvedAt = "2026-10-09T12:00:01Z"
	if _, err := model.ApplyMeasurements(context.Background(), next, testActor); err != nil {
		t.Fatalf("increasing solve sequence: %v", err)
	}
	newEpoch := next
	newEpoch.SolverEpoch = "solver-restarted"
	newEpoch.SolveSeq = 1
	newEpoch.SolvedAt = "2026-10-09T12:00:02Z"
	if _, err := model.ApplyMeasurements(context.Background(), newEpoch, testActor); err != nil {
		t.Fatalf("new solver epoch did not accept sequence reset: %v", err)
	}

	stale := newEpoch
	stale.Revision--
	stale.SolveSeq++
	stale.SolvedAt = "2026-10-09T12:00:03Z"
	if _, err := model.ApplyMeasurements(context.Background(), stale, testActor); err != MeasurementStaleTruth {
		t.Fatalf("stale truth error = %v, want stale_truth", err)
	}
}

func TestFirstSolveOncePerRevisionAndValidityTransitions(t *testing.T) {
	model := newTestModel(t, time.Now)
	input := testMeasurement(model, "solver-a", 1)
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}
	input.SolveSeq = 2
	input.SolvedAt = "2026-10-09T12:00:01Z"
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}
	if result := model.Command(context.Background(), CommandOpen, AuditContext{Peer: "rtu", Protocol: "modbus", Zone: "station"}); !result.Accepted {
		t.Fatalf("open command = %+v", result)
	}
	input = testMeasurement(model, "solver-a", 3)
	input.SolvedAt = "2026-10-09T12:00:02Z"
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}
	entries, _, _ := model.AuditAfter(0)
	firstSolves, validTransitions := 0, 0
	firstSolveSeqs := make(map[uint64]bool)
	firstSolveRevisions := make(map[uint64]bool)
	for _, entry := range entries {
		if entry.Operation == "first_solve" {
			firstSolves++
			detail, ok := entry.Detail.(map[string]any)
			if !ok || detail["solver_epoch"] != input.SolverEpoch || detail["current_a"] != float64(28.9) {
				t.Errorf("first_solve detail = %#v", entry.Detail)
				continue
			}
			firstSolveSeqs[detail["solve_seq"].(uint64)] = true
			firstSolveRevisions[detail["revision"].(uint64)] = true
		}
		if entry.Operation == "measurements_valid" {
			validTransitions++
		}
	}
	if firstSolves != 2 || validTransitions != 1 {
		t.Fatalf("first_solve count=%d valid transition count=%d, want 2 and 1", firstSolves, validTransitions)
	}
	if !firstSolveSeqs[1] || !firstSolveSeqs[3] || !firstSolveRevisions[1] || !firstSolveRevisions[2] {
		t.Fatalf("first_solve detail seqs=%#v revisions=%#v, want seq 1/3 on revisions 1/2", firstSolveSeqs, firstSolveRevisions)
	}
}

func TestMeasurementsExpireAtThreeSecondsAndReplayDoesNotRenew(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	model := newTestModel(t, func() time.Time { return now })
	input := testMeasurement(model, "solver-a", 1)
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Second)
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != MeasurementReplay {
		t.Fatalf("replay error = %v, want replay", err)
	}
	now = now.Add(time.Second)
	if snapshot := model.Snapshot(); snapshot.Measurements.Valid {
		t.Fatalf("measurement remained valid at 3 seconds: %+v", snapshot.Measurements)
	}
	entries, _, _ := model.AuditAfter(0)
	invalidTransitions := 0
	for _, entry := range entries {
		if entry.Operation == "measurements_invalid" {
			invalidTransitions++
		}
	}
	if invalidTransitions != 1 {
		t.Fatalf("measurements_invalid count = %d, want 1", invalidTransitions)
	}
}

func TestStaleTruthDoesNotRenewMeasurementValidity(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	model := newTestModel(t, func() time.Time { return now })
	input := testMeasurement(model, "solver-a", 1)
	if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	stale := input
	stale.Revision++
	if _, err := model.ApplyMeasurements(context.Background(), stale, testActor); err != MeasurementStaleTruth {
		t.Fatalf("stale error = %v, want stale_truth", err)
	}
	now = now.Add(time.Second)
	if snapshot := model.Snapshot(); snapshot.Measurements.Valid {
		t.Fatal("stale solve renewed the validity window")
	}
}

func TestMeasurementInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		edit func(*MeasurementInput)
	}{
		{name: "wrong schema", edit: func(input *MeasurementInput) { input.Schema = 2 }},
		{name: "missing epoch", edit: func(input *MeasurementInput) { input.SolverEpoch = "" }},
		{name: "zero sequence", edit: func(input *MeasurementInput) { input.SolveSeq = 0 }},
		{name: "negative current", edit: func(input *MeasurementInput) { input.CurrentA = -1 }},
		{name: "nan voltage", edit: func(input *MeasurementInput) { input.VoltageKV = math.NaN() }},
		{name: "float32 overflow", edit: func(input *MeasurementInput) { input.CurrentA = math.MaxFloat64 }},
		{name: "non-UTC timestamp", edit: func(input *MeasurementInput) { input.SolvedAt = "2026-10-09T08:00:00-04:00" }},
		{name: "bad timestamp", edit: func(input *MeasurementInput) { input.SolvedAt = "not-time" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newTestModel(t, time.Now)
			input := testMeasurement(model, "solver-a", 1)
			test.edit(&input)
			if _, err := model.ApplyMeasurements(context.Background(), input, testActor); err != MeasurementInvalid {
				t.Fatalf("invalid measurement error = %v, want invalid", err)
			}
			if model.Snapshot().Measurements.Valid {
				t.Fatal("invalid measurements were accepted")
			}
		})
	}
}

func TestAuditRingIsBoundedAndZoneMappingUsesConfiguredCIDRs(t *testing.T) {
	model := newTestModel(t, time.Now)
	for i := 0; i < 510; i++ {
		if result := model.Command(context.Background(), CommandClose, testActor); !result.Accepted {
			t.Fatalf("no-op command %d rejected: %+v", i, result)
		}
	}
	entries, latest, oldest := model.AuditAfter(0)
	if len(entries) != AuditCapacity || latest == nil || oldest == nil || *latest != 510 || *oldest != 11 {
		t.Fatalf("bounded audit: len=%d latest=%v oldest=%v", len(entries), latest, oldest)
	}
	if after, _, _ := model.AuditAfter(509); len(after) != 1 || after[0].Seq != 510 {
		t.Fatalf("audit after sequence 509 = %+v", after)
	}

	zones, err := ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24")
	if err != nil {
		t.Fatal(err)
	}
	for peer, want := range map[string]string{
		"10.40.40.20:502":  "field",
		"10.30.30.20:502":  "station",
		"10.50.50.20:8080": "process",
		"10.99.99.22:8080": "mgmt",
		"192.0.2.1:1234":   "unknown",
	} {
		if got := zones.ForPeer(peer); got != want {
			t.Errorf("zone for %s = %q, want %q", peer, got, want)
		}
	}
	if _, err := ParseZones("field=not-a-cidr"); err == nil {
		t.Error("invalid zone CIDR was accepted")
	}
}
