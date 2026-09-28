package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApplyPhysicsReplyFailuresRetainLastGoodElectrical(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{name: "transport error", err: errors.New("connection refused")},
		{name: "non-success status", status: 503, body: `{"error":"power flow did not converge"}`},
		{name: "invalid JSON", status: 200, body: `{`},
		{name: "array instead of object", status: 200, body: `[]`},
		{name: "null instead of object", status: 200, body: `null`},
		{name: "missing convergence", status: 200, body: `{"solved_at":"new"}`},
		{name: "false convergence", status: 200, body: `{"converged":false}`},
		{name: "non-boolean convergence", status: 200, body: `{"converged":"true"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goodAt := time.Date(2026, time.September, 27, 14, 3, 11, 0, time.UTC)
			previousElectrical := map[string]any{"critical_load_voltage_v": float64(119)}
			state := &AggregatedState{
				Electrical: previousElectrical,
				Physics: PhysicsStatus{
					LastGoodAt: goodAt,
					SolvedAt:   "last-good",
				},
			}

			state.mu.Lock()
			err := applyPhysicsReply(state, tt.status, []byte(tt.body), tt.err, goodAt.Add(time.Second))
			state.mu.Unlock()

			if err == nil {
				t.Fatal("applyPhysicsReply() error = nil, want failure")
			}
			if state.Physics.ConsecutiveFailures != 1 {
				t.Errorf("consecutive failures = %d, want 1", state.Physics.ConsecutiveFailures)
			}
			if state.Physics.LastError == "" {
				t.Error("last error is empty after a failed push")
			}
			if state.Physics.Stale {
				t.Error("physics became stale before three failures after a good solve")
			}
			if !state.Physics.LastGoodAt.Equal(goodAt) || state.Physics.SolvedAt != "last-good" {
				t.Errorf("last-good metadata changed on failure: %+v", state.Physics)
			}
			if !reflect.DeepEqual(state.Electrical, previousElectrical) {
				t.Errorf("electrical state changed on failure: %#v", state.Electrical)
			}
		})
	}
}

func TestApplyPhysicsReplyStaleThresholdAndRecovery(t *testing.T) {
	goodAt := time.Date(2026, time.September, 27, 14, 3, 11, 0, time.UTC)
	state := &AggregatedState{
		Electrical: map[string]any{"critical_load_voltage_v": float64(119)},
		Physics: PhysicsStatus{
			LastGoodAt: goodAt,
			SolvedAt:   "last-good",
		},
	}

	state.mu.Lock()
	for i := 0; i < physicsStaleAfter; i++ {
		if err := applyPhysicsReply(state, 0, nil, errors.New("offline"), goodAt.Add(time.Duration(i)*time.Second)); err == nil {
			t.Fatal("applyPhysicsReply() error = nil, want transport failure")
		}
	}
	state.mu.Unlock()

	if !state.Physics.Stale || state.Physics.ConsecutiveFailures != physicsStaleAfter {
		t.Errorf("physics after threshold = %+v, want stale after %d failures", state.Physics, physicsStaleAfter)
	}

	newAt := goodAt.Add(time.Minute)
	reply, err := json.Marshal(map[string]any{
		"converged":               true,
		"solved_at":               "2026-09-27T14:04:11.000Z",
		"critical_load_voltage_v": float64(121),
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	if err := applyPhysicsReply(state, 200, reply, nil, newAt); err != nil {
		t.Fatalf("applyPhysicsReply() error = %v, want success", err)
	}
	state.mu.Unlock()

	if state.Physics.Stale || state.Physics.ConsecutiveFailures != 0 || state.Physics.LastError != "" {
		t.Errorf("physics did not recover after a good solve: %+v", state.Physics)
	}
	if !state.Physics.LastGoodAt.Equal(newAt) || state.Physics.SolvedAt != "2026-09-27T14:04:11.000Z" {
		t.Errorf("recovered physics metadata = %+v", state.Physics)
	}
	if got := state.Electrical["critical_load_voltage_v"]; got != float64(121) {
		t.Errorf("electrical voltage = %#v, want 121", got)
	}
}

func TestAutoControlDecisionsSuspendedWhenPhysicsStale(t *testing.T) {
	tests := []struct {
		name    string
		physics PhysicsStatus
	}{
		{name: "before first successful solve"},
		{
			name: "three consecutive failed pushes",
			physics: PhysicsStatus{
				LastGoodAt:          time.Now(),
				ConsecutiveFailures: physicsStaleAfter,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRTACState()
			agg.Physics = tt.physics
			agg.Devices["regulator"] = map[string]any{"manual_mode": false}
			agg.Devices["capbank"] = map[string]any{"auto_mode": true}
			agg.Electrical = map[string]any{
				"critical_load_energized": true,
				"critical_load_voltage_v": float64(110),
				"general_load_energized":  true,
				"power_factor":            float64(0.8),
				"downstream_voltage_v":    float64(120),
			}
			if got := autoControlDecisions(); len(got) != 0 {
				t.Errorf("autoControlDecisions() = %#v, want no commands while stale", got)
			}
		})
	}
}

func TestPhysicsFieldsExposedByStateTagsAndHealth(t *testing.T) {
	resetRTACState()
	agg.Electrical = map[string]any{"converged": false, "critical_load_voltage_v": float64(119)}

	stateResponse := httptest.NewRecorder()
	handleRawState(stateResponse, httptest.NewRequest("GET", "/api/state", nil))
	var state struct {
		Physics PhysicsStatus `json:"physics"`
	}
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode state response: %v", err)
	}
	if !state.Physics.Stale {
		t.Errorf("initial /api/state physics stale = false, want true")
	}

	tagsResponse := httptest.NewRecorder()
	handleTags(tagsResponse, httptest.NewRequest("GET", "/api/tags", nil))
	var tags struct {
		Tags map[string]any `json:"tags"`
	}
	if err := json.Unmarshal(tagsResponse.Body.Bytes(), &tags); err != nil {
		t.Fatalf("decode tags response: %v", err)
	}
	for tag, want := range map[string]any{
		"physics.stale":                true,
		"physics.consecutive_failures": float64(0),
		"physics.solved_at":            "",
		"physics.last_error":           "",
		"alarm.physics_stale":          true,
	} {
		if got := tags.Tags[tag]; got != want {
			t.Errorf("tag %q = %#v, want %#v", tag, got, want)
		}
	}

	healthResponse := httptest.NewRecorder()
	handleHealth(healthResponse, httptest.NewRequest("GET", "/api/health", nil))
	var health struct {
		Status  string        `json:"status"`
		Physics PhysicsStatus `json:"physics"`
	}
	if err := json.Unmarshal(healthResponse.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if health.Status != "degraded" || !health.Physics.Stale {
		t.Errorf("initial health = status %q physics %+v, want degraded/stale", health.Status, health.Physics)
	}

	agg.mu.Lock()
	err := applyPhysicsReply(agg, 200, []byte(`{"converged":true,"solved_at":"2026-09-27T14:03:11.412Z"}`), nil, time.Now())
	agg.mu.Unlock()
	if err != nil {
		t.Fatalf("apply fresh physics reply: %v", err)
	}
	healthResponse = httptest.NewRecorder()
	handleHealth(healthResponse, httptest.NewRequest("GET", "/api/health", nil))
	if err := json.Unmarshal(healthResponse.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode recovered health response: %v", err)
	}
	if health.Status != "ok" || health.Physics.Stale {
		t.Errorf("recovered health = status %q physics %+v, want ok/fresh", health.Status, health.Physics)
	}
}

func TestApplyPhysicsReplyRejectsOpenDSS503AndUsesResponseMessage(t *testing.T) {
	state := &AggregatedState{Electrical: map[string]any{"last-good": true}}
	state.mu.Lock()
	err := applyPhysicsReply(state, 503, []byte(`{"error":"power flow did not converge","converged":false}`), nil, time.Now())
	state.mu.Unlock()
	if err == nil || !strings.Contains(state.Physics.LastError, "503") {
		t.Errorf("503 failure error = %v, last_error = %q", err, state.Physics.LastError)
	}
	if _, ok := state.Electrical["last-good"]; !ok {
		t.Errorf("503 failure replaced last good electrical map: %#v", state.Electrical)
	}
}
