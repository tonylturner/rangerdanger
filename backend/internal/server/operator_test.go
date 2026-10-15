package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ccStateFixture is a CC GET /api/state in the contract shape (§6, §13: no
// link.local).
const ccStateFixture = `{"schema":1,"cache_epoch":"c0ffee01","link":{"state":"started","peer":"10.30.30.20:2404","since":"2026-10-09T12:00:01Z","last_rx_at":"2026-10-09T12:05:00Z","connection_seq":2,"gi":{"state":"complete","started_at":"2026-10-09T12:00:01Z","completed_at":"2026-10-09T12:00:02Z"}},"points":[{"id":"feeder-breaker-position","ioa":1001,"asdu":"M_DP_TB_1","value":"closed","quality":{"iv":false,"nt":false,"sb":false,"bl":false,"ov":null},"stale_reason":null,"source_time":"2026-10-09T12:00:01Z","source_time_invalid":false,"received_at":"2026-10-09T12:00:02Z","cot":20}]}`

const ccCommandResult = `{"id":"7","status":"completed","select":{"status":"confirmed","at":"2026-10-09T12:01:00Z"},"execute":{"status":"confirmed","at":"2026-10-09T12:01:00.2Z","terminated_at":"2026-10-09T12:01:00.6Z"},"feedback":{"status":"matched","value":"open","received_at":"2026-10-09T12:01:00.5Z"},"error":null}`

func TestOperatorState(t *testing.T) {
	_, cc := newUpstreamStub(t, map[string]stubResponse{"GET /api/state": {http.StatusOK, ccStateFixture}})
	s := rangeServer(euGeneration(t, nil, map[string]string{"control_centre": cc.URL}), "weak")
	rec := callRoute(s, s.handleOperatorState, http.MethodGet, "/api/operator/state", "/api/operator/state", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/operator/state = %d %s", rec.Code, rec.Body)
	}
	var got, want map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(ccStateFixture), &want); err != nil {
		t.Fatal(err)
	}
	want["package_id"] = euPackageID
	want["range_generation"] = float64(7)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("operator state = %s", rec.Body)
	}
}

func TestOperatorCommand(t *testing.T) {
	const command = `{"point":"feeder-breaker-command","operation":"open"}`
	tests := []struct {
		name     string
		upstream stubResponse
		code     int
		want     string
	}{
		{"completed", stubResponse{http.StatusOK, ccCommandResult}, http.StatusOK, ccCommandResult},
		{"bad input passes through", stubResponse{http.StatusBadRequest, `{"error":"unknown field \"x\""}`}, http.StatusBadRequest, `{"error":"unknown field \"x\""}`},
		{"in flight passes through", stubResponse{http.StatusConflict, `{"error":"command in flight"}`}, http.StatusConflict, `{"error":"command in flight"}`},
		{"link down passes through", stubResponse{http.StatusServiceUnavailable, `{"error":"link not started"}`}, http.StatusServiceUnavailable, `{"error":"link not started"}`},
		{"text refusal keeps its status", stubResponse{http.StatusConflict, "busy\n"}, http.StatusConflict, `{"error":"busy"}`},
		{"other status", stubResponse{http.StatusInternalServerError, `oops`}, http.StatusBadGateway, `{"error":"control centre returned 500: oops"}`},
		{"result not JSON", stubResponse{http.StatusOK, `ok`}, http.StatusBadGateway, `{"error":"control centre returned a body that is not a JSON object"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, cc := newUpstreamStub(t, map[string]stubResponse{"POST /api/commands": test.upstream})
			s := rangeServer(euGeneration(t, nil, map[string]string{"control_centre": cc.URL}), "weak")
			rec := callRoute(s, s.handleOperatorCommand, http.MethodPost, "/api/operator/commands", "/api/operator/commands", command)
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("POST commands = %d %s\nwant %d %s", rec.Code, rec.Body, test.code, test.want)
			}
			// Exactly one request reaches the CC: commands are never retried.
			if len(stub.bodies) != 1 || stub.bodies[0] != command {
				t.Errorf("CC received %q, want the command once", stub.bodies)
			}
		})
	}
}

func TestOperatorCommandCCDown(t *testing.T) {
	s := rangeServer(euGeneration(t, nil, map[string]string{"control_centre": closedURL(t)}), "weak")
	rec := callRoute(s, s.handleOperatorCommand, http.MethodPost, "/api/operator/commands", "/api/operator/commands", `{"point":"feeder-breaker-command","operation":"open"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "control centre unreachable") {
		t.Errorf("POST commands with the CC down = %d %s, want 502", rec.Code, rec.Body)
	}
}

func TestOperatorJournal(t *testing.T) {
	const journal = `{"cache_epoch":"c0ffee01","entries":[{"seq":4,"at":"2026-10-09T12:00:01Z","kind":"gi_started","detail":{"connection_seq":1}}],"latest_seq":4,"oldest_seq":1}`
	stub, cc := newUpstreamStub(t, map[string]stubResponse{
		"GET /api/journal?after=3": {http.StatusOK, journal},
		"GET /api/journal?after=0": {http.StatusInternalServerError, `down`},
	})
	s := rangeServer(euGeneration(t, nil, map[string]string{"control_centre": cc.URL}), "weak")
	tests := []struct {
		target string
		code   int
		want   string
	}{
		{"/api/operator/journal?after=3", http.StatusOK, journal},
		{"/api/operator/journal", http.StatusBadGateway, `{"error":"control centre returned 500: down"}`},
		{"/api/operator/journal?after=-1", http.StatusBadRequest, `{"error":"after must be a non-negative integer"}`},
		{"/api/operator/journal?after=x", http.StatusBadRequest, `{"error":"after must be a non-negative integer"}`},
	}
	for _, test := range tests {
		rec := callRoute(s, s.handleOperatorJournal, http.MethodGet, "/api/operator/journal", test.target, "")
		if rec.Code != test.code || rec.Body.String() != test.want {
			t.Errorf("GET %s = %d %s\nwant %d %s", test.target, rec.Code, rec.Body, test.code, test.want)
		}
	}
	if len(stub.requests) != 2 {
		t.Errorf("CC requests = %q, want the two valid cursors only", stub.requests)
	}
}

// TestOperatorAndSimRoutesNeedTheirServices answers 404 on a
// package whose manifest has no control centre, sim device or
// process solver API (the US package).
func TestOperatorAndSimRoutesNeedTheirServices(t *testing.T) {
	s := rangeServer(usGeneration(nil), "weak")
	routes := []struct {
		method, path, body string
		handler            gin.HandlerFunc
	}{
		{http.MethodGet, "/api/operator/state", "", s.handleOperatorState},
		{http.MethodPost, "/api/operator/commands", `{"point":"feeder-breaker-command","operation":"open"}`, s.handleOperatorCommand},
		{http.MethodGet, "/api/operator/journal", "", s.handleOperatorJournal},
		{http.MethodGet, "/api/field/state", "", s.handleFieldState},
		{http.MethodGet, "/api/field/audit", "", s.handleFieldAudit},
	}
	// The routes are registered on the range router as well.
	routed := routedServer(t)
	for _, route := range routes {
		rec := callRoute(s, route.handler, route.method, route.path, route.path, route.body)
		if rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not supported by this package"}` {
			t.Errorf("%s %s on US = %d %s", route.method, route.path, rec.Code, rec.Body)
		}
		if rec, body := rangeRequest(routed, route.method, route.path, route.body); rec.Code != http.StatusNotFound || body["error"] != notSupportedMessage {
			t.Errorf("routed %s %s on US = %d %s", route.method, route.path, rec.Code, rec.Body)
		}
	}
}
