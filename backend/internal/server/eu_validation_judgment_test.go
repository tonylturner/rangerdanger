package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func TestEUValidationJudgesRunningPolicy(t *testing.T) {
	const slot = "eu-validation-evidence"
	ev := passingEUWorkshop(t, slot)
	now := time.Now().UTC()
	for i := range ev.journal.Entries {
		ev.journal.Entries[i].At = now.Add(-time.Second)
	}
	for i := range ev.cc.Points {
		ev.cc.Points[i].ReceivedAt = now.Add(-time.Second)
	}
	var running []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("validation changed the range: %s %s", r.Method, r.URL.Path)
		}
		var body any
		switch r.URL.Path {
		case "/api/v1/config":
			w.Write(running)
			return
		case "/process/api/electrical":
			process := *ev.process
			process.SolvedAt = time.Now().UTC()
			body = process
		case "/cc-1/api/state":
			body = ev.cc
		case "/cc-1/api/journal":
			body = ev.journal
		default:
			node := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/api/state")
			body = ev.devices[node]
			if body == nil {
				t.Errorf("unexpected evidence endpoint: %s", r.URL.Path)
				http.NotFound(w, r)
				return
			}
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	base := multiDeviceGeneration(t, upstream.URL)
	gen := lifecycle.NewGeneration(base.ID, base.Package, base.Manifest, containd.NewClient(upstream.URL), base.StartedAt)
	s := rangeServer(gen, "custom")
	s.policySource = "manual-custom"
	s.policyChangedAt = now.Add(-time.Minute)
	var err error
	s.db, err = db.Connect(filepath.Join(t.TempDir(), "grading.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.catalog, err = labs.NewLoader(s.cfg.LabDefinitionsPath, ValidatorRequirements()).Seed(t.Context(), s.db, gen.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := s.findPackageScenario(s.activePackage().ID, slot)
	if err != nil {
		t.Fatal(err)
	}
	selection := planSelection{gen.Package.ID, gen.Package.Revision, []string{"pin-rtu-to-field"}}
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Course.Features.SegmentationPlan.Model, selection.ActionIDs)
	if err != nil {
		t.Fatal(err)
	}
	// Like the manual-policy judging fixtures, change rule metadata without
	// changing L4 meaning: this is a custom policy, not an applied plan.
	var manual map[string]any
	if err := json.Unmarshal(compiled.Config, &manual); err != nil {
		t.Fatal(err)
	}
	firewall := manual["firewall"].(map[string]any)
	rules := firewall["rules"].([]any)
	for _, rule := range rules {
		rule.(map[string]any)["id"] = "student-" + rule.(map[string]any)["id"].(string)
	}
	matching := mustJSON(t, manual)
	firewall["rules"] = append([]any{map[string]any{"id": "broad", "action": "ALLOW"}}, rules...)
	open := mustJSON(t, manual)
	request := string(mustJSON(t, map[string]any{"changeRequest": selection}))
	for _, tc := range []struct {
		name, body, outcome, target string
		policy                      []byte
	}{
		{"matches request", request, "PASS", "your 1.4 change request (1 actions)", matching},
		{"prohibited path open against request", request, "FAIL", "your 1.4 change request (1 actions)", open},
		{"prohibited path open against reference", "", "FAIL", "full reference (no change request saved)", open},
		{"partial policy without request", "", "FAIL", "full reference (no change request saved)", matching},
	} {
		t.Run(tc.name, func(t *testing.T) {
			running = tc.policy
			s.lastAppliedHash, err = containd.FirewallHashFromBytes(running)
			if err != nil {
				t.Fatal(err)
			}
			rec := callRoute(s, s.handleValidateScenario, http.MethodPost, "/api/scenarios/:id/validate", "/api/scenarios/"+scenario.ID+"/validate", tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			var result ValidationResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.outcome {
				t.Fatalf("outcome = %s; want %s: %+v", result.Outcome, tc.outcome, result.Checks)
			}
			// Preserve all four existing prerequisites, with no active TCP
			// matrix or retained report added to this hand-tested lab.
			names := []string{
				"Running policy identity",
				"CC link, GI and station delivery",
				"All field devices delivered at current revisions",
				"Feeder supply present",
				"Running policy matches judging target",
			}
			if len(result.Checks) != len(names) {
				t.Fatalf("unexpected checks: %+v", result.Checks)
			}
			for i, check := range result.Checks {
				if check.Name != names[i] {
					t.Fatalf("check %d = %+v; want %s", i, check, names[i])
				}
				want := "pass"
				if i == len(names)-1 {
					if tc.outcome == "FAIL" {
						want = "fail"
					}
					if !strings.Contains(check.Detail, tc.target) {
						t.Fatalf("judgment did not name target %q: %+v", tc.target, check)
					}
					if want == "fail" && !strings.Contains(check.Detail, "running allow; judging target deny") {
						t.Fatalf("failure did not identify the prohibited open path: %+v", check)
					}
				}
				if check.Status != want {
					t.Fatalf("check = %+v; want %s", check, want)
				}
			}
		})
	}
}
