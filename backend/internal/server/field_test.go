package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// deviceStateFixture is the feeder breaker's GET /api/state (§4).
const deviceStateFixture = `{"schema":3,"device_id":"feeder-breaker","kind":"breaker","generation":3735928559,"revision":4,"position":"open","remote_enabled":true,"lockout":true,"protection_tripped":true,"event_seq":4,"changed_at":"2026-10-15T12:03:00Z","protection":{"function":"overcurrent","pickup_a":400,"delay_ms":1000,"instantaneous_a":3000,"picked_up":false,"picked_up_since":null,"last_trip_current_a":null},"measurements":{"current_a":0,"voltage_kv_ll":0,"source_kv_ll":20,"section_live":false,"source_live":true,"p_mw":0,"q_mvar":0,"pf":null,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`

// electricalFixture is the solver's schema-3 electrical state.
const electricalFixture = `{"schema":3,"profile_id":"eu-cigre-mv-teaching","solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","truth_stale":false,"fault_section":"none","f2_tripped":false,"load_multiplier":1.0,"source_pu":1.0,"tap":0,"profile":{"enabled":true,"load_factors":{"residential":0.8,"commercial":0.9,"industrial":1},"source_pu":1},"buses":{"bus20":{"kv":20.02,"live":true},"s1":{"kv":0,"live":false},"s2":{"kv":0,"live":false},"s3":{"kv":19.97,"live":true}},"devices":{"feeder-breaker":{"delivered":true,"solved_revision":4,"revision":4,"current_a":0,"p_mw":0,"q_mvar":0}}}`

// GPS has its own unversioned contract, not the feeder device schema.
const clockStateFixture = `{"actual_time":"2026-10-15T12:03:01Z","reported_time":"2026-10-15T12:03:03.5Z","sync_status":"locked","time_offset_sec":2.5,"satellite_count":9,"holdover_started":"","holdover_drift_ppm":0,"ntp_enabled":true,"ptp_enabled":false,"irig_b_output":true,"alarm":false,"comms_ok":true,"last_command_source":"lab"}`

// Snapshots match the schema-3 kind unions and the electricalFixture's CB1
// trip: S1/S2 are dead, while the separate F2 feeder keeps S3 energised.
func fieldDeviceFixture(t *testing.T, node string) string {
	t.Helper()
	switch node {
	case "breaker-1":
		return deviceStateFixture
	case "f2-breaker":
		return `{"schema":3,"device_id":"f2-breaker","kind":"breaker","generation":3735928559,"revision":1,"position":"closed","remote_enabled":true,"lockout":false,"protection_tripped":false,"event_seq":0,"changed_at":"2026-10-15T12:00:00Z","protection":{"function":"overcurrent","pickup_a":400,"delay_ms":1000,"instantaneous_a":2000,"picked_up":false,"picked_up_since":null,"last_trip_current_a":null},"measurements":{"current_a":11.27,"voltage_kv_ll":19.97,"source_kv_ll":20.02,"section_live":true,"source_live":true,"p_mw":0.3315,"q_mvar":0.2054,"pf":0.85,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`
	case "rmu-1":
		return `{"schema":3,"device_id":"rmu-1","kind":"rmu","generation":3735928559,"revision":1,"position":"closed","remote_enabled":true,"earth_closed":false,"fault_passage":false,"event_seq":0,"changed_at":"2026-10-15T12:00:00Z","protection":{"function":"fpi","pickup_a":300,"delay_ms":0,"instantaneous_a":null,"picked_up":false,"picked_up_since":null,"last_trip_current_a":null},"measurements":{"current_a":0,"voltage_kv_ll":0,"source_kv_ll":0,"section_live":false,"source_live":false,"p_mw":0,"q_mvar":0,"pf":null,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`
	case "rmu-2":
		return `{"schema":3,"device_id":"rmu-2","kind":"rmu","generation":3735928559,"revision":1,"position":"open","remote_enabled":true,"earth_closed":false,"fault_passage":false,"event_seq":0,"changed_at":"2026-10-15T12:00:00Z","protection":{"function":"fpi","pickup_a":300,"delay_ms":0,"instantaneous_a":null,"picked_up":false,"picked_up_since":null,"last_trip_current_a":null},"measurements":{"current_a":0,"voltage_kv_ll":0,"source_kv_ll":19.97,"section_live":false,"source_live":true,"p_mw":0,"q_mvar":0,"pf":null,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`
	case "oltc-1":
		return `{"schema":3,"device_id":"oltc-1","kind":"oltc","generation":3735928559,"revision":1,"tap":0,"target_tap":0,"moving":false,"auto":true,"blocked":false,"armed":false,"remote_enabled":true,"event_seq":0,"changed_at":"2026-10-15T12:00:00Z","plc_request_result":"none","heartbeat":120,"protection":{"function":"oltc_block","pickup_a":866,"delay_ms":0,"instantaneous_a":null,"picked_up":false,"picked_up_since":null,"last_trip_current_a":null},"measurements":{"current_a":11.27,"voltage_kv_ll":20.02,"source_kv_ll":110,"section_live":true,"source_live":true,"p_mw":0.3315,"q_mvar":0.2054,"pf":0.85,"valid":true,"solver_epoch":"e1","solve_seq":200,"solved_at":"2026-10-15T12:03:01Z","received_at":"2026-10-15T12:03:01Z"}}`
	case "gps-1":
		return clockStateFixture
	default:
		t.Fatalf("no field snapshot fixture for %q", node)
		return ""
	}
}

func TestFieldDeviceFixtureContract(t *testing.T) {
	common := "schema device_id kind generation revision event_seq changed_at remote_enabled measurements protection "
	for _, tt := range []struct {
		node, id, kind, function, fields string
	}{
		{"breaker-1", "feeder-breaker", "breaker", "overcurrent", "position lockout protection_tripped"},
		{"f2-breaker", "f2-breaker", "breaker", "overcurrent", "position lockout protection_tripped"},
		{"rmu-1", "rmu-1", "rmu", "fpi", "position earth_closed fault_passage"},
		{"rmu-2", "rmu-2", "rmu", "fpi", "position earth_closed fault_passage"},
		{"oltc-1", "oltc-1", "oltc", "oltc_block", "tap target_tap moving auto blocked armed plc_request_result heartbeat"},
	} {
		t.Run(tt.node, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(fieldDeviceFixture(t, tt.node)), &fields); err != nil {
				t.Fatal(err)
			}
			var got []string
			for key := range fields {
				got = append(got, key)
			}
			want := strings.Fields(common + tt.fields)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) || string(fields["device_id"]) != `"`+tt.id+`"` ||
				string(fields["kind"]) != `"`+tt.kind+`"` || string(fields["schema"]) != "3" {
				t.Fatalf("wrong identity or kind shape: %s", fieldDeviceFixture(t, tt.node))
			}
			var protection struct {
				Function string `json:"function"`
			}
			if err := json.Unmarshal(fields["protection"], &protection); err != nil || protection.Function != tt.function {
				t.Fatalf("wrong protection shape: %s, %v", fields["protection"], err)
			}
		})
	}
}

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
				routes["GET /"+service.Node+"/api/state"] = stubResponse{http.StatusOK, fieldDeviceFixture(t, service.Node)}
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
						if service.Node == "gps-1" {
							continue
						}
						count++
						if string(got.Devices[service.Node]) != fieldDeviceFixture(t, service.Node) {
							t.Errorf("missing state for %s: %s", service.Node, rec.Body)
						}
					}
				}
				if len(got.Devices) != count || string(got.Clock) != clockStateFixture || string(got.Process) != electricalFixture {
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

func TestFieldStateWithoutClock(t *testing.T) {
	s, _ := simServer(t,
		map[string]stubResponse{"GET /api/state": {http.StatusOK, deviceStateFixture}},
		map[string]stubResponse{"GET /api/electrical": {http.StatusOK, electricalFixture}})
	for i := range s.rng.(*fakeRange).gen.Manifest.Services {
		service := &s.rng.(*fakeRange).gen.Manifest.Services[i]
		if service.Node == "gps-1" {
			delete(service.Endpoints, manifest.EndpointSim)
		}
	}
	rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
	var got fieldState
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil ||
		string(got.Clock) != "null" || len(got.Devices) != 5 {
		t.Fatalf("state without clock = %d %s", rec.Code, rec.Body)
	}
}

func TestFieldStateUSNotSupported(t *testing.T) {
	s := rangeServer(usGeneration(nil), "weak")
	rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
	if rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not supported by this package"}` {
		t.Fatalf("US field state = %d %s", rec.Code, rec.Body)
	}
}

func TestFieldStateRejectsAmbiguousClock(t *testing.T) {
	s, _ := simServer(t,
		map[string]stubResponse{"GET /api/state": {http.StatusOK, deviceStateFixture}},
		map[string]stubResponse{"GET /api/electrical": {http.StatusOK, electricalFixture}})
	gen := s.rng.(*fakeRange).gen
	gen.Manifest.Services = append(gen.Manifest.Services, manifest.Service{
		Key: "other-clock", Node: "other-clock", Roles: []manifest.Role{manifest.RoleTimeSource},
		Endpoints: map[string]string{manifest.EndpointSim: "http://unused.invalid"},
	})
	rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
	if rec.Code != http.StatusBadGateway || rec.Body.String() != `{"error":"multiple time-source sims declared"}` {
		t.Fatalf("ambiguous clock = %d %s", rec.Code, rec.Body)
	}
}

func TestFieldStateRejectsMalformedClockObject(t *testing.T) {
	for _, body := range []string{"null", "[]", `"clock"`, "invalid json"} {
		t.Run(body, func(t *testing.T) {
			_, upstream := newUpstreamStub(t, map[string]stubResponse{
				"GET /gps-1/api/state": {http.StatusOK, body},
			})
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "gps-1") {
				t.Fatalf("malformed clock = %d %s", rec.Code, rec.Body)
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

func TestFieldAuditPerDevice(t *testing.T) {
	for _, key := range []string{"breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1", "bad", "", "feeder-breaker", "../rmu-1"} {
		t.Run(key, func(t *testing.T) {
			const audit = `{"generation":1,"entries":[],"latest_seq":null,"oldest_seq":null}`
			_, upstream := newUpstreamStub(t, map[string]stubResponse{"GET /" + key + "/api/audit?after=7": {http.StatusOK, audit}})
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			rec := callRoute(s, s.handleFieldAudit, http.MethodGet, "/api/field/audit", "/api/field/audit?device="+key+"&after=7", "")
			valid := key == "breaker-1" || key == "f2-breaker" || key == "rmu-1" || key == "rmu-2" || key == "oltc-1"
			if valid && (rec.Code != http.StatusOK || rec.Body.String() != audit) || !valid && rec.Code != http.StatusBadRequest {
				t.Fatalf("audit %q = %d %s", key, rec.Code, rec.Body)
			}
		})
	}
}
