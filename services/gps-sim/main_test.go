package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

func resetGPSState() {
	state = &GPSState{
		SyncStatus:     "locked",
		SatelliteCount: 9,
		IRIGB:          true,
		NTPEnabled:     true,
		PTPEnabled:     false,
		CommsOK:        true,
	}
	audit = shared.NewAuditLog(100)
}

func newGPSMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	return mux
}

func postGPSCommand(mux *http.ServeMux, command string, value float64, source string) *httptest.ResponseRecorder {
	body, err := json.Marshal(shared.CommandRequest{Command: command, Value: value, Source: source})
	if err != nil {
		panic(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewReader(body)))
	return response
}

func TestGPSSetOffset(t *testing.T) {
	tests := []struct {
		name       string
		value      float64
		wantAlarm  bool
		wantImpact string
	}{
		{name: "small offset", value: 0.5},
		{name: "spoofing threshold exceeded", value: 2.5, wantAlarm: true, wantImpact: "SOE timestamps will be incorrect — forensic data corrupted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGPSState()
			response := postGPSCommand(newGPSMux(), "set_offset", tt.value, "10.30.30.45")
			if response.Code != http.StatusOK {
				t.Fatalf("set_offset status = %d, want %d", response.Code, http.StatusOK)
			}
			if state.TimeOffsetSec != tt.value || state.LastCommandSource != "10.30.30.45" || state.Alarm != tt.wantAlarm {
				t.Errorf("GPS state after set_offset = {offset:%v source:%q alarm:%v}", state.TimeOffsetSec, state.LastCommandSource, state.Alarm)
			}
			entries := audit.Entries()
			if len(entries) != 1 {
				t.Fatalf("audit entries = %d, want 1", len(entries))
			}
			if entries[0].Result != "executed" || entries[0].Source != "10.30.30.45" || entries[0].ProcessImpact != tt.wantImpact {
				t.Errorf("audit entry = %+v, want executed command from source and impact %q", entries[0], tt.wantImpact)
			}
		})
	}
}

func TestGPSSetSatellites(t *testing.T) {
	tests := []struct {
		name       string
		count      float64
		wantStatus string
		wantAlarm  bool
		wantResult string
		wantCount  int
		wantSource string
	}{
		{name: "zero enters holdover", count: 0, wantStatus: "holdover", wantAlarm: true, wantResult: "executed", wantCount: 0, wantSource: "test"},
		{name: "three enters freerun", count: 3, wantStatus: "freerun", wantAlarm: true, wantResult: "executed", wantCount: 3, wantSource: "test"},
		{name: "four locks", count: 4, wantStatus: "locked", wantResult: "executed", wantCount: 4, wantSource: "test"},
		{name: "thirteen rejected", count: 13, wantStatus: "locked", wantResult: "rejected", wantCount: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetGPSState()
			response := postGPSCommand(newGPSMux(), "set_satellites", tt.count, "test")
			if response.Code != http.StatusOK {
				t.Fatalf("set_satellites status = %d, want %d", response.Code, http.StatusOK)
			}
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result["result"] != tt.wantResult {
				t.Errorf("result = %v, want %q", result["result"], tt.wantResult)
			}
			if state.SatelliteCount != tt.wantCount || state.SyncStatus != tt.wantStatus || state.Alarm != tt.wantAlarm || state.LastCommandSource != tt.wantSource {
				t.Errorf("GPS state = {satellites:%d status:%q alarm:%v source:%q}, want {%d %q %v %q}", state.SatelliteCount, state.SyncStatus, state.Alarm, state.LastCommandSource, tt.wantCount, tt.wantStatus, tt.wantAlarm, tt.wantSource)
			}
			if tt.count == 0 && state.HoldoverStarted == "" {
				t.Error("zero satellites did not record holdover start")
			}
			entries := audit.Entries()
			if len(entries) != 1 || entries[0].Result != tt.wantResult {
				t.Errorf("audit entries = %+v, want one %s entry", entries, tt.wantResult)
			}
		})
	}
}

func TestGPSJamAndRestore(t *testing.T) {
	resetGPSState()
	mux := newGPSMux()

	jam := postGPSCommand(mux, "jam_gps", 0, "attacker")
	if jam.Code != http.StatusOK {
		t.Fatalf("jam_gps status = %d, want %d", jam.Code, http.StatusOK)
	}
	if state.SyncStatus != "holdover" || state.SatelliteCount != 0 || state.HoldoverStarted == "" || state.HoldoverDriftPPM != 0.1 || !state.Alarm {
		t.Errorf("state after jam = %+v, want holdover, 0 satellites, drift, and alarm", state)
	}

	restore := postGPSCommand(mux, "restore_gps", 0, "operator")
	if restore.Code != http.StatusOK {
		t.Fatalf("restore_gps status = %d, want %d", restore.Code, http.StatusOK)
	}
	if state.SyncStatus != "locked" || state.SatelliteCount != 9 || state.HoldoverStarted != "" || state.HoldoverDriftPPM != 0 || state.Alarm || state.LastCommandSource != "operator" {
		t.Errorf("state after restore = %+v, want locked GPS state with holdover cleared", state)
	}
	entries := audit.Entries()
	if len(entries) != 2 || entries[0].ProcessImpact != "time accuracy degrading — SOE records may drift" || entries[1].Result != "executed" {
		t.Errorf("audit entries = %+v, want jam impact followed by restore", entries)
	}
}

func TestGPSUnknownAndMalformedCommand(t *testing.T) {
	resetGPSState()
	mux := newGPSMux()
	unknown := postGPSCommand(mux, "not_a_command", 0, "test")
	if unknown.Code != http.StatusOK {
		t.Fatalf("unknown command HTTP status = %d, want %d", unknown.Code, http.StatusOK)
	}
	var result map[string]any
	if err := json.Unmarshal(unknown.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode unknown-command response: %v", err)
	}
	if result["result"] != "rejected" || result["detail"] != "unknown command" {
		t.Errorf("unknown-command response = %#v, want rejected / unknown command", result)
	}
	if entries := audit.Entries(); len(entries) != 1 || entries[0].Result != "rejected" || entries[0].Command != "not_a_command" {
		t.Errorf("unknown-command audit = %+v, want rejected command entry", entries)
	}
	if state.SyncStatus != "locked" || state.SatelliteCount != 9 || state.LastCommandSource != "" {
		t.Errorf("unknown command changed GPS state: %+v", state)
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(`{"command":`)))
	if response.Code != http.StatusBadRequest {
		t.Errorf("malformed-command status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if entries := audit.Entries(); len(entries) != 1 {
		t.Errorf("malformed command added audit record; entries = %+v", entries)
	}
	if state.SyncStatus != "locked" || state.SatelliteCount != 9 || state.LastCommandSource != "" {
		t.Errorf("malformed command changed GPS state: %+v", state)
	}
}

func TestGPSSnapshotOffsetAndTimestamps(t *testing.T) {
	resetGPSState()
	state.TimeOffsetSec = 30
	snapshot := state.snapshot()
	if snapshot["time_offset_sec"] != float64(30) {
		t.Errorf("snapshot time_offset_sec = %v, want 30", snapshot["time_offset_sec"])
	}

	reported, err := time.Parse(time.RFC3339Nano, snapshot["reported_time"].(string))
	if err != nil {
		t.Fatalf("reported_time is not RFC3339Nano: %v", err)
	}
	actual, err := time.Parse(time.RFC3339Nano, snapshot["actual_time"].(string))
	if err != nil {
		t.Fatalf("actual_time is not RFC3339Nano: %v", err)
	}
	delta := reported.Sub(actual)
	if delta < 29900*time.Millisecond || delta > 30100*time.Millisecond {
		t.Errorf("reported time offset = %v, want approximately 30s", delta)
	}
}
