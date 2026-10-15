package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// multiDeviceGeneration supplies increment-4 endpoints without editing the
// package manifest owned by the platform lane. Paths identify the upstream
// that received each operation, even if the manifest is reordered.
func multiDeviceGeneration(t *testing.T, base string) *lifecycle.Generation {
	t.Helper()
	gen := euGeneration(t, nil, nil)
	for i := range gen.Manifest.Services {
		service := &gen.Manifest.Services[i]
		switch service.Node {
		case "breaker-1", "gps-1":
			service.Endpoints = map[string]string{manifest.EndpointSim: base + "/" + service.Node}
		}
		for _, role := range service.Roles {
			if role == manifest.RoleProcessSolver {
				service.Endpoints = map[string]string{"api": base + "/process"}
			}
		}
	}
	for _, node := range []string{"rmu-1", "rmu-2", "oltc-1", "historian-1"} {
		role, endpoint := manifest.RoleFieldDevice, manifest.EndpointSim
		if node == "historian-1" {
			role, endpoint = manifest.RoleHistorian, "api"
		}
		gen.Manifest.Services = append(gen.Manifest.Services, manifest.Service{
			Key: node, Node: node, Roles: []manifest.Role{role},
			Endpoints: map[string]string{endpoint: base + "/" + node},
		})
	}
	return gen
}

func TestEUFullActionWire(t *testing.T) {
	rmu := []string{"set_local", "set_remote", "earth_close", "earth_open", "reset_fault_passage", "reset"}
	commands := map[string][]string{
		"breaker": {"reset_lockout", "reset"},
		"rmu-1":   rmu,
		"rmu-2":   rmu,
		"oltc-1":  {"set_local", "set_remote", "block", "unblock", "reset"},
		"process": {"fault_s1", "fault_s2", "clear_fault", "reset_f2", "set_load", "set_source", "reset"},
		"gps":     {"set_offset", "reset_offset"},
	}
	for device, names := range commands {
		for _, command := range names {
			t.Run(device+"/"+command, func(t *testing.T) {
				node, path := euRecipe.simDevices[device], "/api/sim/actions"
				if device == "process" {
					node, path = "process", "/api/process/actions"
				} else if device == "gps" {
					path = "/api/command"
				}
				var value *float64
				if command == "set_load" || command == "set_source" || command == "set_offset" {
					v := 1.0
					value = &v
				}
				expectedBody := map[string]any{"action": command}
				if device == "gps" {
					expectedBody = map[string]any{"command": command, "source": "lab"}
				}
				if value != nil {
					expectedBody["value"] = *value
				}
				want, err := json.Marshal(expectedBody)
				if err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf(`{"device_id":%q,"revision":8}`, node)
				if device == "gps" {
					body = `{"result":"executed","detail":"offset changed"}`
				}
				stub, upstream := newUpstreamStub(t, map[string]stubResponse{
					"POST /" + node + path: {http.StatusOK, body},
				})
				gen := multiDeviceGeneration(t, upstream.URL)
				// Reverse ordering: resolution must use nodes, not the first sim.
				for i, j := 0, len(gen.Manifest.Services)-1; i < j; i, j = i+1, j-1 {
					gen.Manifest.Services[i], gen.Manifest.Services[j] = gen.Manifest.Services[j], gen.Manifest.Services[i]
				}
				s := rangeServer(gen, "weak")
				result := s.executeCommand(t.Context(), gen, device, command, "", value)
				if !result.Success || len(stub.bodies) != 1 {
					t.Fatalf("action = %+v, requests = %v", result, stub.requests)
				}
				var got map[string]any
				if err := json.Unmarshal([]byte(stub.bodies[0]), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, expectedBody) {
					t.Fatalf("wire = %s, want %s", stub.bodies[0], want)
				}
			})
		}
	}
}

func TestEUGPSRejectedIsNotSuccess(t *testing.T) {
	_, upstream := newUpstreamStub(t, map[string]stubResponse{
		"POST /gps-1/api/command": {http.StatusOK, `{"result":"rejected","detail":"unknown command"}`},
	})
	gen := multiDeviceGeneration(t, upstream.URL)
	s := rangeServer(gen, "weak")
	result := s.executeCommand(t.Context(), gen, "gps", "reset_offset", "", nil)
	if result.Success || !strings.Contains(result.Detail, "did not execute") {
		t.Fatalf("rejected GPS = %+v", result)
	}
}

func TestEUGPSUsesTimeSourceSimEndpoint(t *testing.T) {
	for _, command := range []string{"set_offset", "reset_offset"} {
		t.Run(command, func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, map[string]stubResponse{
				"POST /gps-1/api/command": {http.StatusOK, `{"result":"executed","detail":"offset changed"}`},
			})
			gen := multiDeviceGeneration(t, upstream.URL)
			for i := range gen.Manifest.Services {
				service := &gen.Manifest.Services[i]
				if service.Node == "gps-1" {
					service.Roles = []manifest.Role{manifest.RoleTimeSource}
					// A GPS api endpoint must never substitute for sim.
					service.Endpoints["api"] = upstream.URL + "/wrong-api"
				}
			}
			// Nor may a different time-source service substitute for node gps-1.
			gen.Manifest.Services = append([]manifest.Service{{
				Key: "other-clock", Node: "other-clock", Roles: []manifest.Role{manifest.RoleTimeSource},
				Endpoints: map[string]string{manifest.EndpointSim: upstream.URL + "/wrong-clock"},
			}}, gen.Manifest.Services...)
			var value *float64
			if command == "set_offset" {
				offset := 2.5
				value = &offset
			}
			s := rangeServer(gen, "weak")
			result := s.executeCommand(t.Context(), gen, "gps", command, "", value)
			if !result.Success || !reflect.DeepEqual(stub.requests, []string{"POST /gps-1/api/command"}) {
				t.Fatalf("GPS dispatch = %+v, requests=%v", result, stub.requests)
			}
			want := `{"command":"reset_offset","source":"lab"}`
			if command == "set_offset" {
				want = `{"command":"set_offset","value":2.5,"source":"lab"}`
			}
			if len(stub.bodies) != 1 || stub.bodies[0] != want {
				t.Fatalf("GPS wire = %v, want %s", stub.bodies, want)
			}
		})
	}
}

func TestFieldStateEverySimNode(t *testing.T) {
	routes := map[string]stubResponse{
		"GET /process/api/electrical": {http.StatusOK, electricalFixture},
	}
	for _, node := range euRecipe.simDevices {
		routes["GET /"+node+"/api/state"] = stubResponse{http.StatusOK, fmt.Sprintf(`{"node":%q}`, node)}
	}
	stub, upstream := newUpstreamStub(t, routes)
	s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
	rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
	var got fieldState
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("state = %d %s", rec.Code, rec.Body)
	}
	if len(got.Devices) != len(euRecipe.simDevices) || string(got.Process) != electricalFixture || len(stub.requests) != 6 {
		t.Fatalf("not every upstream returned: %s, requests=%v", rec.Body, stub.requests)
	}
	for _, node := range euRecipe.simDevices {
		if string(got.Devices[node]) != fmt.Sprintf(`{"node":%q}`, node) {
			t.Errorf("missing or miskeyed %s: %s", node, rec.Body)
		}
	}
}

func TestFieldStateFailureNamesUpstream(t *testing.T) {
	for _, failed := range []string{"breaker-1", "rmu-1", "rmu-2", "oltc-1", "gps-1", "process"} {
		t.Run(failed, func(t *testing.T) {
			routes := map[string]stubResponse{
				"GET /process/api/electrical": {http.StatusOK, electricalFixture},
			}
			for _, node := range euRecipe.simDevices {
				routes["GET /"+node+"/api/state"] = stubResponse{http.StatusOK, `{}`}
			}
			path, name := "/"+failed+"/api/state", failed
			if failed == "process" {
				path, name = "/process/api/electrical", solverService
			}
			routes["GET "+path] = stubResponse{http.StatusServiceUnavailable, `{"error":"unavailable"}`}
			_, upstream := newUpstreamStub(t, routes)
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			rec := callRoute(s, s.handleFieldState, http.MethodGet, "/api/field/state", "/api/field/state", "")
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), name) || strings.Contains(rec.Body.String(), `"devices":`) {
				t.Fatalf("failure = %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestFieldStateTimeoutIs502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
	// An expired deadline exercises timeout mapping without a 5 s sleep.
	rec := callRoute(s, func(c *gin.Context) {
		ctx, cancel := context.WithDeadline(c.Request.Context(), time.Now().Add(-time.Second))
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		s.handleFieldState(c)
	}, http.MethodGet, "/api/field/state", "/api/field/state", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "gps-1") ||
		!strings.Contains(rec.Body.String(), "context deadline exceeded") {
		t.Fatalf("timeout = %d %s", rec.Code, rec.Body)
	}
}
