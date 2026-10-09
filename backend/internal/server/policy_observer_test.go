package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// hashServerForFirewall stands up a tiny HTTP server that returns
// the firewall sub-document we control on each request. Lets the
// observer test drive the "running config changes out from under
// us" scenario deterministically without a real containd.
func hashServerForFirewall(t *testing.T, firewall any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"firewall": firewall})
	}))
}

// TestObserver_NoChange_NoReclassify confirms the observer stays
// quiet when containd's running hash equals lastAppliedHash.
func TestObserver_NoChange_NoReclassify(t *testing.T) {
	fw := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	ts := hashServerForFirewall(t, fw)
	defer ts.Close()

	s := &Server{
		activeConfig:  "improved",
		policySource:  "hardened-reference",
		lastAppliedAt: time.Now().Add(-1 * time.Hour), // outside grace
	}
	// Seed lastAppliedHash with the same hash containd will return.
	configBytes, _ := json.Marshal(map[string]any{"firewall": fw})
	h, err := containd.FirewallHashFromBytes(configBytes)
	if err != nil {
		t.Fatalf("seed hash: %v", err)
	}
	s.lastAppliedHash = h

	s.observePolicyOnce(context.Background(), observedGeneration(ts.URL))

	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "improved" {
		t.Errorf("activeConfig = %q, want improved (no change expected)", s.activeConfig)
	}
	if s.policySource != "hardened-reference" {
		t.Errorf("policySource = %q, want hardened-reference (no change expected)", s.policySource)
	}
}

// TestObserver_HashDivergence_FlipsManualCustom is the core
// regression test for the technical-track use case. A student
// commits a different policy directly in containd; the observer
// should set activeConfig=custom + policySource=manual-custom.
func TestObserver_HashDivergence_FlipsManualCustom(t *testing.T) {
	// Containd reports a different firewall than what we last applied.
	containdSays := map[string]any{
		"defaultAction": "DENY",
		"rules": []any{
			map[string]any{
				"id":          "student-authored",
				"action":      "ALLOW",
				"sourceZones": []string{"lan1"},
				"destZones":   []string{"lan2"},
				"protocols":   []any{map[string]any{"name": "tcp", "port": "502"}},
			},
		},
	}
	ts := hashServerForFirewall(t, containdSays)
	defer ts.Close()

	s := &Server{
		activeConfig: "improved",
		policySource: "hardened-reference",
		// Seed with a DIFFERENT hash, so divergence fires.
		lastAppliedHash: "deadbeef",
		// Apply happened well outside the grace window.
		lastAppliedAt: time.Now().Add(-1 * time.Hour),
	}

	s.observePolicyOnce(context.Background(), observedGeneration(ts.URL))

	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "custom" {
		t.Errorf("activeConfig = %q, want custom", s.activeConfig)
	}
	if s.policySource != "manual-custom" {
		t.Errorf("policySource = %q, want manual-custom", s.policySource)
	}
	if s.lastAppliedHash == "deadbeef" {
		t.Errorf("lastAppliedHash was not updated to the observed value (would cause re-firing every tick)")
	}
}

// TestObserver_GraceWindow_SuppressesFlip protects against a race
// where the apply handler records lastAppliedHash but containd
// hasn't finished the candidate → commit cycle yet. The observer
// must not reclassify during the grace window.
func TestObserver_GraceWindow_SuppressesFlip(t *testing.T) {
	containdSays := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	ts := hashServerForFirewall(t, containdSays)
	defer ts.Close()

	s := &Server{
		activeConfig:    "improved",
		policySource:    "hardened-reference",
		lastAppliedHash: "deadbeef", // would diverge
		lastAppliedAt:   time.Now(), // INSIDE the grace window
	}

	s.observePolicyOnce(context.Background(), observedGeneration(ts.URL))

	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "improved" {
		t.Errorf("activeConfig = %q, want improved (grace window should have suppressed)", s.activeConfig)
	}
}

// TestObserver_FirstObservation_SeedsBaseline confirms that on the
// first tick after server start (no lastAppliedHash set yet), the
// observer adopts the current hash silently instead of firing a
// spurious "manual commit" flip.
func TestObserver_FirstObservation_SeedsBaseline(t *testing.T) {
	containdSays := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	ts := hashServerForFirewall(t, containdSays)
	defer ts.Close()

	s := &Server{
		activeConfig: "weak",
		policySource: "",
		// no lastAppliedHash set
	}

	s.observePolicyOnce(context.Background(), observedGeneration(ts.URL))

	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "weak" {
		t.Errorf("activeConfig = %q, want weak (baseline seed should not reclassify)", s.activeConfig)
	}
	if s.lastAppliedHash == "" {
		t.Errorf("lastAppliedHash still empty — first observation should seed it")
	}
}

// TestObserver_ConcurrentApplyAndObserve confirms the lock keeps
// the observer + an apply handler from racing on the shared state.
// This is a smoke check (not a true race detector run); pair with
// `go test -race ./internal/server/...` for the deeper guarantee.
func TestObserver_ConcurrentApplyAndObserve(t *testing.T) {
	containdSays := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	ts := hashServerForFirewall(t, containdSays)
	defer ts.Close()

	s := &Server{
		activeConfig: "weak",
	}
	gen := observedGeneration(ts.URL)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.observePolicyOnce(context.Background(), gen)
		}()
		go func() {
			defer wg.Done()
			s.activeConfigMu.Lock()
			s.recordApplyLocked([]byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`))
			s.activeConfigMu.Unlock()
		}()
	}
	wg.Wait()
	// If we get here without -race shouting, the lock discipline
	// is at least consistent. The assertion is the absence of a
	// panic / data-race report.
}

// observedGeneration is a live generation whose firewall is url.
func observedGeneration(url string) *lifecycle.Generation {
	return lifecycle.NewGeneration(1, labs.Package{}, &manifest.Manifest{}, containd.NewClient(url))
}

// TestObserver_StoppedGenerationDropsObservation: an observation that
// lands after its range stopped must not relabel the next range's policy.
func TestObserver_StoppedGenerationDropsObservation(t *testing.T) {
	ts := hashServerForFirewall(t, map[string]any{"defaultAction": "ALLOW", "rules": []any{}})
	defer ts.Close()
	s := &Server{
		activeConfig:    "weak",
		lastAppliedHash: "deadbeef",
		lastAppliedAt:   time.Now().Add(-1 * time.Hour),
	}
	gen := observedGeneration(ts.URL)
	if err := gen.Stop(time.Second); err != nil {
		t.Fatal(err)
	}

	s.observePolicyOnce(context.Background(), gen)

	s.activeConfigMu.RLock()
	defer s.activeConfigMu.RUnlock()
	if s.activeConfig != "weak" || s.lastAppliedHash != "deadbeef" {
		t.Errorf("state = %q %q, want the stale observation dropped", s.activeConfig, s.lastAppliedHash)
	}
}
