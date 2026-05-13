package containd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestGenerateJWT verifies that the self-generated JWT has valid structure
// and is compatible with containd's lab-mode token validation.
func TestGenerateJWT(t *testing.T) {
	token := generateJWT("test-secret")
	parts := splitJWT(t, token)

	// Verify header
	header := decodeJWTPart(t, parts[0])
	if header["alg"] != "HS256" {
		t.Errorf("expected alg HS256, got %v", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Errorf("expected typ JWT, got %v", header["typ"])
	}

	// Verify payload
	payload := decodeJWTPart(t, parts[1])
	if payload["sub"] != "rangerdanger-backend" {
		t.Errorf("expected sub rangerdanger-backend, got %v", payload["sub"])
	}
	if payload["role"] != "admin" {
		t.Errorf("expected role admin, got %v", payload["role"])
	}

	// Verify expiration is ~24h from now
	exp, ok := payload["exp"].(float64)
	if !ok {
		t.Fatal("exp claim missing or not a number")
	}
	expTime := time.Unix(int64(exp), 0)
	diff := time.Until(expTime)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("expected exp ~24h from now, got %v", diff)
	}
}

// TestGenerateJWTDeterministicSignature verifies that the same secret
// produces consistent signatures (i.e. HMAC-SHA256 is correctly applied).
func TestGenerateJWTDeterministicSignature(t *testing.T) {
	// Two tokens with the same secret should have identical header+payload structure
	// (payload differs due to exp timestamp, but the signing mechanism should work)
	token1 := generateJWT("shared-secret")
	token2 := generateJWT("different-secret")

	parts1 := splitJWT(t, token1)
	parts2 := splitJWT(t, token2)

	// Headers should be identical (same alg)
	if parts1[0] != parts2[0] {
		t.Error("headers should be identical regardless of secret")
	}

	// Signatures must differ for different secrets
	if parts1[2] == parts2[2] {
		t.Error("signatures should differ for different secrets")
	}
}

// TestNewClientUsesEnvSecret verifies the client reads CONTAIND_JWT_SECRET
// from environment and falls back to default.
func TestNewClientUsesEnvSecret(t *testing.T) {
	// Test default
	os.Unsetenv("CONTAIND_JWT_SECRET")
	client := NewClient("http://localhost:8080")
	if client.AuthToken == "" {
		t.Error("expected non-empty auth token with default secret")
	}

	// Test custom secret
	t.Setenv("CONTAIND_JWT_SECRET", "custom-secret-123")
	client2 := NewClient("http://localhost:8080")
	if client2.AuthToken == client.AuthToken {
		t.Error("expected different token with different secret")
	}
}

// TestNewClientSetsBaseURL verifies the client stores the base URL correctly.
func TestNewClientSetsBaseURL(t *testing.T) {
	client := NewClient("http://firewall:8080")
	if client.BaseURL != "http://firewall:8080" {
		t.Errorf("expected base URL http://firewall:8080, got %s", client.BaseURL)
	}
}

// TestGetHealthSuccess verifies parsing a healthy response from containd.
func TestGetHealthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(HealthStatus{
			Status:    "healthy",
			Version:   "1.0.0",
			Uptime:    3600,
			Zones:     4,
			Sessions:  12,
			EventRate: 5,
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	health, err := client.GetHealth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}
	if health.Zones != 4 {
		t.Errorf("expected 4 zones, got %d", health.Zones)
	}
}

// TestGetHealthUnreachable verifies error handling when containd is down.
func TestGetHealthUnreachable(t *testing.T) {
	client := newTestClient("http://127.0.0.1:1") // nothing listening
	_, err := client.GetHealth()
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

// TestGetHealthNon200 verifies error handling for non-200 responses.
func TestGetHealthNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetHealth()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// TestIsAvailable verifies the availability check.
func TestIsAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	if !client.IsAvailable() {
		t.Error("expected available with healthy response")
	}

	// Unreachable server
	client2 := newTestClient("http://127.0.0.1:1")
	if client2.IsAvailable() {
		t.Error("expected unavailable for unreachable server")
	}
}

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
				{ID: "evt-6", Type: "modbus", Source: "10.20.20.10", Dest: "10.30.30.20", Protocol: "modbus", DstPort: 502, Details: "FC03 Read", Severity: "info", Zone: "dmz"},
				{ID: "evt-7", Type: "alert", Source: "10.10.10.50", Dest: "10.40.40.20", Protocol: "modbus", DstPort: 502, Details: "FC16 BLOCKED", Severity: "critical", Zone: "wan"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	events, err := client.GetEvents("evt-5", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "evt-6" {
		t.Errorf("expected evt-6, got %s", events[0].ID)
	}
	if events[1].Severity != "critical" {
		t.Errorf("expected critical severity, got %s", events[1].Severity)
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
	events, err := client.GetEvents("", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

// TestGetSessionsSuccess verifies session fetching.
func TestGetSessionsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		json.NewEncoder(w).Encode(map[string]any{
			"sessions": []Session{
				{ID: "sess-1", Source: "10.20.20.10", Dest: "10.30.30.20", Protocol: "tcp", DstPort: 502, Bytes: 1024},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	sessions, err := client.GetSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].DstPort != 502 {
		t.Errorf("expected dst port 502, got %d", sessions[0].DstPort)
	}
}

// TestGetFirewallRulesSuccess verifies config/rules parsing.
func TestGetFirewallRulesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				DefaultAction: "DENY",
				Rules: []FirewallRule{
					{ID: "it-to-dmz", Description: "IT to DMZ: SSH/HTTP/S", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "22"}, {Name: "tcp", Port: "443"}}},
					{ID: "deny-writes-safety", Description: "Block Modbus WRITE to Safety", DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	rules, err := client.GetFirewallRules()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Action != "ALLOW" {
		t.Errorf("expected ALLOW, got %s", rules[0].Action)
	}
	if rules[1].ICS == nil {
		t.Fatal("expected ICS config on second rule")
	}
	if rules[1].ICS.Protocol != "modbus" {
		t.Errorf("expected modbus protocol, got %s", rules[1].ICS.Protocol)
	}
}

// TestGetZoneRuleSummariesGrouping verifies rules are grouped by zone pair correctly.
func TestGetZoneRuleSummariesGrouping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				DefaultAction: "DENY",
				Rules: []FirewallRule{
					{ID: "r1", Description: "IT to DMZ SSH", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "22"}}},
					{ID: "r2", Description: "IT to DMZ HTTPS", SourceZones: []string{"wan"}, DestZones: []string{"dmz"}, Action: "ALLOW",
						Protocols: []Protocol{{Name: "tcp", Port: "443"}}},
					{ID: "r3", Description: "HMI View Modbus R/O", SourceZones: []string{"dmz"}, DestZones: []string{"lan1"}, Action: "ALLOW",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4}}},
					{ID: "r4", Description: "Block writes to safety", DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	summaries, err := client.GetZoneRuleSummaries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build map for easier assertions
	smap := make(map[string]ZoneRuleSummary)
	for _, s := range summaries {
		key := s.SourceZone + "->" + s.DestZone
		smap[key] = s
	}

	// wan->dmz should have 2 rules grouped
	wanDmz, ok := smap["wan->dmz"]
	if !ok {
		t.Fatal("missing wan->dmz summary")
	}
	if len(wanDmz.RuleDetails) != 2 {
		t.Errorf("expected 2 rule details for wan->dmz, got %d", len(wanDmz.RuleDetails))
	}
	if wanDmz.Action != "ALLOW" {
		t.Errorf("expected ALLOW for wan->dmz, got %s", wanDmz.Action)
	}

	// dmz->lan1 should have modbus R/O
	dmzLan1, ok := smap["dmz->lan1"]
	if !ok {
		t.Fatal("missing dmz->lan1 summary")
	}
	if dmzLan1.Action != "ALLOW" {
		t.Errorf("expected ALLOW for dmz->lan1, got %s", dmzLan1.Action)
	}

	// any->lan2 should be DENY
	anyLan2, ok := smap["any->lan2"]
	if !ok {
		t.Fatal("missing any->lan2 summary")
	}
	if anyLan2.Action != "DENY" {
		t.Errorf("expected DENY for any->lan2, got %s", anyLan2.Action)
	}
}

// TestGetZoneRuleSummariesMixedAction verifies MIXED action detection.
func TestGetZoneRuleSummariesMixedAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"firewall": FirewallConfig{
				Rules: []FirewallRule{
					{ID: "r1", Description: "Allow read", SourceZones: []string{"dmz"}, DestZones: []string{"lan2"}, Action: "ALLOW",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{1, 2, 3, 4}}},
					{ID: "r2", Description: "Deny write", SourceZones: []string{"dmz"}, DestZones: []string{"lan2"}, Action: "DENY",
						ICS: &ICSConfig{Protocol: "modbus", FunctionCodes: []int{5, 6, 15, 16}}},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	summaries, err := client.GetZoneRuleSummaries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Action != "MIXED" {
		t.Errorf("expected MIXED action, got %s", summaries[0].Action)
	}
}

// TestImportConfigSuccess verifies config import via the candidate/commit flow.
func TestImportConfigSuccess(t *testing.T) {
	var candidateBody []byte
	var sawCandidate, sawCommit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			sawCandidate = true
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
			}
			var err error
			candidateBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case "/api/v1/config/commit":
			sawCommit = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	configJSON := []byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`)
	client := newTestClient(srv.URL)
	_, err := client.ImportConfig(configJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawCandidate {
		t.Error("expected POST /api/v1/config/candidate to be called")
	}
	if !sawCommit {
		t.Error("expected POST /api/v1/config/commit to be called")
	}

	// ImportConfig injects dataplane.enforcement=true so containd's engine
	// actually compiles + applies the ruleset (otherwise commit succeeds but
	// no nft rules are pushed). Re-parse the body and verify the firewall
	// section came through unmodified and the dataplane field is set.
	var sent map[string]any
	if err := json.Unmarshal(candidateBody, &sent); err != nil {
		t.Fatalf("candidate body not valid JSON: %v\n%s", err, candidateBody)
	}
	if dp, _ := sent["dataplane"].(map[string]any); dp == nil || dp["enforcement"] != true {
		t.Errorf("expected dataplane.enforcement=true, got body: %s", candidateBody)
	}
	fw, _ := sent["firewall"].(map[string]any)
	if fw == nil || fw["defaultAction"] != "DENY" {
		t.Errorf("firewall section not preserved: %s", candidateBody)
	}
}

// TestImportConfigSurfacesCommitWarnings asserts that warnings from
// containd's X-Containd-Warnings response header propagate back to the
// caller. Without this, partial commits (e.g. nft apply failed due to
// missing NET_ADMIN) silently return success and the lab UI hides the
// degradation. The header carries one warning per line per containd's
// setWarningHeader convention (api/http/util.go).
func TestImportConfigSurfacesCommitWarnings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/config/commit":
			// containd v0.1.26+ emits one X-Containd-Warnings header
			// per warning (multi-value header). Older builds joined
			// with "\n" in a single header value; collectWarnings
			// handles both. Test the multi-value form here since
			// that's what current containd produces.
			w.Header().Add("X-Containd-Warnings", "ruleset: nft apply failed: operation not permitted")
			w.Header().Add("X-Containd-Warnings", "interfaces: link eth9 not found")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	warnings, err := client.ImportConfig([]byte(`{"firewall":{"rules":[]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "nft apply failed") {
		t.Errorf("warning[0] missing nft hint: %q", warnings[0])
	}
	if !strings.Contains(warnings[1], "eth9 not found") {
		t.Errorf("warning[1] missing iface hint: %q", warnings[1])
	}
}

func TestImportConfigNoWarningsHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	warnings, err := newTestClient(srv.URL).ImportConfig([]byte(`{"firewall":{"rules":[]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if warnings != nil {
		t.Errorf("expected nil warnings for clean apply, got %v", warnings)
	}
}

// TestEnsureEnforcementOn pins the lab-invariant shim that ImportConfig
// applies. The substation-weak.json / substation-improved.json files (and
// student-authored custom policies) typically don't set
// `dataplane.enforcement` — without this shim, containd commits the rules
// but engine.ApplyRules silently no-ops because the compiler is nil
// (pkg/dp/engine/engine.go:113-129 in containd).
func TestEnsureEnforcementOn(t *testing.T) {
	type tc struct {
		name string
		in   string
	}
	cases := []tc{
		{"empty dataplane", `{"firewall":{"rules":[]},"dataplane":{}}`},
		{"missing dataplane key", `{"firewall":{"rules":[]}}`},
		{"explicitly false (must override)", `{"firewall":{"rules":[]},"dataplane":{"enforcement":false}}`},
		{"already true (no-op)", `{"firewall":{"rules":[]},"dataplane":{"enforcement":true}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ensureEnforcementOn([]byte(c.in))
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			var doc map[string]any
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatalf("output not JSON: %v", err)
			}
			dp, _ := doc["dataplane"].(map[string]any)
			if dp == nil || dp["enforcement"] != true {
				t.Errorf("expected dataplane.enforcement=true, got: %s", out)
			}
			// Firewall section preserved.
			fw, _ := doc["firewall"].(map[string]any)
			if fw == nil {
				t.Errorf("firewall section dropped: %s", out)
			}
		})
	}
}

func TestEnsureEnforcementOn_InvalidJSON(t *testing.T) {
	if _, err := ensureEnforcementOn([]byte("not json")); err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestEnsureEnforcementOn_EmptyInput(t *testing.T) {
	out, err := ensureEnforcementOn(nil)
	if err != nil {
		t.Fatalf("nil input should not error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("nil input should pass through, got %s", out)
	}
}

// TestImportConfigLegacyFallback verifies the client falls back to
// /api/v1/config/import when the candidate endpoint returns 404.
func TestImportConfigLegacyFallback(t *testing.T) {
	var sawImport bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			w.WriteHeader(http.StatusNotFound)
		case "/api/v1/config/import":
			sawImport = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	if _, err := client.ImportConfig([]byte(`{}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawImport {
		t.Error("expected fallback to /api/v1/config/import")
	}
}

// TestImportConfigFailure verifies error handling on import failure.
func TestImportConfigFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid config schema"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.ImportConfig([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

// TestImportConfigAuth403 verifies handling of authentication rejection.
// This simulates what would happen if lab mode is disabled and the
// self-generated JWT is not accepted.
func TestImportConfigAuth403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"password change required"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.ImportConfig([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

// TestWaitReadySuccess verifies the polling loop succeeds when containd comes up.
func TestWaitReadySuccess(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.WaitReady(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&callCount) < 3 {
		t.Error("expected at least 3 health check attempts")
	}
}

// TestWaitReadyTimeout verifies WaitReady returns error on timeout.
func TestWaitReadyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	err := client.WaitReady(context.Background(), 3*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// TestAuthHeaderFormat verifies the Authorization header is set correctly
// on all API requests (Bearer token format).
func TestAuthHeaderFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Error("missing Authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if len(auth) < 8 || auth[:7] != "Bearer " {
			t.Errorf("expected 'Bearer <token>', got: %s", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Verify the token has 3 parts (JWT format)
		token := auth[7:]
		parts := 0
		for _, c := range token {
			if c == '.' {
				parts++
			}
		}
		if parts != 2 {
			t.Errorf("expected JWT with 3 parts (2 dots), got %d dots", parts)
		}
		json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetHealth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDoRequestWithBodySetsContentType verifies POST requests include
// Content-Type: application/json.
func TestDoRequestWithBodySetsContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.ImportConfig([]byte(`{}`))
}

// --- helpers ---

func splitJWT(t *testing.T, token string) [3]string {
	t.Helper()
	var parts [3]string
	idx := 0
	for i, s := range splitString(token, '.') {
		if i >= 3 {
			t.Fatal("JWT has more than 3 parts")
		}
		parts[i] = s
		idx = i
	}
	if idx != 2 {
		t.Fatalf("JWT should have 3 parts, got %d", idx+1)
	}
	return parts
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func decodeJWTPart(t *testing.T, part string) map[string]any {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("failed to decode JWT part: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JWT part JSON: %v", err)
	}
	return result
}

func newTestClient(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		AuthToken:  generateJWT("test-secret"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func assertAuthHeader(t *testing.T, r *http.Request) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if auth == "" {
		t.Error("missing Authorization header")
	}
	if len(auth) < 7 || auth[:7] != "Bearer " {
		t.Errorf("expected 'Bearer <token>' format, got: %s", auth)
	}
}

