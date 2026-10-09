package containd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	_, err := client.ImportConfig(context.Background(), configJSON)
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
	warnings, err := client.ImportConfig(context.Background(), []byte(`{"firewall":{"rules":[]}}`))
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

	warnings, err := newTestClient(srv.URL).ImportConfig(context.Background(), []byte(`{"firewall":{"rules":[]}}`))
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
	if _, err := client.ImportConfig(context.Background(), []byte(`{}`)); err != nil {
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
	_, err := client.ImportConfig(context.Background(), []byte(`{}`))
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
	_, err := client.ImportConfig(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}
