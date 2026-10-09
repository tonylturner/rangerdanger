package containd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGetEventsSuccess verifies event fetching and JSON parsing.
func TestGetEventsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify query params
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("since") != "evt-5" {
			t.Errorf("expected since=evt-5, got %s", r.URL.Query().Get("since"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"events": []Event{
				{ID: "evt-6", Kind: "request", Source: "10.20.20.10", Dest: "10.30.30.20", Protocol: "modbus", Transport: "tcp", DstPort: 502, Attributes: map[string]any{"function_code": 3}},
				{ID: "evt-7", Kind: "anomaly", Source: "10.10.10.50", Dest: "10.40.40.20", Protocol: "modbus", Transport: "tcp", DstPort: 502, Attributes: map[string]any{"severity": "critical"}},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	events, err := client.GetEvents(context.Background(), "evt-5", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "evt-6" {
		t.Errorf("expected evt-6, got %s", events[0].ID)
	}
	if events[1].Attributes["severity"] != "critical" {
		t.Errorf("expected critical severity attribute, got %v", events[1].Attributes["severity"])
	}
}

// TestGetEventsNoSince verifies events fetching without a since parameter.
func TestGetEventsNoSince(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "" {
			t.Error("expected no since parameter")
		}
		json.NewEncoder(w).Encode(map[string]any{"events": []Event{}})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	events, err := client.GetEvents(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

// TestGetFlowsSuccess feeds GetFlows containd's real wire shape: a bare
// JSON array of FlowSummary objects, with omitempty keys absent.
func TestGetFlowsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/flows" {
			t.Errorf("expected /api/v1/flows, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "50" {
			t.Errorf("expected limit=50, got %q", got)
		}
		io.WriteString(w, `[
			{"flowId":"f-1","firstSeen":"2026-10-09T10:00:00Z","lastSeen":"2026-10-09T10:00:05Z",
			 "srcIp":"10.30.30.20","dstIp":"10.40.40.10","srcPort":40312,"dstPort":2404,
			 "transport":"tcp","application":"iec104","eventCount":7},
			{"flowId":"f-2","firstSeen":"2026-10-09T10:01:00Z","lastSeen":"2026-10-09T10:01:00Z",
			 "eventCount":1,"avDetected":true,"avBlocked":true}
		]`)
	}))
	defer srv.Close()

	flows, err := newTestClient(srv.URL).GetFlows(context.Background(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flows) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(flows))
	}
	want := Flow{
		FlowID:      "f-1",
		FirstSeen:   time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC),
		LastSeen:    time.Date(2026, 10, 9, 10, 0, 5, 0, time.UTC),
		SrcIP:       "10.30.30.20",
		DstIP:       "10.40.40.10",
		SrcPort:     40312,
		DstPort:     2404,
		Transport:   "tcp",
		Application: "iec104",
		EventCount:  7,
	}
	if flows[0] != want {
		t.Errorf("flow[0]: got %+v, want %+v", flows[0], want)
	}
	if !flows[1].AvDetected || !flows[1].AvBlocked || flows[1].SrcIP != "" {
		t.Errorf("flow[1]: got %+v", flows[1])
	}
}

// TestGetFlowsError verifies a non-200 from containd surfaces its status
// and body.
func TestGetFlowsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"engine unreachable"}`, http.StatusBadGateway)
	}))
	defer srv.Close()

	flows, err := newTestClient(srv.URL).GetFlows(context.Background(), 200)
	if err == nil {
		t.Fatalf("expected error, got flows %+v", flows)
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "engine unreachable") {
		t.Errorf("error should carry status and body, got: %v", err)
	}
}
