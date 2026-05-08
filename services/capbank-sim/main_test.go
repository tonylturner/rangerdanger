package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/services/shared"
)

// resetState restores the capbank to its default operating shape +
// drains the audit log between tests. The handlers operate on
// package-level state so test isolation depends on this — call it
// at the start of every test that mutates state.
func resetState(t *testing.T) {
	t.Helper()
	state.mu.Lock()
	state.SwitchedIn = false
	state.AutoMode = true
	state.KvarRating = 300.0
	state.VoltageThreshLow = 114.0
	state.VoltageThreshHigh = 126.0
	state.CommsOK = true
	state.Alarm = false
	state.SwitchCount = 0
	state.Lockout = false
	state.LastCommandSource = ""
	state.mu.Unlock()
	audit = shared.NewAuditLog(100)
}

// invoke runs an HTTP handler against an in-memory request/response
// and decodes the response body into a generic map. Trims the
// per-test boilerplate.
func invoke(t *testing.T, handler http.HandlerFunc, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var bodyReader *bytes.Buffer
	if body != nil {
		j, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewBuffer(j)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	rec := httptest.NewRecorder()
	handler(rec, req)

	parsed := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

// ── /api/health ─────────────────────────────────────────────────────

func TestHandleHealth(t *testing.T) {
	resetState(t)
	rec, body := invoke(t, handleHealth, "GET", "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body["status"] != "ok" || body["service"] != "capbank-sim" {
		t.Errorf("body = %+v, want status=ok service=capbank-sim", body)
	}
}

// ── /api/state ──────────────────────────────────────────────────────

func TestHandleState_Defaults(t *testing.T) {
	resetState(t)
	rec, body := invoke(t, handleState, "GET", "/api/state", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// switched_in / auto_mode / lockout pinned by the reset;
	// kvar_rating is a model parameter, not lab-tunable.
	if body["switched_in"] != false {
		t.Errorf("switched_in = %v, want false", body["switched_in"])
	}
	if body["auto_mode"] != true {
		t.Errorf("auto_mode = %v, want true", body["auto_mode"])
	}
	if body["lockout"] != false {
		t.Errorf("lockout = %v, want false", body["lockout"])
	}
	if body["kvar_rating"] != 300.0 {
		t.Errorf("kvar_rating = %v, want 300.0", body["kvar_rating"])
	}
}

// ── /api/command — happy paths ──────────────────────────────────────

func TestHandleCommand_SwitchInThenOut(t *testing.T) {
	resetState(t)

	rec, body := invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "switch_in", Source: "rtac-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch_in status = %d, want 200", rec.Code)
	}
	if body["result"] != "executed" {
		t.Errorf("switch_in result = %v, want executed", body["result"])
	}
	state.mu.RLock()
	if !state.SwitchedIn || state.SwitchCount != 1 {
		t.Errorf("after switch_in: SwitchedIn=%v SwitchCount=%d, want true/1", state.SwitchedIn, state.SwitchCount)
	}
	state.mu.RUnlock()

	rec, body = invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "switch_out", Source: "rtac-1"})
	if rec.Code != http.StatusOK || body["result"] != "executed" {
		t.Fatalf("switch_out failed: code=%d body=%+v", rec.Code, body)
	}
	state.mu.RLock()
	if state.SwitchedIn || state.SwitchCount != 2 {
		t.Errorf("after switch_out: SwitchedIn=%v SwitchCount=%d, want false/2", state.SwitchedIn, state.SwitchCount)
	}
	state.mu.RUnlock()
}

func TestHandleCommand_SetAutoSetManual(t *testing.T) {
	resetState(t)

	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "set_manual"})
	state.mu.RLock()
	if state.AutoMode {
		t.Errorf("after set_manual: AutoMode=true, want false")
	}
	state.mu.RUnlock()

	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "set_auto"})
	state.mu.RLock()
	if !state.AutoMode {
		t.Errorf("after set_auto: AutoMode=false, want true")
	}
	state.mu.RUnlock()
}

func TestHandleCommand_SetThresholds(t *testing.T) {
	resetState(t)

	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "set_thresh_low", Value: 110.5})
	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "set_thresh_high", Value: 128.0})

	state.mu.RLock()
	if state.VoltageThreshLow != 110.5 {
		t.Errorf("VoltageThreshLow = %v, want 110.5", state.VoltageThreshLow)
	}
	if state.VoltageThreshHigh != 128.0 {
		t.Errorf("VoltageThreshHigh = %v, want 128.0", state.VoltageThreshHigh)
	}
	state.mu.RUnlock()
}

// ── /api/command — guard rails ──────────────────────────────────────

func TestHandleCommand_RejectsDoubleSwitchIn(t *testing.T) {
	resetState(t)
	state.mu.Lock()
	state.SwitchedIn = true // pre-switched
	state.mu.Unlock()

	rec, body := invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "switch_in"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (semantic reject still 200)", rec.Code)
	}
	if body["result"] != "rejected" {
		t.Errorf("result = %v, want rejected", body["result"])
	}
}

func TestHandleCommand_LockoutAfterSwitchCountExceeded(t *testing.T) {
	resetState(t)
	// maxSwitchCount = 6. Walk it up: switch_in / switch_out alternate.
	commands := []string{"switch_in", "switch_out", "switch_in", "switch_out", "switch_in", "switch_out"}
	for _, c := range commands {
		invoke(t, handleCommand, "POST", "/api/command",
			shared.CommandRequest{Command: c, Source: "rtac-1"})
	}

	state.mu.RLock()
	if !state.Lockout {
		t.Errorf("expected Lockout=true after %d operations, got false", len(commands))
	}
	if !state.Alarm {
		t.Errorf("expected Alarm=true at lockout, got false")
	}
	state.mu.RUnlock()

	// Further operations rejected.
	rec, body := invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "switch_in"})
	if rec.Code != http.StatusOK || body["result"] != "rejected" {
		t.Errorf("post-lockout switch_in: code=%d result=%v, want 200/rejected",
			rec.Code, body["result"])
	}
}

func TestHandleCommand_ResetLockoutClearsCounter(t *testing.T) {
	resetState(t)
	state.mu.Lock()
	state.Lockout = true
	state.Alarm = true
	state.SwitchCount = 6
	state.mu.Unlock()

	rec, body := invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "reset_lockout"})
	if rec.Code != http.StatusOK || body["result"] != "executed" {
		t.Fatalf("reset_lockout: code=%d body=%+v", rec.Code, body)
	}
	state.mu.RLock()
	if state.Lockout || state.Alarm || state.SwitchCount != 0 {
		t.Errorf("reset_lockout left state dirty: lockout=%v alarm=%v count=%d",
			state.Lockout, state.Alarm, state.SwitchCount)
	}
	state.mu.RUnlock()
}

func TestHandleCommand_RejectsUnknownCommand(t *testing.T) {
	resetState(t)
	rec, body := invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "explode_substation"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown command is a semantic reject, not transport error)", rec.Code)
	}
	if body["result"] != "rejected" || body["detail"] != "unknown command" {
		t.Errorf("body = %+v, want result=rejected detail=unknown command", body)
	}
}

func TestHandleCommand_RejectsBadJSON(t *testing.T) {
	resetState(t)
	req := httptest.NewRequest("POST", "/api/command", bytes.NewBufferString("{not-json"))
	rec := httptest.NewRecorder()
	handleCommand(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON", rec.Code)
	}
}

// ── /api/audit ──────────────────────────────────────────────────────

func TestHandleAudit_ReflectsCommands(t *testing.T) {
	resetState(t)

	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "switch_in", Source: "rtac-1"})
	invoke(t, handleCommand, "POST", "/api/command",
		shared.CommandRequest{Command: "set_manual", Source: "rtac-1"})

	rec, body := invoke(t, handleAudit, "GET", "/api/audit", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	entries, ok := body["entries"].([]any)
	if !ok {
		t.Fatalf("entries not an array: %+v", body)
	}
	if len(entries) != 2 {
		t.Errorf("entries count = %d, want 2", len(entries))
	}
}
