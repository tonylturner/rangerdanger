package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// deviceStateFixture is the feeder breaker's GET /api/state (§4).
const deviceStateFixture = `{"schema":2,"device_id":"feeder-breaker","kind":"breaker","generation":3735928559,"revision":4,"position":"open","remote_enabled":true,"lockout":true,"protection_tripped":true,"event_seq":4,"changed_at":"2026-10-15T12:03:00Z","measurements":{"current_a":0,"voltage_kv_ll":0,"source_kv_ll":20,"section_live":false,"source_live":true,"fault_current":false,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`

// electricalFixture is the solver's schema-2 electrical state.
const electricalFixture = `{"schema":2,"profile_id":"eu-cigre-mv-teaching","solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","truth_stale":false,"fault_section":"none","f2_tripped":false,"load_multiplier":1.0,"source_pu":1.0,"tap":0,"buses":{"bus20":{"kv":20.02,"live":true},"s1":{"kv":0,"live":false},"s2":{"kv":0,"live":false},"s3":{"kv":19.97,"live":true}},"devices":{"feeder-breaker":{"delivered":true,"solved_revision":4,"revision":4}}}`

func simServer(t *testing.T, device, solver map[string]stubResponse) (*Server, *upstreamStub) {
	routes := map[string]stubResponse{}
	deviceStub, dev := newUpstreamStub(t, routes)
	_, dss := newUpstreamStub(t, solver)
	gen := multiDeviceGeneration(t, dev.URL)
	for i := range gen.Manifest.Services {
		service := &gen.Manifest.Services[i]
		for _, role := range service.Roles {
			if role == manifest.RoleProcessSolver {
				service.Endpoints["api"] = dss.URL
			}
		}
		if _, ok := service.Endpoints[manifest.EndpointSim]; ok {
			if service.Node == "breaker-1" {
				for key, response := range device {
					routes[strings.Replace(key, " /", " /breaker-1/", 1)] = response
				}
			} else {
				routes["GET /"+service.Node+"/api/state"] = stubResponse{http.StatusOK, deviceStateFixture}
			}
		}
	}
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
		{"all devices", deviceOK, solverOK, http.StatusOK, ""},
		{"solver has no truth yet", deviceOK, map[string]stubResponse{"GET /api/electrical": {http.StatusServiceUnavailable, `{"error":"no_truth"}`}},
			http.StatusBadGateway, `{"error":"process solver returned 503: {\"error\":\"no_truth\"}"}`},
		{"device fails", map[string]stubResponse{"GET /api/state": {http.StatusInternalServerError, `boom`}}, solverOK,
			http.StatusBadGateway, `{"error":"breaker-1 returned 500: boom"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := simServer(t, test.device, test.solver)
			rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
			if test.code == http.StatusOK {
				var got fieldState
				if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
					t.Fatalf("GET field state = %d %s", rec.Code, rec.Body)
				}
				count := 0
				for _, service := range s.rng.(*fakeRange).gen.Manifest.Services {
					if _, ok := service.Endpoints[manifest.EndpointSim]; ok {
						count++
						if string(got.Devices[service.Node]) != deviceStateFixture {
							t.Errorf("missing state for %s: %s", service.Node, rec.Body)
						}
					}
				}
				if len(got.Devices) != count || string(got.Process) != electricalFixture {
					t.Errorf("unexpected field state: %s", rec.Body)
				}
				return
			}
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
