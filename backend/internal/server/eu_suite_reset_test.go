package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestSuiteResetCallsTruthAndPropagatesFailures(t *testing.T) {
	for _, success := range []bool{true, false} {
		fw, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
		gen := euGeneration(t, containd.NewClient(fw.URL), nil)
		s := rangeServer(gen, "improved")
		s.orchestrator = &orchestrator.Orchestrator{}
		s.pcap.FileReady = true
		recipe := *euRecipe
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		recipe.resetTruth = func(ctx context.Context, got *lifecycle.Generation, r *packageRecipe) []resetAction {
			calls++
			if got != gen || r != &recipe {
				t.Fatal("wrong reset context")
			}
			cancel() // avoid the suite's settling sleep; no Docker or live range
			return []resetAction{{Action: "truth delivery", Success: success, Detail: "device acknowledgement"}}
		}
		problems := s.resetLabState(ctx, gen, &recipe)
		found := false
		for _, problem := range problems {
			found = found || strings.Contains(problem, "truth delivery: device acknowledgement")
		}
		if calls != 1 || found == success || s.pcap.FileReady {
			t.Fatalf("calls=%d failures=%v capture=%t", calls, problems, s.pcap.FileReady)
		}
	}
}

func TestPolicyEvidenceBoundaryTracksManualCommits(t *testing.T) {
	s := &Server{activeConfig: "improved", lastAppliedHash: "old", lastAppliedAt: time.Now().Add(-time.Hour), policyChangedAt: time.Now().Add(-time.Hour), policyObservedAt: time.Now().Add(-time.Minute)}
	lower := s.policyObservedAt
	before := time.Now()
	s.classifyObservedPolicy("new", time.Now().UTC())
	if s.policyChangedAt.Before(before) || s.activeConfig != "custom" || !s.policyEvidenceSince.Equal(lower) {
		t.Fatal("manual custom policy retained the old hardening window")
	}
	changed := s.policyChangedAt
	s.classifyObservedPolicy("new", time.Now().UTC())
	if !s.policyChangedAt.Equal(changed) {
		t.Fatal("unchanged policy restarted the evidence window")
	}
}
