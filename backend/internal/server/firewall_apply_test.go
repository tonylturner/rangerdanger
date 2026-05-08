package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// firewall_apply_test exercises the HTTP handlers (handleFirewallApply
// and handleFirewallApplyCustom) end-to-end against a real
// containd.Client wired to an httptest fake. The pure compare logic
// is covered by firewall_compare_test.go; this file is the integration
// layer that gates the path Lab 2.2 / 2.4 / firewall-smoke all use.
//
// Why httptest instead of a mock interface: the Server holds
// *containd.Client (concrete type, not an interface). The existing
// containd/client_test.go already pioneered the httptest-fake-server
// pattern — same approach here keeps the test surface honest (the
// real client's request shape, JWT injection, and dataplane-
// enforcement shim all run as in production).

// fakeContaind builds an httptest server that accepts the
// candidate/commit flow and reports back what it saw via the
// returned counters. Counters are atomic so concurrent calls don't
// race during a multi-call test.
type fakeContaindCounters struct {
	candidateCount int32
	commitCount    int32
	importCount    int32 // legacy fallback path
	lastCandidate  []byte
}

func fakeContaind(t *testing.T, candidateStatus, commitStatus int) (*httptest.Server, *fakeContaindCounters) {
	t.Helper()
	c := &fakeContaindCounters{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			body, _ := io.ReadAll(r.Body)
			c.lastCandidate = body
			atomic.AddInt32(&c.candidateCount, 1)
			w.WriteHeader(candidateStatus)
		case "/api/v1/config/commit":
			atomic.AddInt32(&c.commitCount, 1)
			w.WriteHeader(commitStatus)
		case "/api/v1/config/import":
			atomic.AddInt32(&c.importCount, 1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected containd path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// newTestServer constructs a minimal Server with just enough wiring
// to exercise the firewall_apply handlers. The handlers don't touch
// db / loader / orchestrator, so those stay nil; LabDefinitionsPath
// resolves via runtime.Caller back to the repo root.
func newTestServer(t *testing.T, containdURL string) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	cfg := &config.Config{
		LabDefinitionsPath: filepath.Join(repoRoot, "lab-definitions"),
	}
	return &Server{
		engine:         gin.New(),
		cfg:            cfg,
		containdClient: containd.NewClient(containdURL),
		activeConfig:   "weak",
	}
}

// invoke wraps a Gin handler call so we don't have to repeat the
// router/request/recorder wiring in every test.
func invoke(s *Server, handler gin.HandlerFunc, method, path string, body any) (*httptest.ResponseRecorder, gin.H) {
	router := gin.New()
	router.Handle(method, path, handler)

	var bodyReader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			bodyReader = strings.NewReader(b)
		case []byte:
			bodyReader = bytes.NewReader(b)
		default:
			j, _ := json.Marshal(b)
			bodyReader = bytes.NewReader(j)
		}
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var parsed gin.H
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

// ── handleFirewallApply ─────────────────────────────────────────────

func TestHandleFirewallApply_Weak(t *testing.T) {
	fake, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, body := invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply",
		map[string]string{"config": "weak"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["status"] != "applied" || body["active_config"] != "weak" {
		t.Errorf("body = %+v, want status=applied active_config=weak", body)
	}
	if atomic.LoadInt32(&counters.candidateCount) != 1 {
		t.Errorf("expected 1 candidate POST, got %d", counters.candidateCount)
	}
	if atomic.LoadInt32(&counters.commitCount) != 1 {
		t.Errorf("expected 1 commit POST, got %d", counters.commitCount)
	}
	// activeConfig state should follow.
	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "weak" {
		t.Errorf("activeConfig = %q, want weak", s.activeConfig)
	}
}

func TestHandleFirewallApply_Improved(t *testing.T) {
	fake, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, body := invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply",
		map[string]string{"config": "improved"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["active_config"] != "improved" {
		t.Errorf("active_config = %v, want improved", body["active_config"])
	}
	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "improved" {
		t.Errorf("activeConfig = %q, want improved", s.activeConfig)
	}
}

func TestHandleFirewallApply_RejectsUnknownName(t *testing.T) {
	fake, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, _ := invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply",
		map[string]string{"config": "improved-but-typo"})

	// applyFirewallConfigInternal rejects with a fmt.Errorf; the
	// handler maps that to 502 Bad Gateway.
	if rec.Code == http.StatusOK {
		t.Errorf("status = 200, want non-2xx for unknown config")
	}
	if atomic.LoadInt32(&counters.candidateCount) != 0 {
		t.Errorf("containd should not be called for invalid config name; saw %d candidates",
			counters.candidateCount)
	}
}

func TestHandleFirewallApply_BadJSON(t *testing.T) {
	fake, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, _ := invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply",
		"{not json")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON", rec.Code)
	}
}

func TestHandleFirewallApply_PropagatesContaindFailure(t *testing.T) {
	// Both endpoints fail — the legacy /import fallback also hits the
	// fake which returns 200 above, so to make this fail end-to-end we
	// stand up a fake whose /import path also errors.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	s := newTestServer(t, srv.URL)

	rec, body := invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply",
		map[string]string{"config": "weak"})

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (containd error should bubble up as bad gateway)", rec.Code)
	}
	if errMsg, ok := body["error"].(string); !ok || errMsg == "" {
		t.Errorf("expected non-empty error in body, got: %+v", body)
	}
}

// ── handleFirewallApplyCustom ───────────────────────────────────────

// minimalValidCustom is the smallest config the handler accepts —
// non-empty firewall.rules and non-empty interfaces. The contents
// don't need to be schematically valid; the handler only checks
// presence + container shape. Containd would reject malformed
// rules later but this layer's contract is "passes the validation
// gate and forwards to containd."
const minimalValidCustom = `{
  "interfaces": [{"name":"wan","device":"eth0","zone":"wan"}],
  "firewall":   {"rules": [{"id":"r1","action":"DENY"}]}
}`

func TestHandleFirewallApplyCustom_Success(t *testing.T) {
	fake, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, body := invoke(s, s.handleFirewallApplyCustom, "POST", "/api/firewall/apply-custom",
		minimalValidCustom)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["active_config"] != "custom" {
		t.Errorf("active_config = %v, want \"custom\"", body["active_config"])
	}
	if atomic.LoadInt32(&counters.candidateCount) != 1 {
		t.Errorf("expected 1 candidate POST, got %d", counters.candidateCount)
	}
	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "custom" {
		t.Errorf("activeConfig = %q, want \"custom\"", s.activeConfig)
	}
}

func TestHandleFirewallApplyCustom_RejectsEmptyRules(t *testing.T) {
	fake, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	cfg := `{"interfaces":[{"name":"wan"}],"firewall":{"rules":[]}}`
	rec, _ := invoke(s, s.handleFirewallApplyCustom, "POST", "/api/firewall/apply-custom", cfg)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty rules", rec.Code)
	}
	if atomic.LoadInt32(&counters.candidateCount) != 0 {
		t.Errorf("containd must not be called when validation rejects; saw %d", counters.candidateCount)
	}
}

func TestHandleFirewallApplyCustom_RejectsEmptyInterfaces(t *testing.T) {
	fake, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	cfg := `{"interfaces":[],"firewall":{"rules":[{"id":"r1"}]}}`
	rec, _ := invoke(s, s.handleFirewallApplyCustom, "POST", "/api/firewall/apply-custom", cfg)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty interfaces", rec.Code)
	}
	if atomic.LoadInt32(&counters.candidateCount) != 0 {
		t.Errorf("containd must not be called; saw %d", counters.candidateCount)
	}
}

func TestHandleFirewallApplyCustom_RejectsBadJSON(t *testing.T) {
	fake, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	rec, _ := invoke(s, s.handleFirewallApplyCustom, "POST", "/api/firewall/apply-custom", "not json")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid JSON", rec.Code)
	}
}

func TestHandleFirewallApplyCustom_RejectsOversizedBody(t *testing.T) {
	fake, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
	s := newTestServer(t, fake.URL)

	// Handler caps request body at 512 KiB. Generate a payload that
	// exceeds that — its actual content doesn't matter because the
	// MaxBytesReader truncates before unmarshal sees it.
	big := strings.Repeat("a", 600*1024)
	rec, _ := invoke(s, s.handleFirewallApplyCustom, "POST", "/api/firewall/apply-custom", big)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversized body", rec.Code)
	}
}
