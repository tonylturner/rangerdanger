package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestChangeRequestManualPolicyJudgment(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			var running []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/config" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Write(running)
			}))
			defer upstream.Close()
			gen := usGeneration(containd.NewClient(upstream.URL))
			if pkg == euPackageID {
				gen = euGeneration(t, containd.NewClient(upstream.URL), nil)
			}
			s := rangeServer(gen, "custom")
			ids := []string{"pin-rtac-to-field"}
			if pkg == euPackageID {
				ids = []string{"pin-rtu-to-field"}
			}
			compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, ids)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{compiled})
			// Appliance-authored rule IDs/descriptions need not match compiler
			// provenance. Preserve only their L4 meaning.
			var manual map[string]any
			if err := json.Unmarshal(compiled.Config, &manual); err != nil {
				t.Fatal(err)
			}
			firewall := manual["firewall"].(map[string]any)
			rules := firewall["rules"].([]any)
			for i, rule := range rules {
				rule.(map[string]any)["id"] = fmt.Sprintf("hand-written-%d", i)
				rule.(map[string]any)["description"] = "student-authored"
			}
			encode := func(document any) []byte {
				t.Helper()
				data, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			equivalent := encode(manual)
			setRunning := func(data []byte) {
				t.Helper()
				running = data
				s.lastAppliedHash, err = containd.FirewallHashFromBytes(data)
				if err != nil {
					t.Fatal(err)
				}
				s.policySource = "manual-custom"
			}
			for _, tc := range []struct {
				name, want, detail string
				ctx                context.Context
				data               []byte
			}{
				{"partial request", "pass", "your 1.4 change request (1 actions)", ctx, equivalent},
				{"no request", "fail", "full reference (no change request saved)", t.Context(), equivalent},
			} {
				t.Run(tc.name, func(t *testing.T) {
					setRunning(tc.data)
					check := s.policyJudgment(tc.ctx, gen)
					if check.Status != tc.want || !strings.Contains(check.Detail, tc.detail) {
						t.Fatalf("%+v", check)
					}
				})
			}
			t.Run("allow shadows source pin", func(t *testing.T) {
				firewall["rules"] = append([]any{map[string]any{"id": "broad", "action": "ALLOW"}}, rules...)
				setRunning(encode(manual))
				check := s.policyJudgment(ctx, gen)
				if check.Status != "fail" || !strings.Contains(check.Detail, "policy mismatches: ") {
					t.Fatal(check)
				}
			})
			t.Run("missing required conduits", func(t *testing.T) {
				firewall["rules"], firewall["defaultAction"] = []any{}, "DENY"
				setRunning(encode(manual))
				if check := s.policyJudgment(ctx, gen); check.Status != "fail" || !strings.Contains(check.Detail, "policy mismatches: ") {
					t.Fatal(check)
				}
			})
			t.Run("unsupported selector", func(t *testing.T) {
				firewall["rules"] = []any{map[string]any{"action": "ALLOW", "schedule": "office-hours"}}
				setRunning(encode(manual))
				if check := s.policyJudgment(ctx, gen); check.Status != "fail" || !strings.Contains(check.Detail, "unsupported schedule") {
					t.Fatal(check)
				}
			})
			t.Run("weak identical with empty request", func(t *testing.T) {
				weak, err := os.ReadFile(filepath.Join(s.cfg.LabDefinitionsPath, gen.Package.Policies["weak"]))
				if err != nil {
					t.Fatal(err)
				}
				empty, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
				if err != nil {
					t.Fatal(err)
				}
				setRunning(weak)
				check := s.policyJudgment(context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{empty}), gen)
				if check.Status != "fail" || !strings.Contains(check.Detail, "weak baseline remains active") || !strings.Contains(check.Detail, "(0 actions)") {
					t.Fatal(check)
				}
			})
			t.Run("applied snapshot supersedes request", func(t *testing.T) {
				setRunning(compiled.Config)
				s.appliedPlan, s.planProvenance, s.policySource = &compiled, &planProvenance{PolicyHash: s.lastAppliedHash}, "plan"
				empty, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
				if err != nil {
					t.Fatal(err)
				}
				check := s.policyJudgment(context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{empty}), gen)
				if check.Status != "pass" || !strings.Contains(check.Detail, "applied plan") || strings.Contains(check.Detail, "your 1.4") {
					t.Fatal(check)
				}
			})
		})
	}
}

func TestChangeRequestTransport(t *testing.T) {
	var running []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config":
			w.Write(running)
		case "/api/state":
			json.NewEncoder(w).Encode(substationState(healthyOpts()))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	gen := s.rng.(*fakeRange).gen
	s.rng.(*fakeRange).active = gen.Package.ID
	for i := range gen.Manifest.Services {
		if gen.Manifest.Services[i].Node == "rtac-1" {
			gen.Manifest.Services[i].Endpoints["api"] = upstream.URL
		}
	}
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"pin-rtac-to-field"})
	if err != nil {
		t.Fatal(err)
	}
	running = compiled.Config
	s.activeConfig, s.policySource = "custom", "manual-custom"
	s.lastAppliedHash, err = containd.FirewallHashFromBytes(running)
	if err != nil {
		t.Fatal(err)
	}
	s.db, err = db.Connect(filepath.Join(t.TempDir(), "grading.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.catalog, err = labs.NewLoader(s.cfg.LabDefinitionsPath, ValidatorRequirements()).Seed(t.Context(), s.db, gen.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := s.findActiveScenario("firewall-implementation")
	if err != nil {
		t.Fatal(err)
	}
	var steps []labs.ScenarioStep
	if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
		t.Fatal(err)
	}
	stepIndex := -1
	for i, step := range steps {
		if step.Action != nil && step.Action.Expect["firewall_config"] == "improved" {
			stepIndex = i
			break
		}
	}
	if stepIndex < 0 {
		t.Fatal("missing authored policy check")
	}
	valid := planSelection{gen.Package.ID, gen.Package.Revision, []string{"pin-rtac-to-field"}}
	for _, route := range []struct {
		name, pattern, path string
		handler             gin.HandlerFunc
	}{
		{"execute", "/api/scenarios/:id/steps/:stepIdx/execute", fmt.Sprintf("/api/scenarios/%s/steps/%d/execute", scenario.ID, stepIndex), s.handleExecuteStep},
		{"validate", "/api/scenarios/:id/validate", "/api/scenarios/" + scenario.ID + "/validate", s.handleValidateScenario},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, body, detail string
				status             int
			}{
				{"valid", string(mustJSON(t, map[string]any{"changeRequest": valid})), "your 1.4 change request (1 actions)", 200},
				{"empty", string(mustJSON(t, map[string]any{"changeRequest": planSelection{gen.Package.ID, gen.Package.Revision, []string{}}})), "your 1.4 change request (0 actions)", 200},
				{"no body", "", "full reference (no change request saved)", 200},
				{"no request", "{}", "full reference (no change request saved)", 200},
				{"wrong package", string(mustJSON(t, map[string]any{"changeRequest": planSelection{"other", gen.Package.Revision, nil}})), "package or revision", 400},
				{"wrong revision", string(mustJSON(t, map[string]any{"changeRequest": planSelection{gen.Package.ID, gen.Package.Revision + 1, nil}})), "package or revision", 400},
				{"unknown action", string(mustJSON(t, map[string]any{"changeRequest": planSelection{gen.Package.ID, gen.Package.Revision, []string{"unknown"}}})), "unknown plan action", 400},
				{"untrusted verdict", `{"changeRequest":{"packageId":"` + gen.Package.ID + `","revision":` + fmt.Sprint(gen.Package.Revision) + `,"verdicts":[]}}`, "unknown field", 400},
				{"malformed", "{", "invalid grading request", 400},
				{"trailing document", "{} {}", "one JSON document", 400},
				{"oversized", `{"changeRequest":"` + strings.Repeat("a", 65<<10) + `"}`, "invalid grading request", 400},
			} {
				t.Run(tc.name, func(t *testing.T) {
					rec := callRoute(s, route.handler, "POST", route.pattern, route.path, tc.body)
					if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.detail) {
						t.Fatalf("%d %s", rec.Code, rec.Body)
					}
				})
			}
			// Precedence is evaluated after validating the request.
			s.policySource, s.appliedPlan, s.planProvenance = "plan", &compiled, &planProvenance{PolicyHash: s.lastAppliedHash}
			rec := callRoute(s, route.handler, "POST", route.pattern, route.path, string(mustJSON(t, map[string]any{"changeRequest": planSelection{gen.Package.ID, gen.Package.Revision, nil}})))
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "applied plan") || strings.Contains(rec.Body.String(), "your 1.4") {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			rec = callRoute(s, route.handler, "POST", route.pattern, route.path, `{"changeRequest":{"packageId":"wrong"}}`)
			if rec.Code != 400 {
				t.Fatalf("invalid request hidden by applied plan: %d %s", rec.Code, rec.Body)
			}
			s.policySource, s.appliedPlan, s.planProvenance = "manual-custom", nil, nil
		})
	}
	s.registerRoutes()
	if rec := serve(s, http.MethodGet, "/api/scenarios/"+scenario.ID+"/validate"); rec.Code != 404 {
		t.Fatalf("GET alias survived: %d %s", rec.Code, rec.Body)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestChangeRequestEUActiveMatrixAndAudit(t *testing.T) {
	s, gen, setRunning, _ := euPolicyCheckFixture(t)
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-field"})
	if err != nil {
		t.Fatal(err)
	}
	setRunning(compiled.Config)
	s.policySource = "manual-custom"
	s.lastAppliedHash, err = containd.FirewallHashFromBytes(compiled.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.policyChangedAt, s.policyEvidenceSince = time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)
	ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{compiled})
	slot := "eu-hardening-configurations"
	ev := s.collectEUWorkshop(ctx, gen, slot)
	matrix, err := resolveProof(gen, compiled.Expectations, slot, true)
	if err != nil {
		t.Fatal(err)
	}
	if ev.negativeErr != nil || len(ev.negative) != len(matrix) || ev.target != "your 1.4 change request (1 actions)" || !ev.foreignBreakerAllowed {
		t.Fatalf("collector did not use request: %+v", ev)
	}
	for i, row := range ev.negative {
		if !reflect.DeepEqual(row.validationProbe, matrix[i]) {
			t.Fatalf("probe %d = %+v, want %+v", i, row.validationProbe, matrix[i])
		}
	}
	// No Docker means measurements are explicitly untested, not fake passes.
	if check := euWorkshopNegative(ev); check.Status != "fail" || !strings.Contains(check.Detail, ev.target) {
		t.Fatal(check)
	}
	if check := euNoForeignBreaker(ev); check.Status != "pass" || !strings.Contains(check.Detail, ev.target) {
		t.Fatal(check)
	}
	reference, err := s.judgingExpectations(gen)
	if err != nil {
		t.Fatal(err)
	}
	different := false
	for i, expectation := range reference {
		if expectation.PlanVerdict != expectation.HardenedVerdict {
			t.Fatal("automation no longer uses reference")
		}
		different = different || expectation.PlanVerdict != compiled.Expectations[i].PlanVerdict
	}
	if !different {
		t.Fatal("fixture must distinguish request from full reference")
	}
	// Even if an internal caller carries an interactive context, the
	// automation matrix must not use it as a grading request.
	wantAutomation, err := resolveProof(gen, reference, "", true)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]validationProbe{}
	for _, probe := range wantAutomation {
		byID[probe.ID] = probe
	}
	result, err := s.measureProbeMatrix(ctx, gen, func(_ context.Context, probe validationProbe) validationRow {
		if !reflect.DeepEqual(probe, byID[probe.ID]) {
			t.Errorf("automation received request probe: %+v", probe)
		}
		return validationRow{validationProbe: probe, Actual: probe.Expected, Pass: true}
	})
	if err != nil || len(result.Rows) != len(wantAutomation) {
		t.Fatalf("automation: %+v; %v", result, err)
	}
	us := usGeneration(nil)
	empty, err := labs.CompilePlan(us.Package, us.Package.Curriculum, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries := []map[string]any{{"source": "10.10.10.50", "target": "regulator", "command": "set_tap", "result": "executed"}}
	check := s.usAuditJudgment(context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{empty}), us, entries)
	if check.Status != "pass" || !strings.Contains(check.Detail, "(0 actions)") {
		t.Fatal(check)
	}
	if check := s.usAuditJudgment(t.Context(), us, entries); check.Status != "warn" || !strings.Contains(check.Detail, "full reference") {
		t.Fatal(check)
	}
}
