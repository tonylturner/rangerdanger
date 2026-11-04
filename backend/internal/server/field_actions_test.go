package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestFieldActionDispatch(t *testing.T) {
	for _, tt := range []struct {
		device, action, path, state string
	}{
		{"process", "fault_s1", "/process/api/process/actions", electricalFixture},
		{"process", "fault_s2", "/process/api/process/actions", electricalFixture},
		{"process", "clear_fault", "/process/api/process/actions", electricalFixture},
		{"feeder-breaker", "reset_lockout", "/breaker-1/api/sim/actions", deviceStateFixture},
		{"f2-breaker", "reset_lockout", "/f2-breaker/api/sim/actions", fieldDeviceFixture(t, "f2-breaker")},
		{"rmu-1", "reset_fault_passage", "/rmu-1/api/sim/actions", fieldDeviceFixture(t, "rmu-1")},
		{"rmu-2", "reset_fault_passage", "/rmu-2/api/sim/actions", fieldDeviceFixture(t, "rmu-2")},
	} {
		t.Run(tt.device+"/"+tt.action, func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, map[string]stubResponse{
				"POST " + tt.path: {http.StatusOK, tt.state},
			})
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			// Exercise the actual route registration and lifecycle middleware.
			s.engine = gin.New()
			s.registerRoutes()
			rec := httptest.NewRecorder()
			body := fmt.Sprintf(`{"device":%q,"action":%q}`, tt.device, tt.action)
			s.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/field/actions", strings.NewReader(body)))
			if rec.Code != http.StatusOK || rec.Body.String() != tt.state {
				t.Fatalf("action = %d %s", rec.Code, rec.Body)
			}
			if !reflect.DeepEqual(stub.requests, []string{"POST " + tt.path}) ||
				!reflect.DeepEqual(stub.bodies, []string{fmt.Sprintf(`{"action":%q}`, tt.action)}) {
				t.Fatalf("not sent once over lab action endpoint: %v %v", stub.requests, stub.bodies)
			}
		})
	}
}

func TestFieldActionRejectsInvalidBody(t *testing.T) {
	for _, body := range []string{
		``, `null`, `[]`, `{}`, `{"device":"process"}`,
		`{"device":"process","action":"fault_s1"} {}`,
		`{"device":"process","action":"fault_s1","source":"operator"}`,
		`{"device":"process","action":"fault_s1","value":1}`,
		`{"device":"process","action":true}`,
		`{"device":"process","action":"reset"}`,
		`{"device":"process","action":"set_load"}`,
		`{"device":"feeder-breaker","action":"close"}`,
		`{"device":"feeder-breaker","action":"reset"}`,
		`{"device":"f2-breaker","action":"fault_s1"}`,
		`{"device":"rmu-1","action":"earth_close"}`,
		`{"device":"rmu-2","action":"set_local"}`,
		`{"device":"rmu-2","action":"reset_lockout"}`,
		`{"device":"oltc-1","action":"reset"}`,
		`{"device":"gps","action":"reset_offset"}`,
		`{"device":"relay","action":"inject_fault"}`,
		`{"device":"breaker","action":"reset_lockout"}`,
		`{"device":"breaker-1","action":"reset_lockout"}`,
		`{"device":"../process","action":"fault_s1"}`,
		strings.Repeat(" ", maxRouteRequestBody) + `{}`,
	} {
		t.Run(body[:min(len(body), 100)], func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, nil)
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			rec := callRoute(s, s.handleFieldAction, http.MethodPost, "/api/field/actions", "/api/field/actions", body)
			if rec.Code != http.StatusBadRequest || len(stub.requests) != 0 {
				t.Fatalf("invalid body = %d %s, upstream requests %v", rec.Code, rec.Body, stub.requests)
			}
		})
	}
}

func TestFieldActionUnsupportedPackage(t *testing.T) {
	for _, kind := range []string{"US", "no truth capability", "no recipe"} {
		t.Run(kind, func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, nil)
			gen := multiDeviceGeneration(t, upstream.URL)
			switch kind {
			case "US":
				gen = usGeneration(nil)
			case "no truth capability":
				gen.Package.Capabilities = []string{labs.CapabilityProtocolIEC104}
			case "no recipe":
				gen.Package.ID = "other"
			}
			s := rangeServer(gen, "weak")
			rec := callRoute(s, s.handleFieldAction, http.MethodPost, "/api/field/actions", "/api/field/actions", `{"device":"process","action":"fault_s1"}`)
			if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not supported by this package") || len(stub.requests) != 0 {
				t.Fatalf("unsupported package = %d %s, requests %v", rec.Code, rec.Body, stub.requests)
			}
		})
	}
}

func TestFieldActionReportsFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{"refusal", `{"error":"simulation refused"}`, http.StatusConflict},
		{"upstream failure", `{"error":"solver unavailable"}`, http.StatusServiceUnavailable},
		{"acknowledgement", `{"accepted":true}`, http.StatusOK},
		{"array", `[]`, http.StatusOK},
		{"invalid JSON", `{`, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, map[string]stubResponse{
				"POST /process/api/process/actions": {tt.status, tt.body},
			})
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			rec := callRoute(s, s.handleFieldAction, http.MethodPost, "/api/field/actions", "/api/field/actions", `{"device":"process","action":"clear_fault"}`)
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"error":`) || len(stub.requests) != 1 {
				t.Fatalf("failed action = %d %s, requests %v", rec.Code, rec.Body, stub.requests)
			}
			var result struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if tt.status != http.StatusOK && (!strings.Contains(result.Error, fmt.Sprint(tt.status)) || !strings.Contains(result.Error, tt.body)) {
				t.Fatalf("upstream refusal lost: %s", rec.Body)
			}
		})
	}
}

func TestFieldActionUnreachable(t *testing.T) {
	s := rangeServer(multiDeviceGeneration(t, closedURL(t)), "weak")
	rec := callRoute(s, s.handleFieldAction, http.MethodPost, "/api/field/actions", "/api/field/actions", `{"device":"rmu-2","action":"reset_fault_passage"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "unreachable") {
		t.Fatalf("unreachable device = %d %s", rec.Code, rec.Body)
	}
}

func TestFieldActionMissingEndpoint(t *testing.T) {
	stub, upstream := newUpstreamStub(t, nil)
	gen := multiDeviceGeneration(t, upstream.URL)
	for i := range gen.Manifest.Services {
		gen.Manifest.Services[i].Endpoints = nil
	}
	s := rangeServer(gen, "weak")
	rec := callRoute(s, s.handleFieldAction, http.MethodPost, "/api/field/actions", "/api/field/actions", `{"device":"feeder-breaker","action":"reset_lockout"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no simulation control endpoint") || len(stub.requests) != 0 {
		t.Fatalf("missing endpoint = %d %s, requests %v", rec.Code, rec.Body, stub.requests)
	}
}
