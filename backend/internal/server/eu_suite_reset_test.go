package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestSuiteRetainsLastReceiptAndStopsOnConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			status := 200
			if conflict {
				status = 412
			}
			fw, counts := fakeContaind(t, status, 200)
			readyState := deliveredState(t, resetRevisions)
			devices := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/process/api/electrical":
					fmt.Fprint(w, readyState)
				case "/process/api/process/actions":
					fmt.Fprint(w, electricalFixture)
				case "/gps-1/api/command":
					fmt.Fprint(w, `{"result":"executed"}`)
				default:
					node := strings.Split(r.URL.Path, "/")[1]
					if node == "breaker-1" {
						node = "feeder-breaker"
					}
					fmt.Fprintf(w, `{"schema":3,"device_id":%q,"revision":%d}`, node, resetRevisions[node])
				}
			}))
			defer devices.Close()
			gen := multiDeviceGeneration(t, devices.URL)
			gen = lifecycle.NewGeneration(gen.ID, gen.Package, gen.Manifest, containd.NewClient(fw.URL), gen.StartedAt)
			s := rangeServer(gen, "improved")
			s.orchestrator = &orchestrator.Orchestrator{}
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				return "", "", 0, nil // weak baseline's allow canary
			}
			seedEUScenario(t, s)
			if err := s.db.Where("id <> ?", "eu-hardening-configurations").Delete(&models.Scenario{}).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(&models.Scenario{}).Where("id = ?", "eu-hardening-configurations").Update("steps", "[]").Error; err != nil {
				t.Fatal(err)
			}
			rec := callRoute(s, s.handleWorkshopTestSuite, "POST", "/api/workshop/test-suite", "/api/workshop/test-suite", "")
			var result struct {
				testSuiteResult
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if conflict {
				if rec.Code != 409 || counts.candidateCount != 1 || counts.commitCount != 0 || result.Code != containd.ConfigConflictCode {
					t.Fatalf("conflicting suite continued applying: %d %s", rec.Code, rec.Body)
				}
			} else if rec.Code != 200 || counts.commitCount != 2 || result.Containd == nil ||
				result.Containd.ApplyID != "apply-5" || result.Containd.Protection != "protecting" {
				t.Fatalf("suite lost last commit receipt: %d %s (commits=%d)", rec.Code, rec.Body, counts.commitCount)
			}
		})
	}
}

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
		problems, applied, err := s.resetLabState(ctx, gen, &recipe)
		if err != nil || applied.Receipt.Protection != "protecting" {
			t.Fatalf("reset apply=%+v err=%v", applied, err)
		}
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
