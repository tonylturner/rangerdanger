package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func compileProbePlan(t testing.TB, gen *lifecycle.Generation, ids []string) labs.CompiledPlan {
	t.Helper()
	plan, err := labs.CompilePlan(gen.Package, gen.Package.Course.Features.SegmentationPlan.Model, ids)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func useAppliedProbePlan(t testing.TB, s *Server, plan labs.CompiledPlan) {
	t.Helper()
	hash, err := containd.FirewallHashFromBytes(plan.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.policySource, s.appliedPlan = "plan", &plan
	gen := s.rng.(*fakeRange).gen
	s.planProvenance = &planProvenance{PolicyHash: hash, PackageID: gen.Package.ID, Revision: gen.Package.Revision}
	s.lastAppliedHash = hash
}

func TestProbeSuccessFollowsJudgingTarget(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			if pkg == euPackageID {
				gen = euGeneration(t, nil, nil)
			}
			var all []string
			for _, action := range gen.Package.SegmentationPlan().Catalog {
				all = append(all, action.ID)
			}
			allow := compileProbePlan(t, gen, nil)
			deny := compileProbePlan(t, gen, all)
			step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
			for _, tc := range []struct {
				name, target, verdict string
				request, applied      *labs.CompiledPlan
				rc                    int
				stderr                string
				pass                  bool
			}{
				{name: "Weak exposure fails reference", target: "full reference (no change request saved)", verdict: "DENY", rc: 0},
				{name: "reference timeout passes", target: "full reference (no change request saved)", verdict: "DENY", rc: 124, pass: true},
				{name: "refusal fails deny", target: "full reference (no change request saved)", verdict: "DENY", rc: 1, stderr: "Connection refused"},
				{name: "saved empty request allows deferred path", request: &allow, target: "your 1.4 change request (0 actions)", verdict: "ALLOW", pass: true},
				{name: "saved full request denies exposure", request: &deny, target: fmt.Sprintf("your 1.4 change request (%d actions)", len(all)), verdict: "DENY"},
				{name: "applied empty plan supersedes full request", request: &deny, applied: &allow, target: "applied plan", verdict: "ALLOW", pass: true},
				{name: "applied full plan supersedes empty request", request: &allow, applied: &deny, target: "applied plan", verdict: "DENY"},
				{name: "applied full plan timeout passes", applied: &deny, target: "applied plan", verdict: "DENY", rc: 124, pass: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := rangeServer(gen, "weak")
					s.policySource = "manual-custom"
					ctx := t.Context()
					if tc.request != nil {
						ctx = context.WithValue(ctx, changeRequestKey{}, compiledChangeRequest{*tc.request})
					}
					if tc.applied != nil {
						useAppliedProbePlan(t, s, *tc.applied)
					}
					s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
						return "", tc.stderr, tc.rc, nil
					}
					gen = installProbePolicy(t, s, gen)
					rows := s.executeProbe(ctx, gen, step)
					observed := "reachable"
					if tc.rc == 124 {
						observed = "blocked"
					}
					want := fmt.Sprintf("expected %s by %s, observed %s", tc.verdict, tc.target, observed)
					if len(rows) != 1 || rows[0].Success != tc.pass || !strings.HasPrefix(rows[0].Detail, want) {
						t.Fatalf("rows=%+v; want success=%t and %q", rows, tc.pass, want)
					}
				})
			}
		})
	}
}

func TestProbeBridgedRouteRemainsReachable(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			gen := usGeneration(nil)
			source := "relay-1"
			if pkg == euPackageID {
				gen, source = euGeneration(t, nil, nil), "rmu-1"
			}
			s := rangeServer(gen, "weak")
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) { return "", "", 0, nil }
			step := labs.ScenarioStep{Node: source, Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.20", Port: 502}}}}
			gen = installProbePolicy(t, s, gen)
			rows := s.executeProbe(t.Context(), gen, step)
			if len(rows) != 1 || !rows[0].Success || !strings.Contains(rows[0].Detail, "expected ALLOW by full reference") {
				t.Fatal(rows)
			}
		})
	}
}

func TestProbeUnresolvableTargetFailsWithoutExecution(t *testing.T) {
	s := activePackageServer(nil)
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		t.Fatal("probe executed for a target outside the topology")
		return "", "", 0, nil
	}
	step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{{Host: "10.40.40.99", Port: 502}}}}
	gen := installProbePolicy(t, s, s.rng.(*fakeRange).gen)
	rows := s.executeProbe(t.Context(), gen, step)
	if len(rows) != 1 || rows[0].Success || !strings.Contains(rows[0].Detail, "cannot resolve policy tuple") || !strings.Contains(rows[0].Detail, "full reference") {
		t.Fatal(rows)
	}
}

func TestProbeUnavailableJudgingTargetFailsEveryRow(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			gen := usGeneration(nil)
			s := rangeServer(gen, "weak")
			if malformed {
				plan := compileProbePlan(t, gen, nil)
				useAppliedProbePlan(t, s, plan)
				s.appliedPlan.Config = []byte(`{}`)
			} else {
				gen.Package.Course.Features.SegmentationPlan = nil
			}
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				t.Error("probe ran without a judging target")
				return "", "", 0, nil
			}
			step := labs.ScenarioStep{Node: "kali-1", Action: &labs.StepAction{Type: "probe", Targets: []labs.ProbeTarget{
				{Host: "10.40.40.20", Port: 502, Note: "first"}, {Host: "10.40.40.21", Port: 502, Note: "second"},
			}}}
			rows := s.executeProbe(t.Context(), gen, step)
			if len(rows) != 2 {
				t.Fatal(rows)
			}
			for i, row := range rows {
				if row.Success || !strings.Contains(row.Detail, "judging target") || !strings.HasPrefix(row.Detail, step.Action.Targets[i].Note+": ") {
					t.Fatal(row)
				}
			}
		})
	}
}

// Supply independent running authority for tests that exercise judging targets.
func installProbePolicy(t testing.TB, s *Server, gen *lifecycle.Generation) *lifecycle.Generation {
	t.Helper()
	data := []byte(`{"firewall":{"defaultAction":"ALLOW","rules":[]}}`)
	if s.policySource == "plan" && s.appliedPlan != nil {
		data = s.appliedPlan.Config
	}
	return installRunningProbePolicy(t, s, gen, data)
}

func installRunningProbePolicy(t testing.TB, s *Server, gen *lifecycle.Generation, data []byte) *lifecycle.Generation {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/config" {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
		w.Write(data)
	}))
	t.Cleanup(upstream.Close)
	var err error
	s.lastAppliedHash, err = containd.FirewallHashFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	next := lifecycle.NewGeneration(gen.ID, gen.Package, gen.Manifest, containd.NewClient(upstream.URL), gen.StartedAt)
	s.rng.(*fakeRange).gen = next
	return next
}
