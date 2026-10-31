package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// getFlows serves target (which may carry a query string; the shared
// invoke helper registers its path verbatim as the route) through the
// flows handler.
func getFlows(s *Server, target string) (*httptest.ResponseRecorder, map[string]any) {
	router := gin.New()
	router.GET("/api/firewall/flows", s.rangeBound(), s.handleGetFirewallFlows)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestHandleGetFirewallFlows(t *testing.T) {
	var gotLimit string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/flows" {
			t.Errorf("unexpected containd path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		gotLimit = r.URL.Query().Get("limit")
		io.WriteString(w, `[{"flowId":"f-1","firstSeen":"2026-10-09T10:00:00Z","lastSeen":"2026-10-09T10:00:05Z","srcIp":"10.30.30.20","dstIp":"10.40.40.10","srcPort":40312,"dstPort":2404,"transport":"tcp","application":"iec104","eventCount":7}]`)
	}))
	t.Cleanup(fake.Close)
	s := newTestServer(t, fake.URL)

	tests := []struct {
		name      string
		path      string
		wantCode  int
		wantLimit string
	}{
		{"default limit", "/api/firewall/flows", http.StatusOK, "200"},
		{"explicit limit", "/api/firewall/flows?limit=25", http.StatusOK, "25"},
		{"limit too large", "/api/firewall/flows?limit=5001", http.StatusBadRequest, ""},
		{"limit not a number", "/api/firewall/flows?limit=all", http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLimit = ""
			rec, body := getFlows(s, tt.path)
			if rec.Code != tt.wantCode {
				t.Fatalf("status: got %d, want %d (%s)", rec.Code, tt.wantCode, rec.Body.String())
			}
			if gotLimit != tt.wantLimit {
				t.Errorf("containd limit: got %q, want %q", gotLimit, tt.wantLimit)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			flows, ok := body["flows"].([]any)
			if !ok || len(flows) != 1 {
				t.Fatalf("flows: got %v", body["flows"])
			}
			flow := flows[0].(map[string]any)
			if flow["flowId"] != "f-1" || flow["dstPort"] != float64(2404) || flow["application"] != "iec104" {
				t.Errorf("flow: got %v", flow)
			}
		})
	}
}

func TestHandleGetFirewallFlows_EmptyTable(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[]`)
	}))
	t.Cleanup(fake.Close)
	s := newTestServer(t, fake.URL)

	rec, _ := getFlows(s, "/api/firewall/flows")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"flows":[]}` {
		t.Errorf("body: got %s, want {\"flows\":[]}", got)
	}
}

func TestHandleGetFirewallFlows_ContaindError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine unreachable", http.StatusBadGateway)
	}))
	t.Cleanup(fake.Close)
	s := newTestServer(t, fake.URL)

	rec, body := getFlows(s, "/api/firewall/flows")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
	if flows, ok := body["flows"].([]any); !ok || len(flows) != 0 {
		t.Errorf("flows: got %v, want []", body["flows"])
	}
	if body["error"] == nil {
		t.Error("expected error field")
	}
}

// TestHandleGetFirewallHealth pins the portal's /api/firewall/health
// body: containd's own health fields, passed through.
func TestHandleGetFirewallHealth(t *testing.T) {
	for _, forwarding := range []string{"enabled", "disabled", "unknown"} {
		for _, reservation := range []string{
			`{"active":false}`,
			`{"active":true,"bootFault":{"error":"stored conflict"},"recovery":{"state":"pending"},"listenerFaults":["https bind failed"],"guard":{"healthy":false}}`,
			`{"active":true,"recovery":{"state":"failed","error":"engine unreachable"},"faultHistory":["old fault"]}`,
			`{"active":true,"bootFault":null,"recovery":{"state":"applied"},"guard":{"healthy":true},"faultHistory":["old fault"]}`,
			"unavailable",
		} {
			t.Run(forwarding+reservation, func(t *testing.T) {
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/health":
						fmt.Fprintf(w, `{"build":"0.1.33","component":"mgmt","status":"ok","time":"2026-10-09T12:34:56.789Z","ipv4Forwarding":%q,"managementBinding":{"state":"unbound-appliance","recoveryPorts":[8080]}}`, forwarding)
					case "/api/v1/system/management-reservation":
						if r.Header.Get("Authorization") == "" {
							t.Error("unauthenticated reservation query")
						}
						if reservation == "unavailable" {
							w.WriteHeader(503)
							return
						}
						io.WriteString(w, reservation)
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
					}
				}))
				defer fake.Close()
				s := newTestServer(t, fake.URL)
				rec, body := invoke(s, s.handleGetFirewallHealth, "GET", "/api/firewall/health", nil)
				if rec.Code != 200 || body["status"] != "ok" || body["ipv4Forwarding"] != forwarding {
					t.Fatalf("lost liveness/forwarding: %s", rec.Body)
				}
				binding := body["managementBinding"].(map[string]any)
				if binding["state"] != "unbound-appliance" || len(binding["recoveryPorts"].([]any)) != 1 {
					t.Fatalf("lost binding: %s", rec.Body)
				}
				if reservation == "unavailable" {
					if body["managementReservationError"] == nil {
						t.Fatal("lost diagnostic failure")
					}
				} else {
					var want any
					json.Unmarshal([]byte(reservation), &want)
					if !reflect.DeepEqual(body["managementReservation"], want) {
						t.Fatalf("lost reservation diagnostics: %s", rec.Body)
					}
				}
			})
		}
	}
}

func TestHandleGetFirewallHealth_Unreachable(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")

	rec, body := invoke(s, s.handleGetFirewallHealth, "GET", "/api/firewall/health", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
	if body["status"] != "unavailable" || body["error"] == nil {
		t.Errorf("body: got %v", body)
	}
}
