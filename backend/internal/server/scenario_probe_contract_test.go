package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

func TestProbeExecuteStepJudgingTargetAndResponse(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			controller := "rtac-1"
			if pkg == euPackageID {
				gen, controller = euGeneration(t, nil, nil), "rtu-1"
			}
			s := rangeServer(gen, "weak")
			s.catalog = &labs.Catalog{Packages: []labs.Package{gen.Package}}
			var err error
			s.db, err = db.Connect(filepath.Join(t.TempDir(), "probe.db"))
			if err != nil {
				t.Fatal(err)
			}
			step := labs.ScenarioStep{Title: "Probe target", Node: "kali-1", Action: &labs.StepAction{
				Type: "probe", Targets: []labs.ProbeTarget{
					{Host: "10.40.40.20", Port: 502},
					{From: controller, Host: "10.40.40.20", Port: 502},
				},
			}}
			if err := s.db.Create(&models.Scenario{ID: "probe-contract", PackageID: pkg, Steps: string(mustJSON(t, []labs.ScenarioStep{step}))}).Error; err != nil {
				t.Fatal(err)
			}
			var all []string
			for _, action := range gen.Package.Curriculum.Catalog {
				all = append(all, action.ID)
			}
			empty := compileProbePlan(t, gen, nil)
			full := compileProbePlan(t, gen, all)
			for _, tc := range []struct {
				name                   string
				request                *planSelection
				applied                *labs.CompiledPlan
				negativeRC, positiveRC int
				stderr, target         string
				pass                   bool
			}{
				{name: "reference reachable mismatch", target: "full reference", positiveRC: 0},
				{name: "reference refusal mismatch", target: "full reference", negativeRC: 1, stderr: "Connection refused"},
				{name: "reference timeout matches", target: "full reference", negativeRC: 124, pass: true},
				{name: "one operational mismatch fails step", target: "full reference", negativeRC: 124, positiveRC: 124},
				{name: "empty request permits deferred exposure", request: &planSelection{pkg, gen.Package.Revision, nil}, target: "your 1.4 change request (0 actions)", pass: true},
				{name: "selected request rejects exposure", request: &planSelection{pkg, gen.Package.Revision, []string{"block-enterprise-to-field"}}, target: "your 1.4 change request (1 actions)"},
				{name: "applied empty supersedes request", applied: &empty, request: &planSelection{pkg, gen.Package.Revision, all}, target: "applied plan", pass: true},
				{name: "applied full supersedes request", applied: &full, request: &planSelection{pkg, gen.Package.Revision, nil}, target: "applied plan"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s.policySource, s.appliedPlan, s.planProvenance = "manual-custom", nil, nil
					if tc.applied != nil {
						useAppliedProbePlan(t, s, *tc.applied)
					}
					s.execInContainer = func(_ context.Context, container string, _ []string, _ int) (string, string, int, error) {
						source, err := nodeService(gen, "kali-1")
						if err != nil {
							return "", "", -1, err
						}
						if container == source.Container {
							return "", tc.stderr, tc.negativeRC, nil
						}
						return "", "", tc.positiveRC, nil
					}
					body := ""
					if tc.request != nil {
						body = string(mustJSON(t, map[string]any{"changeRequest": tc.request}))
					}
					rec := callRoute(s, s.handleExecuteStep, http.MethodPost, "/api/scenarios/:id/steps/:stepIdx/execute", "/api/scenarios/probe-contract/steps/0/execute", body)
					var response StepExecutionResult
					if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil ||
						response.Success != tc.pass || response.ActionType != "probe" || len(response.Results) != 2 {
						t.Fatalf("%d %s", rec.Code, rec.Body)
					}
					for _, row := range response.Results {
						if !strings.Contains(row.Detail, "by "+tc.target) {
							t.Fatal(row)
						}
					}
					var shape map[string]json.RawMessage
					if err := json.Unmarshal(rec.Body.Bytes(), &shape); err != nil || len(shape) != 6 {
						t.Fatalf("changed response shape: %s", rec.Body)
					}
					var rows []map[string]json.RawMessage
					if err := json.Unmarshal(shape["results"], &rows); err != nil || len(rows[0]) != 3 || len(rows[1]) != 3 {
						t.Fatalf("changed row shape: %s", rec.Body)
					}
				})
			}
		})
	}
}

func TestSuiteProbeJudgesReferenceAndAppliedPlan(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			scenarioID := "firewall-implementation"
			if pkg == euPackageID {
				gen, scenarioID = euGeneration(t, nil, nil), "eu-firewall-implementation"
			}
			s := rangeServer(gen, "weak")
			var err error
			s.db, err = db.Connect(filepath.Join(t.TempDir(), "suite-probe.db"))
			if err != nil {
				t.Fatal(err)
			}
			s.catalog, err = labs.NewLoader(s.cfg.LabDefinitionsPath, ValidatorRequirements()).Seed(t.Context(), s.db, pkg)
			if err != nil {
				t.Fatal(err)
			}
			scenario, err := s.findActiveScenario(scenarioID)
			if err != nil {
				t.Fatal(err)
			}
			var steps []labs.ScenarioStep
			if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
				t.Fatal(err)
			}
			for i, step := range steps {
				if step.Action == nil || step.Action.Type != "probe" || step.ExpectedConfig != "weak" {
					continue
				}
				for _, applied := range []bool{false, true} {
					t.Run(fmt.Sprintf("applied-empty-plan=%t", applied), func(t *testing.T) {
						s.policySource, s.appliedPlan, s.planProvenance = "weak", nil, nil
						if applied {
							useAppliedProbePlan(t, s, compileProbePlan(t, gen, nil))
						}
						s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
							return "", "", 0, nil // The authored Weak-baseline exposure.
						}
						changed, err := ensureHardenedPrecondition(step, "weak", func(string) ([]string, error) {
							t.Error("suite changed policy to make a baseline probe pass")
							return nil, nil
						})
						if err != nil || changed {
							t.Fatalf("baseline precondition changed: %t %v", changed, err)
						}
						result := evaluateTestStep(i, step, stepExecutors{
							probe: func(step labs.ScenarioStep) []StepActionResult { return s.executeProbe(t.Context(), gen, step) },
						})
						if result.Passed != applied || result.AutoPass {
							t.Fatalf("suite did not preserve target mismatch: %+v", result)
						}
					})
				}
				return
			}
			t.Fatal("authored baseline probe was not found")
		})
	}
}

func TestScenarioProbeRealBashConnectDiagnostics(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprint(refuse), func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			host, port, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			if refuse {
				if err := listener.Close(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", "exec 3<>/dev/tcp/$1/$2", "_", host, port)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			rc := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				rc = exit.ExitCode()
				err = nil
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			verdict, detail := classifyScenarioProbe(stdout.String(), stderr.String(), rc, err)
			if verdict != "reachable" || refuse && !strings.Contains(detail, "connection refused") {
				t.Fatalf("real bash output misclassified: %q %q rc=%d err=%v: %s %s", stdout.String(), stderr.String(), rc, err, verdict, detail)
			}
		})
	}
}
