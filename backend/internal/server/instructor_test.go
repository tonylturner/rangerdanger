package server

import (
	"net/http"
	"reflect"
	"testing"
)

// deviceStateFixture is the feeder breaker's GET /api/state (§4).
const deviceStateFixture = `{"schema":1,"device_id":"feeder-breaker","generation":3735928559,"revision":4,"position":"open","remote_enabled":true,"lockout":true,"fault_active":true,"protection_tripped":true,"event_seq":4,"changed_at":"2026-10-09T12:03:00Z","measurements":{"current_a":0,"voltage_kv_ll":0,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-09T12:03:01Z","received_at":"2026-10-09T12:03:01Z"}}`

// electricalFixture is the solver's GET /api/electrical in EU mode (§5).
const electricalFixture = `{"schema":1,"profile_id":"eu-feeder-20kv","frequency_hz":50,"primary_kv":20,"breaker":"open","source_voltage_kv_ll":20,"load_voltage_kv_ll":0,"current_a":0,"load_kw":0,"energized":false,"converged":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-09T12:03:01Z","truth_revision":4,"truth_generation":3735928559,"truth_stale":false}`

func instructorServer(t *testing.T, device, solver map[string]stubResponse) (*Server, *upstreamStub) {
	deviceStub, dev := newUpstreamStub(t, device)
	_, dss := newUpstreamStub(t, solver)
	gen := euGeneration(t, nil, map[string]string{"feeder_breaker": dev.URL, "opendss_sim": dss.URL})
	return rangeServer(gen, "weak"), deviceStub
}

func TestInstructorState(t *testing.T) {
	deviceOK := map[string]stubResponse{"GET /api/state": {http.StatusOK, deviceStateFixture}}
	solverOK := map[string]stubResponse{"GET /api/electrical": {http.StatusOK, electricalFixture}}
	tests := []struct {
		name           string
		device, solver map[string]stubResponse
		code           int
		want           string
	}{
		{"both", deviceOK, solverOK, http.StatusOK, `{"device":` + deviceStateFixture + `,"process":` + electricalFixture + `}`},
		{"solver has no truth yet", deviceOK, map[string]stubResponse{"GET /api/electrical": {http.StatusServiceUnavailable, `{"error":"no_truth"}`}},
			http.StatusBadGateway, `{"error":"process solver returned 503: {\"error\":\"no_truth\"}"}`},
		{"device fails", map[string]stubResponse{"GET /api/state": {http.StatusInternalServerError, `boom`}}, solverOK,
			http.StatusBadGateway, `{"error":"instructor device returned 500: boom"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := instructorServer(t, test.device, test.solver)
			rec := callRoute(s, s.handleInstructorState, http.MethodGet, "/api/instructor/state", "/api/instructor/state", "")
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("GET instructor state = %d %s\nwant %d %s", rec.Code, rec.Body, test.code, test.want)
			}
		})
	}
}

func TestInstructorAudit(t *testing.T) {
	const audit = `{"generation":3735928559,"entries":[],"latest_seq":null,"oldest_seq":null}`
	s, _ := instructorServer(t, map[string]stubResponse{"GET /api/audit?after=12": {http.StatusOK, audit}}, nil)
	rec := callRoute(s, s.handleInstructorAudit, http.MethodGet, "/api/instructor/audit", "/api/instructor/audit?after=12", "")
	if rec.Code != http.StatusOK || rec.Body.String() != audit {
		t.Errorf("GET instructor audit = %d %s", rec.Code, rec.Body)
	}
}

func TestInstructorActions(t *testing.T) {
	tests := []struct {
		name, path, body string
		upstream         *stubResponse
		wantSent         string
		code             int
		want             string
	}{
		{"fault on", "/api/instructor/fault", `{"active":true}`, &stubResponse{http.StatusOK, deviceStateFixture}, `{"active":true}`, http.StatusOK, deviceStateFixture},
		{"fault off", "/api/instructor/fault", `{"active":false}`, &stubResponse{http.StatusOK, deviceStateFixture}, `{"active":false}`, http.StatusOK, deviceStateFixture},
		{"reset", "/api/instructor/reset", `{}`, &stubResponse{http.StatusOK, deviceStateFixture}, `{}`, http.StatusOK, deviceStateFixture},
		{"acknowledgement is not state", "/api/instructor/reset", `{}`, &stubResponse{http.StatusOK, `{"accepted":true}`}, `{}`,
			http.StatusBadGateway, `{"error":"instructor device returned an action acknowledgement instead of state"}`},
		{"non-object body", "/api/instructor/reset", `{}`, &stubResponse{http.StatusOK, `[]`}, `{}`,
			http.StatusBadGateway, `{"error":"instructor device returned a body that is not a JSON object"}`},
		{"malformed body", "/api/instructor/reset", `{}`, &stubResponse{http.StatusOK, `{"schema":`}, `{}`,
			http.StatusBadGateway, `{"error":"instructor device returned a body that is not a JSON object"}`},
		{"device refuses", "/api/instructor/reset", `{}`, &stubResponse{http.StatusInternalServerError, "reset request failed\n"}, `{}`,
			http.StatusBadGateway, `{"error":"instructor device returned 500: reset request failed"}`},
		{"fault without active", "/api/instructor/fault", `{}`, nil, "", http.StatusBadRequest, `{"error":"active is required"}`},
		{"fault unknown field", "/api/instructor/fault", `{"active":true,"x":1}`, nil, "", http.StatusBadRequest, `{"error":"invalid body: json: unknown field \"x\""}`},
		{"reset with fields", "/api/instructor/reset", `{"hard":true}`, nil, "", http.StatusBadRequest, `{"error":"invalid body: json: unknown field \"hard\""}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := map[string]stubResponse{}
			if test.upstream != nil {
				routes["POST "+test.path] = *test.upstream
			}
			s, device := instructorServer(t, routes, nil)
			handler := s.handleInstructorFault
			if test.path == "/api/instructor/reset" {
				handler = s.handleInstructorReset
			}
			rec := callRoute(s, handler, http.MethodPost, test.path, test.path, test.body)
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("POST %s = %d %s\nwant %d %s", test.path, rec.Code, rec.Body, test.code, test.want)
			}
			if test.upstream == nil {
				if len(device.requests) != 0 {
					t.Errorf("a refused request reached the device: %q", device.requests)
				}
				return
			}
			if !reflect.DeepEqual(device.requests, []string{"POST " + test.path}) {
				t.Errorf("device requests = %q, want exactly one POST", device.requests)
			}
			if len(device.bodies) != 1 || device.bodies[0] != test.wantSent {
				t.Errorf("device received %q, want %s", device.bodies, test.wantSent)
			}
		})
	}
}
