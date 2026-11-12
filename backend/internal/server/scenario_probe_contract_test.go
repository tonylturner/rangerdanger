package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
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
			step := labs.ScenarioStep{ExpectedConfig: "hardened", Title: "Probe target", Node: "kali-1", Action: &labs.StepAction{
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
				name, config           string
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
				{name: "improved uses saved request", config: "improved", request: &planSelection{pkg, gen.Package.Revision, []string{"block-enterprise-to-field"}}, target: "your 1.4 change request (1 actions)"},
				{name: "weak reachable matches", config: "weak", target: "the Weak baseline", pass: true},
				{name: "weak refusal matches", config: "weak", target: "the Weak baseline", negativeRC: 1, stderr: "Connection refused", pass: true},
				{name: "weak timeout mismatch", config: "weak", target: "the Weak baseline", negativeRC: 124},
				{name: "weak supersedes applied and request", config: "weak", applied: &full, request: &planSelection{pkg, gen.Package.Revision, all}, target: "the Weak baseline", pass: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					step.ExpectedConfig = "hardened"
					if tc.config != "" {
						step.ExpectedConfig = tc.config
					}
					if err := s.db.Model(&models.Scenario{}).Where("id = ?", "probe-contract").Update("steps", string(mustJSON(t, []labs.ScenarioStep{step}))).Error; err != nil {
						t.Fatal(err)
					}
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
					gen = installProbePolicy(t, s, gen)
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

// Exercise the shipped Weak demonstrations through the suite's real evaluator.
// A baseline observation passes even when a conflicting plan/request exists;
// changing just one observation to a timeout still fails the step.
func TestSuiteProbeJudgesDeclaredWeakBaseline(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			prefix := ""
			if pkg == euPackageID {
				gen, prefix = euGeneration(t, nil, nil), "eu-"
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
			var all []string
			for _, action := range gen.Package.Curriculum.Catalog {
				all = append(all, action.ID)
			}
			full := compileProbePlan(t, gen, all)
			for _, scenarioID := range []string{"baseline-assessment", "firewall-implementation", "vendor-rdp-compromise"} {
				t.Run(scenarioID, func(t *testing.T) {
					scenario, err := s.findPackageScenario(s.activePackage().ID, prefix+scenarioID)
					if err != nil {
						t.Fatal(err)
					}
					var steps []labs.ScenarioStep
					if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
						t.Fatal(err)
					}
					found := false
					for i, step := range steps {
						if step.Action == nil || step.Action.Type != "probe" || step.ExpectedConfig != "weak" {
							continue
						}
						found = true
						for _, applied := range []bool{false, true} {
							for _, rc := range []int{0, 1, 124} {
								t.Run(fmt.Sprintf("step=%d/applied=%t/rc=%d", i, applied, rc), func(t *testing.T) {
									s.policySource, s.appliedPlan, s.planProvenance = "weak", nil, nil
									if applied {
										useAppliedProbePlan(t, s, full)
									}
									first := step.Action.Targets[0]
									s.execInContainer = func(_ context.Context, _ string, cmd []string, _ int) (string, string, int, error) {
										if cmd[4] == first.Host && cmd[5] == fmt.Sprint(first.Port) {
											if rc == 1 {
												return "", "Connection refused", rc, nil
											}
											return "", "", rc, nil
										}
										return "", "", 0, nil
									}
									changed, err := ensureHardenedPrecondition(step, "weak", func(string) ([]string, error) {
										t.Error("suite changed policy to make a baseline probe pass")
										return nil, nil
									})
									if err != nil || changed {
										t.Fatalf("baseline precondition changed: %t %v", changed, err)
									}
									gen = installProbePolicy(t, s, gen)
									ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{full})
									result := evaluateTestStep(i, step, stepExecutors{
										probe: func(step labs.ScenarioStep) []StepActionResult {
											rows := s.executeProbe(ctx, gen, step)
											for _, row := range rows {
												if !strings.Contains(row.Detail, "expected reachable by the Weak baseline") {
													t.Fatal(row)
												}
											}
											return rows
										},
									})
									if result.Passed != (rc != 124) || result.AutoPass {
										t.Fatalf("suite ignored declared target or mismatch: %+v", result)
									}
								})
							}
						}
					}
					if !found {
						t.Fatal("authored baseline probe was not found")
					}
				})
			}
		})
	}
}

func TestProbeWeakUsesPackagedReference(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			if pkg == euPackageID {
				gen = euGeneration(t, nil, nil)
			}
			s := rangeServer(gen, "weak")
			// Keep a real reference envelope, but deliberately make its verdict
			// differ from an empty compiled plan: probes must read the package.
			path, err := s.policyPath(gen.Package, "weak")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			firewall := document["firewall"].(map[string]any)
			firewall["rules"], firewall["defaultAction"] = []any{}, "DENY"
			s.cfg.LabDefinitionsPath = t.TempDir()
			gen.Package.Policies = map[string]string{"weak": "weak.json"}
			path = filepath.Join(s.cfg.LabDefinitionsPath, "weak.json")
			if err := os.WriteFile(path, mustJSON(t, document), 0600); err != nil {
				t.Fatal(err)
			}
			step := labs.ScenarioStep{ExpectedConfig: "weak", Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
			for _, rc := range []int{0, 124} {
				s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) { return "", "", rc, nil }
				gen = installProbePolicy(t, s, gen)
				rows := s.executeProbe(t.Context(), gen, step)
				if len(rows) != 1 || rows[0].Success != (rc == 124) || !strings.Contains(rows[0].Detail, "expected blocked by the Weak baseline") {
					t.Fatal(rows)
				}
			}
			if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				t.Error("probe ran without a valid Weak reference")
				return "", "", 0, nil
			}
			rows := s.executeProbe(t.Context(), gen, step)
			if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "the Weak baseline: judging target cannot be evaluated") {
				t.Fatal(rows)
			}
			delete(gen.Package.Policies, "weak")
			rows = s.executeProbe(t.Context(), gen, step)
			if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "the Weak baseline: judging target unavailable") {
				t.Fatal(rows)
			}
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
			want := "connected"
			if refuse {
				want = "refused"
			}
			if verdict != want || refuse && !strings.Contains(detail, "connection refused") {
				t.Fatalf("real bash output misclassified: %q %q rc=%d err=%v: %s %s", stdout.String(), stderr.String(), rc, err, verdict, detail)
			}
		})
	}
}
