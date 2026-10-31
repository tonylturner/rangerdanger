package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/config"
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
	ts, _ := mutableHashServerForFirewall(t, firewall)
	return ts
}

func mutableHashServerForFirewall(t *testing.T, firewall any) (*httptest.Server, func(any)) {
	t.Helper()
	var mu sync.RWMutex
	current := firewall
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			http.NotFound(w, r)
			return
		}
		mu.RLock()
		observed := current
		mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"firewall": observed})
	}))
	return ts, func(next any) {
		mu.Lock()
		current = next
		mu.Unlock()
	}
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

// TestObserver_FirstObservationClassifiesRunningPolicy covers fresh starts,
// adopted ranges, and the next genuine divergence after the baseline.
func TestObserver_FirstObservationClassifiesRunningPolicy(t *testing.T) {
	weak := map[string]any{"defaultAction": "DENY", "rules": []any{}}
	improved := map[string]any{"defaultAction": "DROP", "rules": []any{}}
	unknown := map[string]any{
		"defaultAction": "DENY",
		"rules": []any{
			map[string]any{"id": "manual-rule", "action": "ALLOW", "sourceZones": []string{"lan1"}, "destZones": []string{"lan2"}},
		},
	}
	tests := []struct {
		name             string
		running          any
		wantConfig       string
		wantSource       string
		wantBoundary     bool
		divergeAfterSeed bool
	}{
		{"fresh default", weak, "weak", "weak", false, false},
		{"adopted improved", improved, "improved", "hardened-reference", true, false},
		{"adopted custom", unknown, "custom", "manual-custom", true, false},
		{"later divergence", improved, "improved", "hardened-reference", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definitions := t.TempDir()
			policies := map[string]any{"weak": weak, "improved": improved}
			for name, firewall := range policies {
				data, err := json.Marshal(map[string]any{"firewall": firewall})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(definitions, name+".json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			ts, setFirewall := mutableHashServerForFirewall(t, tt.running)
			defer ts.Close()
			pkg := labs.Package{
				ID:                 "test-package",
				FirewallConfigPath: "weak.json",
				Policies:           map[string]string{"weak": "weak.json", "improved": "improved.json"},
			}
			startedAt := time.Now().UTC().Add(-time.Hour)
			gen := lifecycle.NewGeneration(1, pkg, &manifest.Manifest{}, containd.NewClient(ts.URL), startedAt)
			defer func() {
				if err := gen.Stop(time.Second); err != nil {
					t.Errorf("stop generation: %v", err)
				}
			}()
			s := &Server{cfg: &config.Config{LabDefinitionsPath: definitions}}
			s.activateRange(gen)
			observationStart := time.Now().UTC()
			s.observePolicyOnce(context.Background(), gen)
			observationEnd := time.Now().UTC()

			s.activeConfigMu.RLock()
			gotConfig, gotSource, gotHash := s.activeConfig, s.policySource, s.lastAppliedHash
			policyChangedAt, policyEvidenceSince := s.policyChangedAt, s.policyEvidenceSince
			s.activeConfigMu.RUnlock()
			if gotConfig != tt.wantConfig || gotSource != tt.wantSource {
				t.Errorf("first observation = %q/%q, want %q/%q", gotConfig, gotSource, tt.wantConfig, tt.wantSource)
			}
			if gotHash == "" {
				t.Error("first observation did not seed the running hash")
			}
			if tt.wantBoundary {
				if !policyEvidenceSince.Equal(startedAt) {
					t.Errorf("policyEvidenceSince = %s, want generation start %s", policyEvidenceSince, startedAt)
				}
				if policyChangedAt.Before(observationStart) || policyChangedAt.After(observationEnd) {
					t.Errorf("policyChangedAt = %s, want observation window [%s, %s]", policyChangedAt, observationStart, observationEnd)
				}
			} else if !policyChangedAt.IsZero() || !policyEvidenceSince.IsZero() {
				t.Errorf("default policy boundaries = %s/%s, want both zero", policyChangedAt, policyEvidenceSince)
			}

			if tt.divergeAfterSeed {
				setFirewall(unknown)
				s.observePolicyOnce(context.Background(), gen)
				s.activeConfigMu.RLock()
				gotConfig, gotSource = s.activeConfig, s.policySource
				s.activeConfigMu.RUnlock()
				if gotConfig != "custom" || gotSource != "manual-custom" {
					t.Errorf("later divergence = %q/%q, want custom/manual-custom", gotConfig, gotSource)
				}
			}
		})
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
	return lifecycle.NewGeneration(1, labs.Package{}, &manifest.Manifest{}, containd.NewClient(url), time.Time{})
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

// The first response is delayed while a second manual commit happens. Neither
// response completion time can be the next observation's safe lower bound.
func TestObserver_DelayedSuccessiveManualCommits(t *testing.T) {
	requestStart := make(chan time.Time, 2)
	release := make(chan struct{})
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		requestStart <- time.Now().UTC()
		<-release
		fmt.Fprintf(w, `{"firewall":{"defaultAction":"DENY","rules":[],"comment":"commit-%d"}}`, calls)
	}))
	defer ts.Close()
	s := &Server{lastAppliedHash: "old", policyObservedAt: time.Now().Add(-time.Hour).UTC()}
	gen := observedGeneration(ts.URL)
	var previousRequest time.Time
	for i := 0; i < 2; i++ {
		before := time.Now().UTC()
		done := make(chan struct{})
		go func() { s.observePolicyOnce(t.Context(), gen); close(done) }()
		handlerStart := <-requestStart
		responseStart := time.Now().UTC()
		release <- struct{}{}
		<-done
		if s.policyObservedAt.Before(before) || s.policyObservedAt.After(handlerStart) || s.policyChangedAt.Before(responseStart) {
			t.Fatalf("request bound=%s upper=%s", s.policyObservedAt, s.policyChangedAt)
		}
		if i == 1 && !s.policyEvidenceSince.Equal(previousRequest) {
			t.Fatal("lost first request's lower bound")
		}
		if s.policyObservedAt.Location() != time.UTC || s.policyChangedAt.Location() != time.UTC || s.policyEvidenceSince.Location() != time.UTC {
			t.Fatal("non-UTC policy evidence")
		}
		previousRequest = s.policyObservedAt
	}
	s.recordApplyLocked([]byte(`{"defaultAction":"DENY","rules":[]}`))
	for _, at := range []time.Time{s.lastAppliedAt, s.policyChangedAt, s.policyObservedAt, s.policyEvidenceSince} {
		if at.Location() != time.UTC {
			t.Fatal("non-UTC apply timestamp")
		}
	}
}
