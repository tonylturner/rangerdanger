package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/services/shared"
)

func newRelayMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	return mux
}

func resetRelayState() {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.BreakerClosed = true
	state.RemoteControlEnabled = true
	state.Lockout = false
	state.FaultSeen = false
	state.MeasuredCurrent = 120.0
	state.MeasuredVoltage = 12.47
	state.CommsOK = true
	state.LastCommandSource = ""
	// Reset audit log
	audit = shared.NewAuditLog(100)
}

func postCommand(ts *httptest.Server, cmd string) map[string]any {
	body, _ := json.Marshal(shared.CommandRequest{Command: cmd, Source: "test"})
	resp, err := http.Post(ts.URL+"/api/command", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func getJSON(ts *httptest.Server, path string) map[string]any {
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func TestRelayHealth(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := getJSON(ts, "/api/health")
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
	if result["service"] != "relay-sim" {
		t.Errorf("expected service relay-sim, got %v", result["service"])
	}
}

func TestRelayInitialState(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := getJSON(ts, "/api/state")
	if result["breaker_closed"] != true {
		t.Errorf("expected breaker_closed=true, got %v", result["breaker_closed"])
	}
	if result["remote_control_enabled"] != true {
		t.Errorf("expected remote_control_enabled=true, got %v", result["remote_control_enabled"])
	}
	if result["comms_ok"] != true {
		t.Errorf("expected comms_ok=true, got %v", result["comms_ok"])
	}
	if result["lockout"] != false {
		t.Errorf("expected lockout=false, got %v", result["lockout"])
	}
}

func TestRelayTrip(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := postCommand(ts, "trip")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getJSON(ts, "/api/state")
	if st["breaker_closed"] != false {
		t.Errorf("expected breaker_closed=false after trip, got %v", st["breaker_closed"])
	}
}

func TestRelayClose(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	// Trip first
	postCommand(ts, "trip")
	// Then close
	result := postCommand(ts, "close")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getJSON(ts, "/api/state")
	if st["breaker_closed"] != true {
		t.Errorf("expected breaker_closed=true after close, got %v", st["breaker_closed"])
	}
}

func TestRelayTripRemoteDisabled(t *testing.T) {
	resetRelayState()
	state.mu.Lock()
	state.RemoteControlEnabled = false
	state.mu.Unlock()

	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := postCommand(ts, "trip")
	if result["result"] != "rejected" {
		t.Errorf("expected rejected, got %v", result["result"])
	}
	if result["detail"] != "remote control disabled" {
		t.Errorf("expected 'remote control disabled', got %v", result["detail"])
	}

	// Breaker should still be closed
	st := getJSON(ts, "/api/state")
	if st["breaker_closed"] != true {
		t.Errorf("breaker should remain closed when remote control disabled")
	}
}

func TestRelayLockout(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := postCommand(ts, "lockout")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getJSON(ts, "/api/state")
	if st["lockout"] != true {
		t.Errorf("expected lockout=true, got %v", st["lockout"])
	}
	if st["breaker_closed"] != false {
		t.Errorf("expected breaker_closed=false after lockout, got %v", st["breaker_closed"])
	}
}

func TestRelayTripDuringLockout(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	postCommand(ts, "lockout")
	result := postCommand(ts, "close")
	if result["result"] != "rejected" {
		t.Errorf("expected rejected during lockout, got %v", result["result"])
	}
	if result["detail"] != "lockout active" {
		t.Errorf("expected 'lockout active', got %v", result["detail"])
	}
}

func TestRelayUnknownCommand(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	result := postCommand(ts, "bogus")
	if result["result"] != "rejected" {
		t.Errorf("expected rejected, got %v", result["result"])
	}
	if result["detail"] != "unknown command" {
		t.Errorf("expected 'unknown command', got %v", result["detail"])
	}
}

func TestRelayAuditRecords(t *testing.T) {
	resetRelayState()
	ts := httptest.NewServer(newRelayMux())
	defer ts.Close()

	postCommand(ts, "trip")
	postCommand(ts, "close")
	postCommand(ts, "bogus")

	result := getJSON(ts, "/api/audit")
	entries, ok := result["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", result["entries"])
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 audit entries, got %d", len(entries))
	}
}
