package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestPartialPlanProofVersusAcceptance(t *testing.T) {
	gen := usGeneration(nil)
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-field"})
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := resolveProof(gen, compiled.Expectations, "", true)
	if err != nil {
		t.Fatal(err)
	}
	rows := []validationRow{}
	for _, probe := range matrix {
		rows = append(rows, validationRow{validationProbe: probe, Actual: probe.PlanExpected, Pass: true})
	}
	proof, acceptance := judgeValidation(rows, true, "hash", true, true)
	if !proof.Pass || acceptance.Pass || len(acceptance.PreventionGaps) == 0 || len(proof.Rows) != 21 {
		t.Fatalf("proof %+v acceptance %+v", proof, acceptance)
	}
	proof, acceptance = judgeValidation(rows, false, "hash", true, true)
	if proof.Applicable || proof.Pass || len(proof.Rows) != 0 || acceptance.Pass {
		t.Fatal("manual policy received plan proof")
	}
	proof, _ = judgeValidation(rows, true, "hash", false, true)
	if proof.Pass {
		t.Fatal("unstable plan passed")
	}
	rows[0].Actual = "skipped"
	proof, acceptance = judgeValidation(rows, true, "hash", true, true)
	if proof.Pass || acceptance.Pass || proof.Skipped != 1 || acceptance.Skipped != 1 {
		t.Fatal("untested row passed")
	}
}

func TestUSCommandAndPolicyJudgmentByPlan(t *testing.T) {
	gen := usGeneration(nil)
	var data []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		w.Write(data)
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	gen = s.rng.(*fakeRange).gen
	for _, ids := range [][]string{nil, {"block-enterprise-to-field"}, {"block-vendor-to-field"}, {"pin-rtac-to-field"}} {
		compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, ids)
		if err != nil {
			t.Fatal(err)
		}
		data = compiled.Config
		hash, _ := containd.FirewallHashFromBytes(data)
		s.activeConfig, s.policySource, s.lastAppliedHash = "custom", "plan", hash
		s.planProvenance = &planProvenance{PolicyHash: hash}
		s.appliedPlan = &compiled
		for _, tc := range []struct{ source, action string }{{"10.10.10.50", "block-enterprise-to-field"}, {"10.20.20.10", "block-vendor-to-field"}, {"10.30.30.40", "pin-rtac-to-field"}, {"operator", ""}, {"rtac-sim", ""}} {
			want := labs.VerdictAllow
			for _, id := range ids {
				if tc.action == id {
					want = labs.VerdictDeny
				}
			}
			verdict, err := s.commandVerdict(context.Background(), gen, "recloser", "open", tc.source)
			if err != nil || verdict != want {
				t.Fatalf("%v %s: %s want %s, %v", ids, tc.source, verdict, want, err)
			}
			if want == labs.VerdictDeny {
				result := s.executeRTACCommand(context.Background(), gen, usRecipe, "recloser", "open", tc.source, nil)
				if !result.Blocked || result.Success {
					t.Fatalf("denied command forwarded: %+v", result)
				}
			}
		}
		if check := s.usPolicyJudgment(context.Background(), gen); check.Status != "pass" {
			t.Fatal(check)
		}
	}
	if _, err := s.commandVerdict(context.Background(), gen, "recloser", "open", "not-a-node"); err == nil {
		t.Fatal("unknown source accepted")
	}
	data, _ = os.ReadFile(filepath.Join(s.cfg.LabDefinitionsPath, gen.Package.Policies["weak"]))
	s.lastAppliedHash, _ = containd.FirewallHashFromBytes(data)
	s.policySource = "weak"
	if check := s.usPolicyJudgment(context.Background(), gen); check.Status != "fail" {
		t.Fatal(check)
	}
	data, _ = os.ReadFile(filepath.Join(s.cfg.LabDefinitionsPath, gen.Package.Policies["improved"]))
	s.lastAppliedHash, _ = containd.FirewallHashFromBytes(data)
	s.policySource = "manual-custom"
	if check := s.usPolicyJudgment(context.Background(), gen); check.Status != "pass" || !strings.Contains(check.Detail, "not an applied plan") {
		t.Fatal(check)
	}
}

func TestEUPartialPlanValidators(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	for _, ids := range [][]string{nil, {"block-enterprise-to-field"}, {"block-enterprise-to-station"}} {
		compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, ids)
		if err != nil {
			t.Fatal(err)
		}
		for _, slot := range euWorkshopSlots[1:] {
			ev := passingEUWorkshop(t, slot)
			ev.isPlan = true
			ev.foreignBreakerAllowed = true
			for _, e := range compiled.Expectations {
				if e.From == "kali-1" && e.To == "rtu-1" && e.Port == 2404 {
					ev.foreignBreakerAllowed = e.PlanVerdict == labs.VerdictAllow
				}
			}
			matrix, err := resolveProof(gen, compiled.Expectations, slot, true)
			if err != nil {
				t.Fatal(err)
			}
			ev.negative, ev.negativeExpected = nil, len(matrix)
			for _, probe := range matrix {
				ev.negative = append(ev.negative, validationRow{validationProbe: probe, Actual: probe.Expected, Pass: true})
			}
			ev.report.PlanProof = proofResult{Applicable: true, Pass: true, HashStable: true, PolicyHash: ev.policyHash}
			ev.report.Acceptance.Pass = false // independent Hardened acceptance can fail
			assertEUChecks(t, slot, ev, true)
			if len(ev.negative) > 0 {
				ev.negative[0].Actual = "skipped"
				ev.negative[0].Pass = false
				assertEUChecks(t, slot, ev, false)
			}
		}
	}
}

func TestPlanConduitDeniesFollowSelectedRules(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	rows, err := conduitDenyMatrix(gen, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ids   []string
		fewer bool
	}{{nil, true}, {[]string{"block-enterprise-to-field"}, true}} {
		compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, tc.ids)
		if err != nil {
			t.Fatal(err)
		}
		filtered, err := planDenyFilter(rows, &compiled, gen.Package)
		if err != nil || len(filtered) >= len(rows) {
			t.Fatalf("not filtered: %d/%d %v", len(filtered), len(rows), err)
		}
		for _, row := range filtered {
			if row.source == "10.10.10.50" && row.port == 2404 {
				t.Fatal("deferred station closure still requires a DENY event")
			}
		}
	}
}

func TestEUPlanHandlers(t *testing.T) {
	var running []byte
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			running, _ = io.ReadAll(r.Body)
		case "/api/v1/config/commit":
		case "/api/v1/config":
			w.Write(running)
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	s := rangeServer(euGeneration(t, containd.NewClient(fake.URL), nil), "weak")
	rec, _ := invoke(s, s.handleFirewallApplyPlan, "POST", "/api/firewall/apply-plan", planRequest(s, "block-enterprise-to-field"))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var result struct {
		Plan planResponse `json:"plan"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan.Label != "partial" || len(result.Plan.Expectations) != 37 {
		t.Fatalf("unexpected EU response %+v", result.Plan)
	}
	s.lastAppliedAt = time.Now().Add(-time.Minute)
	s.classifyObservedPolicy(result.Plan.PolicyHash, time.Now())
	if s.policySource != "plan" {
		t.Fatal("matching EU plan reclassified")
	}
}

func TestApplyPlanRejectsInvalidProgramWithoutRecording(t *testing.T) {
	fake, counters := fakeContaind(t, 200, 200)
	s := newTestServer(t, fake.URL)
	gen := s.rng.(*fakeRange).gen
	gen.Package.Curriculum.Plan.Rules[0].Selected[0].Action = "invalid"
	rec, body := invoke(s, s.handleFirewallApplyPlan, "POST", "/api/firewall/apply-plan", planRequest(s))
	if rec.Code != 422 || body["warnings"] == nil || s.planProvenance != nil || counters.candidateCount != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestUSExecutorAllowsDeferredExposure(t *testing.T) {
	gen := usGeneration(nil)
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/config" {
			w.Write(compiled.Config)
			return
		}
		if r.URL.Path == "/api/command/recloser" {
			calls++
			io.WriteString(w, `{"result":"executed","detail":"fixture command executed"}`)
			return
		}
		t.Errorf("unexpected %s", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	gen = s.rng.(*fakeRange).gen
	for i := range gen.Manifest.Services {
		if gen.Manifest.Services[i].Node == "rtac-1" {
			for key := range gen.Manifest.Services[i].Endpoints {
				gen.Manifest.Services[i].Endpoints[key] = upstream.URL
			}
		}
	}
	result := s.executeRTACCommand(t.Context(), gen, usRecipe, "recloser", "open", "10.10.10.50", nil)
	if !result.Success || result.Blocked || calls != 1 {
		t.Fatalf("allow result %+v calls %d", result, calls)
	}
}

func TestRunningNewConnectionDoesNotMatchEstablishedReturn(t *testing.T) {
	policy, err := readEvaluatedFirewall([]byte(`{"firewall":{"defaultAction":"DENY","rules":[{"id":"return","state":["ESTABLISHED","RELATED"],"action":"ALLOW"}]}}`))
	if err != nil || labs.Evaluate(policy.Rules, policy.DefaultAction, labs.PlanTuple{}) != labs.VerdictDeny {
		t.Fatalf("%+v %v", policy, err)
	}
}

func TestUSRegisteredValidatorsJudgeRulesNotLabels(t *testing.T) {
	var data []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	gen := s.rng.(*fakeRange).gen
	gen.StartedAt = time.Now().Add(-time.Hour).UTC()
	for _, tc := range []struct {
		name, source string
		ids          []string
		want         bool
	}{
		{"weak", "weak", nil, false},
		{"manual reference", "manual-custom", nil, true},
		{"partial plan", "plan", []string{"block-enterprise-to-field"}, true},
		{"weak-label plan", "plan", nil, true},
	} {
		if tc.source == "plan" {
			compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, tc.ids)
			if err != nil {
				t.Fatal(err)
			}
			data = compiled.Config
			s.appliedPlan = &compiled
		} else {
			name := "improved"
			if tc.source == "weak" {
				name = "weak"
			}
			var err error
			data, err = os.ReadFile(filepath.Join(s.cfg.LabDefinitionsPath, gen.Package.Policies[name]))
			if err != nil {
				t.Fatal(err)
			}
			s.appliedPlan = nil
		}
		hash, _ := containd.FirewallHashFromBytes(data)
		s.policySource, s.lastAppliedHash, s.activeConfig = tc.source, hash, "custom"
		s.policyChangedAt = time.Now().Add(-time.Minute).UTC()
		s.planProvenance = &planProvenance{PolicyHash: hash}
		s.lastValidationReport = &retainedValidationReport{Hash: hash, Markdown: "fixture", RangeStart: gen.StartedAt, PolicyAt: s.policyChangedAt, StartedAt: s.policyChangedAt.Add(time.Second), FinishedAt: s.policyChangedAt.Add(2 * time.Second), Acceptance: acceptanceResult{Pass: tc.source != "plan"}, PlanProof: proofResult{Applicable: tc.source == "plan", Pass: true, HashStable: true, PolicyHash: hash}}
		for _, slot := range []string{"us-firewall-implementation", "us-hardening-configurations", "us-vendor-rdp-compromise", "us-validation-evidence"} {
			checks := scenarioValidators[slot].run(t.Context(), s, validatorInput{state: substationState(healthyOpts()), gen: gen, activeConfig: "custom"})
			target := findCheckMatching(t, checks, "Running policy matches")
			if (target.Status == "pass") != tc.want {
				t.Fatalf("%s/%s: %+v", tc.name, slot, checks)
			}
			if slot == "us-validation-evidence" && tc.want {
				retained := findCheckMatching(t, checks, "Retained PASS")
				if retained.Status != "pass" {
					t.Fatalf("%s: %+v", tc.name, retained)
				}
			}
		}
	}
}

func TestUSAuditAgainstPlanRatherThanForeignSourceLabel(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")
	gen := s.rng.(*fakeRange).gen
	entries := []map[string]any{{"source": "10.10.10.50", "target": "regulator", "command": "set_tap", "result": "executed"}}
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.appliedPlan, s.policySource = &compiled, "plan"
	if check := s.usAuditJudgment(gen, entries); check.Status != "pass" || !strings.Contains(check.Detail, "deferred exposures") {
		t.Fatal(check)
	}
	compiled, err = labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-field"})
	if err != nil {
		t.Fatal(err)
	}
	s.appliedPlan = &compiled
	if check := s.usAuditJudgment(gen, entries); check.Status != "warn" {
		t.Fatal(check)
	}
	s.policySource = "manual-custom"
	if check := s.usAuditJudgment(gen, entries); check.Status != "warn" {
		t.Fatal(check)
	}
}
