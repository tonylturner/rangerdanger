package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

const resetPolicy = `{"firewall":{"defaultAction":"DENY","rules":[{"id":"student-reset","action":"RESET"}]}}`
const wantResetDetail = "blocked by RESET (student-reset): the timeout test cannot prove it; use DENY"

func TestRunningDefaultRejectsReset(t *testing.T) {
	if _, err := readEvaluatedFirewall([]byte(`{"firewall":{"defaultAction":"RESET","rules":[]}}`)); err == nil {
		t.Fatal("RESET default accepted")
	}
}

func TestStrictResetNeverProvesClosure(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		rc           int
	}{
		{"connected", "", 0}, {"refused", "Connection refused", 1}, {"coincidental timeout", "", 124}, {"missing tool", "tool missing", 127},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, _ := euProbeDockerFixture(t, tc.rc, tc.stderr, false, nil)
			p := validationProbe{Src: "source", Dst: "10.40.40.20", Port: "502", Expected: "deny", Decision: labs.Decision{Verdict: labs.VerdictReset, RuleID: "student-reset"}}
			row := (&Server{}).strictTCPProbe(t.Context(), cli, p)
			if row.Actual != "reset" || row.Pass || row.ProbeError != wantResetDetail {
				t.Fatal(row)
			}
		})
	}
}

func TestScenarioResetUsesRunningNotJudgingPolicy(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		for _, rc := range []int{0, 1, 124, 127} {
			t.Run(fmt.Sprintf("%s/%d", pkg, rc), func(t *testing.T) {
				gen := usGeneration(nil)
				if pkg == euPackageID {
					gen = euGeneration(t, nil, nil)
				}
				s := rangeServer(gen, "custom")
				gen = installRunningProbePolicy(t, s, gen, []byte(resetPolicy))
				s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
					stderr := ""
					if rc == 1 {
						stderr = "Connection refused"
					}
					return "", stderr, rc, nil
				}
				step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
				rows := s.executeProbe(t.Context(), gen, step)
				if len(rows) != 1 || rows[0].Success || rows[0].Detail != wantResetDetail {
					t.Fatal(rows)
				}
			})
		}
	}
}

func TestScenarioProbePolicyBoundaryFailsClosed(t *testing.T) {
	for _, mutation := range []string{"live change", "same hash recommit", "recorded change", "unavailable before"} {
		t.Run(mutation, func(t *testing.T) {
			data := []byte(`{"firewall":{"defaultAction":"ALLOW","rules":[]}}`)
			var changed atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mutation == "unavailable before" {
					w.WriteHeader(503)
					return
				}
				if changed.Load() && mutation == "live change" {
					fmt.Fprint(w, resetPolicy)
					return
				}
				w.Write(data)
			}))
			defer upstream.Close()
			gen := usGeneration(containd.NewClient(upstream.URL))
			s := rangeServer(gen, "custom")
			s.lastAppliedHash, _ = containd.FirewallHashFromBytes(data)
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				changed.Store(true)
				if mutation == "same hash recommit" {
					s.policyChangedAt = s.policyChangedAt.Add(time.Second)
				}
				if mutation == "recorded change" {
					s.lastAppliedHash += "-different"
				}
				return "", "", 124, nil
			}
			step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
			rows := s.executeProbe(t.Context(), gen, step)
			if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "policy") {
				t.Fatal(rows)
			}
			if mutation == "unavailable before" && changed.Load() {
				t.Fatal("ran without running authority")
			}
		})
	}
}

func TestResetCommandNeverForwardedAndPolicyJudgmentNamesRule(t *testing.T) {
	s := rangeServer(usGeneration(nil), "custom")
	gen := installRunningProbePolicy(t, s, s.rng.(*fakeRange).gen, []byte(resetPolicy))
	// There is no usable RTAC endpoint: forwarding would fail, not report blocked.
	row := s.executeRTACCommand(t.Context(), gen, usRecipe, "recloser", "open", "10.10.10.50", nil)
	if !row.Blocked || row.Success || row.Detail != wantResetDetail {
		t.Fatal(row)
	}
	check := s.policyJudgment(t.Context(), gen)
	if check.Status != "fail" || !strings.Contains(check.Detail, wantResetDetail) {
		t.Fatal(check)
	}
	// An otherwise impossible authored RESET also must never count as admission
	// in retained-audit classification.
	plan := compileProbePlan(t, gen, nil)
	plan.Config = []byte(resetPolicy)
	s.policySource = "plan"
	s.appliedPlan = &plan
	s.planProvenance = &planProvenance{}
	check = s.usAuditJudgment(t.Context(), gen, []map[string]any{{"source": "10.10.10.50", "target": "regulator", "command": "set_tap", "result": "executed"}})
	if check.Status != "warn" {
		t.Fatal(check)
	}
}

func TestResetCanaryWaitsForRefusal(t *testing.T) {
	setFirewallWaitBudget(t, 2*time.Second)
	firewall := map[string]any{"defaultAction": "ALLOW", "rules": []any{map[string]any{"id": "student-reset", "action": "RESET"}}}
	s := firewallWaitPolicyTestServer(t, usPackageID, "custom", firewall, firewall)
	calls := scriptedFirewallCanary(t, s, []canaryObservation{{rc: 0}, {rc: 124}, {rc: 1, stderr: "Connection refused"}})
	if err := s.waitForFirewallPolicy(t.Context(), s.rng.(*fakeRange).gen); err != nil || *calls != 3 {
		t.Fatalf("calls=%d err=%v", *calls, err)
	}
	for _, observation := range []string{"connected", "timeout", "skipped"} {
		if canaryObservationMatches(observation, "reset") {
			t.Fatal(observation)
		}
	}
}

func TestProbeMatrixResetAndRuleID(t *testing.T) {
	s := rangeServer(usGeneration(nil), "custom")
	gen := installRunningProbePolicy(t, s, s.rng.(*fakeRange).gen, []byte(resetPolicy))
	gen.StartedAt = time.Now().Add(-time.Hour)
	s.policyChangedAt = time.Now().Add(-time.Minute)
	cli, _ := euProbeDockerFixture(t, 124, "", false, nil)
	result, err := s.measureProbeMatrix(t.Context(), gen, func(ctx context.Context, p validationProbe) validationRow { return s.strictTCPProbe(ctx, cli, p) })
	if err != nil || !result.PolicyStable {
		t.Fatalf("%+v %v", result, err)
	}
	expectations, err := s.judgingExpectations(gen)
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := resolveProof(gen, expectations, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range result.Rows {
		if matrix[i].Route.Bridged {
			if row.RuleID != "" || row.Observed != "deny" {
				t.Fatal(row)
			}
		} else if row.RuleID != "student-reset" || row.Observed != "reset" || row.Error != wantResetDetail {
			t.Fatal(row)
		}
	}
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), `"ruleId":"student-reset"`) {
		t.Fatal(string(raw))
	}
}

func TestAuthorizedRetryNeverRetriesReset(t *testing.T) {
	for _, actual := range []string{"deny", "reset", "allow", "skipped"} {
		calls := 0
		rows := runEUProbeMatrix(t.Context(), []validationProbe{{Category: "authorized"}}, func(_ context.Context, p validationProbe) validationRow {
			calls++
			return validationRow{validationProbe: p, Actual: actual}
		})
		want := 1
		if actual == "deny" {
			want = 2
		}
		if calls != want || len(rows) != 1 || rows[0].Actual != actual {
			t.Fatalf("%s calls=%d rows=%+v", actual, calls, rows)
		}
	}
}

func TestResetSurvivesCancelledTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := validationProbe{Src: "source", Dst: "10.40.40.20", Port: "502", Expected: "deny", Decision: labs.Decision{Verdict: labs.VerdictReset, RuleID: "student-reset"}}
	row := (&Server{}).strictTCPProbe(ctx, nil, p)
	if row.Actual != "reset" || row.Pass || row.ProbeError != wantResetDetail {
		t.Fatal(row)
	}
	rows := runEUProbeMatrix(ctx, []validationProbe{p}, func(context.Context, validationProbe) validationRow {
		t.Fatal("probe executed after cancellation")
		return validationRow{}
	})
	if len(rows) != 1 || rows[0].Actual != "reset" || rows[0].Pass || rows[0].ProbeError != wantResetDetail {
		t.Fatal(rows)
	}
}

func TestCanarySameHashCommitFailsClosed(t *testing.T) {
	s := firewallWaitTestServer(t, usPackageID, "weak", true)
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		s.activeConfigMu.Lock()
		s.policyChangedAt = s.policyChangedAt.Add(time.Second)
		s.activeConfigMu.Unlock()
		return "", "", 0, nil
	}
	if err := s.waitForFirewallPolicy(t.Context(), s.rng.(*fakeRange).gen); err == nil || !strings.Contains(err.Error(), "policy changed") {
		t.Fatalf("same-hash boundary accepted: %v", err)
	}
}
