package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/services/shared"
)

func newRegulatorMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	return mux
}

func resetRegulatorState() {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.TapPosition = 0
	state.VoltageSetpoint = 120.0
	state.ManualMode = false
	state.CommsOK = true
	state.Alarm = false
	state.LastCommandSource = ""
	audit = shared.NewAuditLog(100)
}

func postRegulatorCmd(ts *httptest.Server, cmd string, value float64) map[string]any {
	body, _ := json.Marshal(shared.CommandRequest{Command: cmd, Source: "test", Value: value})
	resp, err := http.Post(ts.URL+"/api/command", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func getRegulatorJSON(ts *httptest.Server, path string) map[string]any {
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func TestRegulatorHealth(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := getRegulatorJSON(ts, "/api/health")
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
	if result["service"] != "regulator-sim" {
		t.Errorf("expected service regulator-sim, got %v", result["service"])
	}
}

func TestRegulatorInitialState(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := getRegulatorJSON(ts, "/api/state")
	if result["tap_position"] != float64(0) {
		t.Errorf("expected tap_position=0, got %v", result["tap_position"])
	}
	if result["voltage_setpoint_v"] != float64(120.0) {
		t.Errorf("expected voltage_setpoint_v=120.0, got %v", result["voltage_setpoint_v"])
	}
}

func TestRegulatorRaiseTap(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "raise_tap", 0)
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRegulatorJSON(ts, "/api/state")
	if st["tap_position"] != float64(1) {
		t.Errorf("expected tap_position=1, got %v", st["tap_position"])
	}
}

func TestRegulatorLowerTap(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "lower_tap", 0)
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRegulatorJSON(ts, "/api/state")
	if st["tap_position"] != float64(-1) {
		t.Errorf("expected tap_position=-1, got %v", st["tap_position"])
	}
}

func TestRegulatorRaiseTapAtMax(t *testing.T) {
	resetRegulatorState()
	state.mu.Lock()
	state.TapPosition = 16
	state.mu.Unlock()

	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "raise_tap", 0)
	if result["result"] != "rejected" {
		t.Errorf("expected rejected at max tap, got %v", result["result"])
	}
	if result["detail"] != "already at max tap" {
		t.Errorf("expected 'already at max tap', got %v", result["detail"])
	}
}

func TestRegulatorLowerTapAtMin(t *testing.T) {
	resetRegulatorState()
	state.mu.Lock()
	state.TapPosition = -16
	state.mu.Unlock()

	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "lower_tap", 0)
	if result["result"] != "rejected" {
		t.Errorf("expected rejected at min tap, got %v", result["result"])
	}
	if result["detail"] != "already at min tap" {
		t.Errorf("expected 'already at min tap', got %v", result["detail"])
	}
}

func TestRegulatorSetTap(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "set_tap", 5)
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRegulatorJSON(ts, "/api/state")
	if st["tap_position"] != float64(5) {
		t.Errorf("expected tap_position=5, got %v", st["tap_position"])
	}
}

func TestRegulatorSetTapOutOfRange(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "set_tap", 20)
	if result["result"] != "rejected" {
		t.Errorf("expected rejected for out-of-range tap, got %v", result["result"])
	}
	if result["detail"] != "tap out of range" {
		t.Errorf("expected 'tap out of range', got %v", result["detail"])
	}
}

func TestRegulatorUnknownCommand(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	result := postRegulatorCmd(ts, "bogus", 0)
	if result["result"] != "rejected" {
		t.Errorf("expected rejected, got %v", result["result"])
	}
}

func TestRegulatorAuditRecords(t *testing.T) {
	resetRegulatorState()
	ts := httptest.NewServer(newRegulatorMux())
	defer ts.Close()

	postRegulatorCmd(ts, "raise_tap", 0)
	postRegulatorCmd(ts, "lower_tap", 0)
	postRegulatorCmd(ts, "set_tap", 3)

	result := getRegulatorJSON(ts, "/api/audit")
	entries, ok := result["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", result["entries"])
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 audit entries, got %d", len(entries))
	}
}
