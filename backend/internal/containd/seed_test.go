package containd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestSeedConfigIfNeededSuccess verifies end-to-end seeding: wait for
// containd to become ready, then import the config via candidate/commit.
func TestSeedConfigIfNeededSuccess(t *testing.T) {
	var healthCalls, candidateCalls, commitCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			atomic.AddInt32(&healthCalls, 1)
			json.NewEncoder(w).Encode(HealthStatus{Status: "healthy"})
		case "/api/v1/config/candidate":
			atomic.AddInt32(&candidateCalls, 1)
			w.WriteHeader(http.StatusOK)
		case "/api/v1/config/commit":
			atomic.AddInt32(&commitCalls, 1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// Write a temp config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test-config.json")
	os.WriteFile(cfgPath, []byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`), 0644)

	client := newTestClient(srv.URL)
	SeedConfigIfNeeded(client, cfgPath)

	if atomic.LoadInt32(&healthCalls) < 1 {
		t.Error("expected at least one health check")
	}
	if atomic.LoadInt32(&candidateCalls) != 1 {
		t.Errorf("expected exactly 1 candidate call, got %d", atomic.LoadInt32(&candidateCalls))
	}
	if atomic.LoadInt32(&commitCalls) != 1 {
		t.Errorf("expected exactly 1 commit call, got %d", atomic.LoadInt32(&commitCalls))
	}
}

// TestSeedConfigIfNeededEmptyPath verifies seeding is skipped when no path is set.
func TestSeedConfigIfNeededEmptyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no requests should be made when config path is empty")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	SeedConfigIfNeeded(client, "")
	// Should return immediately without errors
}

// TestSeedConfigIfNeededBadFile verifies seeding handles missing config file gracefully.
func TestSeedConfigIfNeededBadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no requests should be made when config file is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	SeedConfigIfNeeded(client, "/nonexistent/path/config.json")
	// Should log error and return without panicking
}
