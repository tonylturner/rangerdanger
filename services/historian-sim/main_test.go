package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tturner/rangerdanger/services/shared"
)

func resetHistorianState() {
	state = &HistorianState{
		Recording:       true,
		PollIntervalSec: 5,
		RtacEndpoint:    "http://10.30.30.20:8080",
		CommsOK:         true,
	}
	audit = shared.NewAuditLog(100)
}

func newHistorianMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("GET /api/history", handleHistory)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	return mux
}

func postHistorianCommand(mux *http.ServeMux, command string, value float64) *httptest.ResponseRecorder {
	body, err := json.Marshal(shared.CommandRequest{Command: command, Value: value, Source: "test"})
	if err != nil {
		panic(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewReader(body)))
	return response
}

func TestHistorianRecordingCommands(t *testing.T) {
	tests := []struct {
		command string
		want    bool
		detail  string
	}{
		{command: "start_recording", want: true, detail: "recording started"},
		{command: "stop_recording", want: false, detail: "recording stopped"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			resetHistorianState()
			state.Recording = !tt.want
			response := postHistorianCommand(newHistorianMux(), tt.command, 0)
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode command response: %v", err)
			}
			if response.Code != http.StatusOK || result["result"] != "executed" || result["detail"] != tt.detail || state.Recording != tt.want {
				t.Errorf("command response/state = (%d, %#v, recording=%v), want executed/%q and recording=%v", response.Code, result, state.Recording, tt.detail, tt.want)
			}
		})
	}
}

func TestHistorianPollIntervalBounds(t *testing.T) {
	tests := []struct {
		value  float64
		result string
		want   int
	}{
		{value: 1, result: "executed", want: 1},
		{value: 60, result: "executed", want: 60},
		{value: 0, result: "rejected", want: 5},
		{value: 61, result: "rejected", want: 5},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%g", tt.value), func(t *testing.T) {
			resetHistorianState()
			response := postHistorianCommand(newHistorianMux(), "set_poll_interval", tt.value)
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode command response: %v", err)
			}
			if response.Code != http.StatusOK || result["result"] != tt.result {
				t.Errorf("command response = (%d, %#v), want %s", response.Code, result, tt.result)
			}
			if state.PollIntervalSec != tt.want {
				t.Errorf("PollIntervalSec = %d, want %d", state.PollIntervalSec, tt.want)
			}
		})
	}
}

func TestHistorianClearHistory(t *testing.T) {
	resetHistorianState()
	state.History = []Point{{Timestamp: "2026-01-01T00:00:00Z"}, {Timestamp: "2026-01-01T00:00:01Z"}}
	state.PointCount = len(state.History)

	response := postHistorianCommand(newHistorianMux(), "clear_history", 0)
	if response.Code != http.StatusOK {
		t.Fatalf("clear_history status = %d, want %d", response.Code, http.StatusOK)
	}
	if state.History != nil || state.PointCount != 0 {
		t.Errorf("history after clear = (%v, %d), want nil and 0", state.History, state.PointCount)
	}
	entries := audit.Entries()
	if len(entries) != 1 || entries[0].Result != "executed" || entries[0].ProcessImpact != "historical data destroyed — forensic evidence lost" {
		t.Errorf("clear-history audit = %+v, want history-destroyed impact", entries)
	}
}

func TestHistorianWriteBackCommands(t *testing.T) {
	resetHistorianState()
	mux := newHistorianMux()

	enable := postHistorianCommand(mux, "enable_write_back", 0)
	if enable.Code != http.StatusOK || !state.WriteBackEnabled {
		t.Fatalf("enable_write_back = (status %d, enabled %v), want enabled", enable.Code, state.WriteBackEnabled)
	}
	disable := postHistorianCommand(mux, "disable_write_back", 0)
	if disable.Code != http.StatusOK || state.WriteBackEnabled {
		t.Fatalf("disable_write_back = (status %d, enabled %v), want disabled", disable.Code, state.WriteBackEnabled)
	}
	entries := audit.Entries()
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(entries))
	}
	if entries[0].ProcessImpact != "historian becomes a control path — security risk" || entries[1].Result != "executed" {
		t.Errorf("write-back audit = %+v, want security impact on enable and executed disable", entries)
	}
}

func TestHistorianHistoryAndStateHandlers(t *testing.T) {
	resetHistorianState()
	want := []Point{
		{Timestamp: "2026-09-24T12:00:00Z", SubstationVoltageV: 124.7, DownstreamVoltageV: 119.2, FeederCurrentA: 83.5, GeneralLoadKw: 42, CriticalLoadKw: 18, BreakerClosed: true, RecloserClosed: false, TotalLossesKw: 1.7, PowerFactor: 0.94},
		{Timestamp: "2026-09-24T12:00:05Z", SubstationVoltageV: 124.5, DownstreamVoltageV: 119.0, FeederCurrentA: 84, GeneralLoadKw: 43, CriticalLoadKw: 18, BreakerClosed: false, RecloserClosed: false, TotalLossesKw: 1.8, PowerFactor: 0.93},
	}
	state.History = want
	state.PointCount = len(want)
	mux := newHistorianMux()

	historyResponse := httptest.NewRecorder()
	mux.ServeHTTP(historyResponse, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var historyBody struct {
		PointCount int     `json:"point_count"`
		Points     []Point `json:"points"`
	}
	if err := json.Unmarshal(historyResponse.Body.Bytes(), &historyBody); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if historyBody.PointCount != len(want) || len(historyBody.Points) != len(want) {
		t.Fatalf("history count/points = %d/%d, want %d/%d", historyBody.PointCount, len(historyBody.Points), len(want), len(want))
	}
	for i := range want {
		if historyBody.Points[i] != want[i] {
			t.Errorf("history point %d = %+v, want %+v", i, historyBody.Points[i], want[i])
		}
	}

	stateResponse := httptest.NewRecorder()
	mux.ServeHTTP(stateResponse, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	var stateBody map[string]any
	if err := json.Unmarshal(stateResponse.Body.Bytes(), &stateBody); err != nil {
		t.Fatalf("decode state response: %v", err)
	}
	if _, ok := stateBody["history"]; ok {
		t.Errorf("state snapshot unexpectedly includes history: %#v", stateBody)
	}
	if stateBody["point_count"] != float64(len(want)) {
		t.Errorf("state point_count = %v, want %d", stateBody["point_count"], len(want))
	}
}

func TestHistorianUnknownAndMalformedCommand(t *testing.T) {
	resetHistorianState()
	state.History = []Point{{Timestamp: "2026-09-24T12:00:00Z"}}
	state.PointCount = 1
	mux := newHistorianMux()
	unknown := postHistorianCommand(mux, "not_a_command", 0)
	var result map[string]any
	if err := json.Unmarshal(unknown.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode unknown-command response: %v", err)
	}
	if unknown.Code != http.StatusOK || result["result"] != "rejected" || result["detail"] != "unknown command" {
		t.Errorf("unknown-command response = (%d, %#v), want rejected / unknown command", unknown.Code, result)
	}
	if !state.Recording || state.WriteBackEnabled || state.PollIntervalSec != 5 || state.PointCount != 1 || len(state.History) != 1 {
		t.Errorf("unknown command changed historian state: %+v", state)
	}
	if entries := audit.Entries(); len(entries) != 1 || entries[0].Result != "rejected" || entries[0].Command != "not_a_command" {
		t.Errorf("unknown-command audit = %+v, want rejected command entry", entries)
	}

	malformed := httptest.NewRecorder()
	mux.ServeHTTP(malformed, httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewBufferString(`{"command":`)))
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed-command status = %d, want %d", malformed.Code, http.StatusBadRequest)
	}
	if entries := audit.Entries(); len(entries) != 1 {
		t.Errorf("malformed command added audit record; entries = %+v", entries)
	}
	if !state.Recording || state.WriteBackEnabled || state.PollIntervalSec != 5 || state.PointCount != 1 || len(state.History) != 1 {
		t.Errorf("malformed command changed historian state: %+v", state)
	}
}
