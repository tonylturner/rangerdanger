package server

import (
	"net/http"
	"testing"
)

// deviceStateFixture is the feeder breaker's GET /api/state (§4).
const deviceStateFixture = `{"schema":1,"device_id":"feeder-breaker","generation":3735928559,"revision":4,"position":"open","remote_enabled":true,"lockout":true,"fault_active":true,"protection_tripped":true,"event_seq":4,"changed_at":"2026-10-09T12:03:00Z","measurements":{"current_a":0,"voltage_kv_ll":0,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-09T12:03:01Z","received_at":"2026-10-09T12:03:01Z"}}`

// electricalFixture is the solver's GET /api/electrical in EU mode (§5).
const electricalFixture = `{"schema":1,"profile_id":"eu-feeder-20kv","frequency_hz":50,"primary_kv":20,"breaker":"open","source_voltage_kv_ll":20,"load_voltage_kv_ll":0,"current_a":0,"load_kw":0,"energized":false,"converged":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-09T12:03:01Z","truth_revision":4,"truth_generation":3735928559,"truth_stale":false}`

func simServer(t *testing.T, device, solver map[string]stubResponse) (*Server, *upstreamStub) {
	deviceStub, dev := newUpstreamStub(t, device)
	_, dss := newUpstreamStub(t, solver)
	gen := euGeneration(t, nil, map[string]string{"feeder_breaker": dev.URL, "opendss_sim": dss.URL})
	return rangeServer(gen, "weak"), deviceStub
}

func TestFieldState(t *testing.T) {
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
			http.StatusBadGateway, `{"error":"sim device returned 500: boom"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := simServer(t, test.device, test.solver)
			rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("GET field state = %d %s\nwant %d %s", rec.Code, rec.Body, test.code, test.want)
			}
		})
	}
}

func TestFieldAudit(t *testing.T) {
	const audit = `{"generation":3735928559,"entries":[],"latest_seq":null,"oldest_seq":null}`
	s, _ := simServer(t, map[string]stubResponse{"GET /api/audit?after=12": {http.StatusOK, audit}}, nil)
	rec := callRoute(s, s.handleFieldAudit, http.MethodGet, "/api/field/audit", "/api/field/audit?after=12", "")
	if rec.Code != http.StatusOK || rec.Body.String() != audit {
		t.Errorf("GET field audit = %d %s", rec.Code, rec.Body)
	}
}
