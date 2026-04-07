package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/services/shared"
)

func newRecloserMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	return mux
}

func resetRecloserState() {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Closed = true
	state.RecloseEnabled = true
	state.ShotCount = 0
	state.Lockout = false
	state.FaultSeen = false
	state.AutoMode = true
	state.CommsOK = true
	state.LastCommandSource = ""
	audit = shared.NewAuditLog(100)
}

func postRecloserCmd(ts *httptest.Server, cmd string) map[string]any {
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

func getRecloserJSON(ts *httptest.Server, path string) map[string]any {
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

func TestRecloserHealth(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := getRecloserJSON(ts, "/api/health")
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
	if result["service"] != "recloser-sim" {
		t.Errorf("expected service recloser-sim, got %v", result["service"])
	}
}

func TestRecloserInitialState(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := getRecloserJSON(ts, "/api/state")
	if result["closed"] != true {
		t.Errorf("expected closed=true, got %v", result["closed"])
	}
	if result["reclose_enabled"] != true {
		t.Errorf("expected reclose_enabled=true, got %v", result["reclose_enabled"])
	}
	if result["auto_mode"] != true {
		t.Errorf("expected auto_mode=true, got %v", result["auto_mode"])
	}
	if result["lockout"] != false {
		t.Errorf("expected lockout=false, got %v", result["lockout"])
	}
}

func TestRecloserOpen(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := postRecloserCmd(ts, "open")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRecloserJSON(ts, "/api/state")
	if st["closed"] != false {
		t.Errorf("expected closed=false after open, got %v", st["closed"])
	}
}

func TestRecloserDisableReclose(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := postRecloserCmd(ts, "disable_reclose")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRecloserJSON(ts, "/api/state")
	if st["reclose_enabled"] != false {
		t.Errorf("expected reclose_enabled=false, got %v", st["reclose_enabled"])
	}
}

func TestRecloserCloseWhenLockout(t *testing.T) {
	resetRecloserState()
	state.mu.Lock()
	state.Lockout = true
	state.Closed = false
	state.mu.Unlock()

	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := postRecloserCmd(ts, "close")
	if result["result"] != "rejected" {
		t.Errorf("expected rejected, got %v", result["result"])
	}
	if result["detail"] != "lockout active" {
		t.Errorf("expected 'lockout active', got %v", result["detail"])
	}
}

func TestRecloserUnknownCommand(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := postRecloserCmd(ts, "bogus")
	if result["result"] != "rejected" {
		t.Errorf("expected rejected, got %v", result["result"])
	}
	if result["detail"] != "unknown command" {
		t.Errorf("expected 'unknown command', got %v", result["detail"])
	}
}

func TestRecloserAuditRecords(t *testing.T) {
	resetRecloserState()
	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	postRecloserCmd(ts, "open")
	postRecloserCmd(ts, "close")

	result := getRecloserJSON(ts, "/api/audit")
	entries, ok := result["entries"].([]any)
	if !ok {
		t.Fatalf("expected entries array, got %T", result["entries"])
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 audit entries, got %d", len(entries))
	}
}

func TestRecloserResetLockout(t *testing.T) {
	resetRecloserState()
	state.mu.Lock()
	state.Lockout = true
	state.Closed = false
	state.ShotCount = 3
	state.mu.Unlock()

	ts := httptest.NewServer(newRecloserMux())
	defer ts.Close()

	result := postRecloserCmd(ts, "reset_lockout")
	if result["result"] != "executed" {
		t.Fatalf("expected executed, got %v", result["result"])
	}

	st := getRecloserJSON(ts, "/api/state")
	if st["lockout"] != false {
		t.Errorf("expected lockout=false after reset, got %v", st["lockout"])
	}
}
