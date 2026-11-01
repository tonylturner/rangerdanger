package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

const conntrackKillFixture = `{"scope":"connection","matched":1,"deleted":1,"conntrackDeleted":true,"revocationInstalled":true,"blockedUntil":"2026-10-31T12:01:00Z","closure":"TCP resets on next matching packet"}`

const conntrackFixture = `[
	{"proto":"tcp","state":"ESTABLISHED","src":"10.60.60.20","dst":"10.30.30.20","sport":"40312","dport":"2404","timeoutSecs":431999},
	{"proto":"tcp","state":"ESTABLISHED","src":"10.10.10.50","dst":"10.30.30.20","sport":"51000","dport":"2404","timeoutSecs":431990},
	{"proto":"tcp","state":"ESTABLISHED","src":"10.30.30.20","dst":"10.40.40.20","sport":"40100","dport":"502","timeoutSecs":431000},
	{"proto":"icmp","src":"10.10.10.50","dst":"10.30.30.20","timeoutSecs":29}
]`

func TestFirewallSessionsList(t *testing.T) {
	tests := []struct {
		name, query string
		code        int
		want        string
	}{
		{"all", "", http.StatusOK, `{"sessions":[` +
			`{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404,"state":"ESTABLISHED","timeout":431999},` +
			`{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404,"state":"ESTABLISHED","timeout":431990},` +
			`{"proto":"tcp","src":"10.30.30.20","dst":"10.40.40.20","sport":40100,"dport":502,"state":"ESTABLISHED","timeout":431000},` +
			`{"proto":"icmp","src":"10.10.10.50","dst":"10.30.30.20","sport":null,"dport":null,"state":null,"timeout":29}],"count":4}`},
		{"dport", "?dport=2404", http.StatusOK, `{"sessions":[` +
			`{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404,"state":"ESTABLISHED","timeout":431999},` +
			`{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404,"state":"ESTABLISHED","timeout":431990}],"count":2}`},
		{"dport and src", "?dport=2404&src=10.60.60.20", http.StatusOK, `{"sessions":[` +
			`{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404,"state":"ESTABLISHED","timeout":431999}],"count":1}`},
		{"dst only", "?dst=10.30.30.20", http.StatusOK, `{"sessions":[` +
			`{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404,"state":"ESTABLISHED","timeout":431999},` +
			`{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404,"state":"ESTABLISHED","timeout":431990},` +
			`{"proto":"icmp","src":"10.10.10.50","dst":"10.30.30.20","sport":null,"dport":null,"state":null,"timeout":29}],"count":3}`},
		{"src dst and dport", "?dport=2404&src=10.60.60.20&dst=10.30.30.20", http.StatusOK, `{"sessions":[` +
			`{"proto":"tcp","src":"10.60.60.20","dst":"10.30.30.20","sport":40312,"dport":2404,"state":"ESTABLISHED","timeout":431999}],"count":1}`},
		{"no match", "?dport=20000", http.StatusOK, `{"sessions":[],"count":0}`},
		{"bad dport", "?dport=0", http.StatusBadRequest, `{"error":"dport must be a port number 1-65535"}`},
		{"bad src", "?src=10.60.60", http.StatusBadRequest, `{"error":"src must be an IPv4 address"}`},
		{"empty dst", "?dst=", http.StatusBadRequest, `{"error":"dst must be an IPv4 address"}`},
		{"bad dst", "?dst=10.30.30.500", http.StatusBadRequest, `{"error":"dst must be an IPv4 address"}`},
		{"IPv6 dst", "?dst=2001:db8::1", http.StatusBadRequest, `{"error":"dst must be an IPv4 address"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, fw := newUpstreamStub(t, map[string]stubResponse{
				"GET /api/v1/conntrack?limit=5000": {http.StatusOK, conntrackFixture},
			})
			s := rangeServer(usGeneration(containd.NewClient(fw.URL)), "weak")
			rec := callRoute(s, s.handleFirewallSessions, http.MethodGet, "/api/firewall/sessions", "/api/firewall/sessions"+test.query, "")
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("GET sessions%s = %d %s\nwant %d %s", test.query, rec.Code, rec.Body, test.code, test.want)
			}
			if test.code == http.StatusBadRequest && len(stub.requests) != 0 {
				t.Errorf("invalid filter reached containd: %q", stub.requests)
			}
		})
	}
}

func TestFirewallSessionsListUpstreamFailures(t *testing.T) {
	_, fw := newUpstreamStub(t, map[string]stubResponse{
		"GET /api/v1/conntrack?limit=5000": {http.StatusBadGateway, `{"error":"engine conntrack status 500"}`},
	})
	for name, url := range map[string]string{"containd error": fw.URL, "containd down": closedURL(t)} {
		t.Run(name, func(t *testing.T) {
			s := rangeServer(usGeneration(containd.NewClient(url)), "weak")
			rec := callRoute(s, s.handleFirewallSessions, http.MethodGet, "/api/firewall/sessions", "/api/firewall/sessions?dport=2404", "")
			if rec.Code != http.StatusBadGateway {
				t.Errorf("GET sessions = %d %s, want 502", rec.Code, rec.Body)
			}
		})
	}
}

func TestFirewallSessionKill(t *testing.T) {
	const tuple = `{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404}`
	tests := []struct {
		name, body string
		upstream   *stubResponse
		code       int
		want       string
	}{
		{"deleted", tuple, &stubResponse{http.StatusOK, conntrackKillFixture}, http.StatusOK, strings.TrimSuffix(conntrackKillFixture, "}") + `,"status":"deleted"}`},
		{"not found", tuple, &stubResponse{http.StatusBadGateway, `{"error":"engine conntrack delete status 500: {\"error\":\"conntrack delete: not found\"}"}`},
			http.StatusNotFound, `{"detail":"conntrack entry not found: {\"error\":\"engine conntrack delete status 500: {\\\"error\\\":\\\"conntrack delete: not found\\\"}\"}","error":"no conntrack entry tcp 10.10.10.50:51000 to 10.30.30.20:2404"}`},
		{"other failure", tuple, &stubResponse{http.StatusNotImplemented, `{"error":"conntrack delete not supported"}`},
			http.StatusBadGateway, `{"error":"kill conntrack entry returned 501: {\"error\":\"conntrack delete not supported\"}"}`},
		{"unknown field", `{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404,"flush":true}`, nil,
			http.StatusBadRequest, `{"error":"invalid body: json: unknown field \"flush\""}`},
		{"zero port", `{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":0,"dport":2404}`, nil,
			http.StatusBadRequest, `{"error":"sport and dport must be port numbers 1-65535"}`},
		{"missing port", `{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","dport":2404}`, nil,
			http.StatusBadRequest, `{"error":"sport and dport must be port numbers 1-65535"}`},
		{"bad proto", `{"proto":"icmp","src":"10.10.10.50","dst":"10.30.30.20","sport":1,"dport":2404}`, nil,
			http.StatusBadRequest, `{"error":"proto must be tcp or udp"}`},
		{"bad address", `{"proto":"tcp","src":"10.10.10.500","dst":"10.30.30.20","sport":1,"dport":2404}`, nil,
			http.StatusBadRequest, `{"error":"src and dst must be IPv4 addresses"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := map[string]stubResponse{}
			if test.upstream != nil {
				routes["POST /api/v1/conntrack/kill"] = *test.upstream
			}
			stub, fw := newUpstreamStub(t, routes)
			s := rangeServer(usGeneration(containd.NewClient(fw.URL)), "weak")
			rec := callRoute(s, s.handleFirewallSessionKill, http.MethodPost, "/api/firewall/sessions/kill", "/api/firewall/sessions/kill", test.body)
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Errorf("POST kill = %d %s\nwant %d %s", rec.Code, rec.Body, test.code, test.want)
			}
			if test.upstream != nil && (len(stub.bodies) != 1 || stub.bodies[0] != tuple) {
				t.Errorf("containd kill bodies = %q, want the exact tuple", stub.bodies)
			}
		})
	}
}

func TestFirewallSessionKillPartialFailure(t *testing.T) {
	for _, test := range []struct {
		name, body, wantErr string
	}{
		{"deletion false", strings.Replace(conntrackKillFixture, `"conntrackDeleted":true`, `"conntrackDeleted":false`, 1), "conntrack deletion failed"},
		{"delete error", strings.Replace(conntrackKillFixture, `"deleted":1`, `"deleted":0,"deleteError":"permission denied"`, 1), "permission denied"},
		{"revocation false", strings.Replace(conntrackKillFixture, `"revocationInstalled":true`, `"revocationInstalled":false`, 1), "session revocation failed"},
		{"revocation error", strings.Replace(conntrackKillFixture, `"revocationInstalled":true`, `"revocationInstalled":true,"revocationError":"unavailable"`, 1), "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, fw := newUpstreamStub(t, map[string]stubResponse{
				"POST /api/v1/conntrack/kill": {http.StatusOK, test.body},
			})
			s := rangeServer(usGeneration(containd.NewClient(fw.URL)), "weak")
			rec := callRoute(s, s.handleFirewallSessionKill, http.MethodPost, "/api/firewall/sessions/kill", "/api/firewall/sessions/kill",
				`{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":51000,"dport":2404}`)
			var got sessionKillResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			var want containd.SessionKillResult
			if err := json.Unmarshal([]byte(test.body), &want); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusBadGateway || got.Status != "failed" || !strings.Contains(got.Error, test.wantErr) || !reflect.DeepEqual(got.SessionKillResult, &want) {
				t.Fatalf("kill = %d %s; want failed with all upstream result fields", rec.Code, rec.Body)
			}
		})
	}
}
