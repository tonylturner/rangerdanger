package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

const (
	testPackageID  = "test-package"
	testTemplateID = "test-workshop"
)

// activePackageServer returns a server whose active package is
// testPackageID, owning the template testTemplateID.
func activePackageServer(database *gorm.DB) *Server {
	return &Server{
		db:  database,
		cfg: &config.Config{Package: testPackageID},
		rng: servingRange(nil),
		catalog: &labs.Catalog{Packages: []labs.Package{
			{
				ID: "other-package", Title: "Other", Revision: 2, Template: labs.LabYAML{ID: "other-workshop"}, OperatorView: labs.OperatorViewEUIEC104,
				FirewallConfigPath: "other/open.json",
				Policies:           map[string]string{"strict": "other/strict.json", "open": "other/open.json", "audit": "other/audit.json"},
			},
			{
				ID: testPackageID, Title: "Test", Revision: 1, Template: labs.LabYAML{ID: testTemplateID}, OperatorView: labs.OperatorViewUSDNP3,
				FirewallConfigPath: "test/weak.json",
				Policies:           map[string]string{"weak": "test/weak.json", "improved": "test/improved.json"},
			},
		}},
	}
}

func routedServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Connect(filepath.Join(t.TempDir(), "packages.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := activePackageServer(database)
	s.engine = gin.New()
	s.registerRoutes()
	return s
}

func serve(s *Server, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"lab_instance_id":"lab-1"}`)))
	return rec
}

func TestListPackagesMarksActive(t *testing.T) {
	s := routedServer(t)
	rec := serve(s, http.MethodGet, "/api/packages")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/packages = %d %s", rec.Code, rec.Body)
	}
	var got []packageSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Policies list the default first, then the rest by name.
	want := []packageSummary{
		{ID: "other-package", Title: "Other", Revision: 2, OperatorView: "eu-iec104", Policies: []string{"open", "audit", "strict"}},
		{ID: testPackageID, Title: "Test", Revision: 1, Active: true, OperatorView: "us-dnp3-substation", Policies: []string{"weak", "improved"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %#v, want %#v", got, want)
	}
	for _, field := range []string{`"operator_view":"eu-iec104"`, `"policies":["weak","improved"]`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("GET /api/packages = %s, want %s", rec.Body, field)
		}
	}
}

func TestListPackagesWithoutCatalogIsEmpty(t *testing.T) {
	s := routedServer(t)
	s.setCatalog(nil)
	rec := serve(s, http.MethodGet, "/api/packages")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("GET /api/packages without a catalog = %d %s, want 200 []", rec.Code, rec.Body)
	}
}

func TestScenarioRoutesServeActivePackageOnly(t *testing.T) {
	s := routedServer(t)
	for _, row := range []models.Scenario{
		{ID: "active-lab", PackageID: testPackageID, LabTemplateID: testTemplateID, Order: "1", Steps: `[{"id":"only","title":"Only"}]`},
		{ID: "active-unvalidated", PackageID: testPackageID, LabTemplateID: testTemplateID, Order: "2", Steps: `[]`},
		{ID: "other-lab", PackageID: "other-package", LabTemplateID: "other-workshop", Order: "0", Validator: "us-remediation-planning", Steps: `[{"id":"only","title":"Only"}]`},
	} {
		if err := s.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	list := func(path string) []string {
		t.Helper()
		rec := serve(s, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
		var body struct {
			Scenarios []models.Scenario `json:"scenarios"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, scenario := range body.Scenarios {
			ids = append(ids, scenario.ID)
		}
		return ids
	}
	if got, want := list("/api/scenarios"), []string{"active-lab", "active-unvalidated"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unfiltered list = %v, want %v", got, want)
	}
	if got, want := list("/api/scenarios?lab_template_id="+testTemplateID), []string{"active-lab", "active-unvalidated"}; !reflect.DeepEqual(got, want) {
		t.Errorf("active template list = %v, want %v", got, want)
	}
	if got := list("/api/scenarios?lab_template_id=other-workshop"); len(got) != 0 {
		t.Errorf("inactive template list = %v, want empty", got)
	}

	if rec := serve(s, http.MethodGet, "/api/scenarios/active-lab"); rec.Code != http.StatusOK {
		t.Errorf("GET active scenario = %d, want 200", rec.Code)
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/scenarios/other-lab"},
		{http.MethodGet, "/api/scenarios/other-lab/validate"},
		{http.MethodPost, "/api/scenarios/other-lab/steps/0/execute"},
		{http.MethodGet, "/api/scenarios/missing/validate"},
		{http.MethodGet, "/api/scenarios/active-unvalidated/validate"},
		// The write routes are gone.
		{http.MethodPost, "/api/scenarios"},
		{http.MethodPost, "/api/labs/templates"},
		// So are lab instances, templates and scenario runs.
		{http.MethodGet, "/api/labs/templates"},
		{http.MethodGet, "/api/labs/instances"},
		{http.MethodPost, "/api/labs/instances"},
		{http.MethodGet, "/api/labs/instances/lab/nodes/kali-1/terminal"},
		{http.MethodPost, "/api/scenarios/active-lab/run"},
		{http.MethodGet, "/api/scenario-runs/run"},
		{http.MethodPost, "/api/nodes/kali-1/action"},
	} {
		if rec := serve(s, request.method, request.path); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d %s, want 404", request.method, request.path, rec.Code, rec.Body)
		}
	}
}

func TestSeedFailureIsReturnedAndKeepsCatalog(t *testing.T) {
	s := routedServer(t)
	definitions := t.TempDir()
	manifest := filepath.Join(definitions, "packages", "broken", "package.yml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("schema: 1\nid: broken\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.loader = labs.NewLoader(definitions, ValidatorRequirements())
	before := s.activePackage()

	rec := serve(s, http.MethodPost, "/api/admin/seed")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "field unknown not found") {
		t.Fatalf("POST /api/admin/seed = %d %s, want 500 with the load error", rec.Code, rec.Body)
	}
	if after := s.activePackage(); !reflect.DeepEqual(after, before) {
		t.Errorf("failed seed changed the active package: %#v -> %#v", before, after)
	}
}

func TestWorkshopGraphReadsTheServingPackage(t *testing.T) {
	s := routedServer(t)
	// The catalog's copy of the package has no nodes: the graph describes
	// the range that runs, from the package its generation started with.
	s.rng.(*fakeRange).gen.Package.Template = labs.LabYAML{ID: testTemplateID, Nodes: []labs.NodeYAML{{ID: "kali-1"}}}
	rec := serve(s, http.MethodGet, "/api/workshop/graph")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kali-1"`) {
		t.Errorf("GET /api/workshop/graph = %d %s, want the serving topology", rec.Code, rec.Body)
	}
}

// TestShippedPackagesLoad boots the loader against the repository's real
// lab-definitions/ with the real validator registry.
func TestShippedPackagesLoad(t *testing.T) {
	definitions := filepath.Clean(filepath.Join("..", "..", "..", "lab-definitions"))
	database, err := db.Connect(filepath.Join(t.TempDir(), "shipped.db"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := labs.NewLoader(definitions, ValidatorRequirements()).Seed(context.Background(), database, config.DefaultPackage)
	if err != nil {
		t.Fatalf("Seed(shipped packages): %v", err)
	}

	info, ok := catalog.Info(config.DefaultPackage)
	wantInfo := labs.PackageInfo{
		ID: "us-dnp3-substation", Title: "US distribution substation (DNP3)", Revision: 2,
		Capabilities: []string{"process.electrical", "policy.containd", "audit.device-control", "capture.firewall"},
		OperatorView: "us-dnp3-substation",
	}
	if !ok || !reflect.DeepEqual(info, wantInfo) {
		t.Errorf("default package = %#v, %v; want %#v", info, ok, wantInfo)
	}
	euListed := false
	for _, pkg := range catalog.Packages {
		if pkg.ID == euPackageID {
			euListed = true
			if pkg.Revision != 5 || pkg.Curriculum == nil || len(pkg.Curriculum.Roles) != 7 {
				t.Fatal("EU revision / curriculum binding missing")
			}
			for role, id := range pkg.Curriculum.Roles {
				found := false
				for _, scenario := range pkg.Scenarios {
					found = found || scenario.ID == id
				}
				if !found || !strings.HasPrefix(id, "eu-") {
					t.Errorf("EU role %s has nonlocal scenario %s", role, id)
				}
			}
			if names := policyNames(pkg); !reflect.DeepEqual(names, []string{"weak", "improved"}) {
				t.Errorf("EU listed policies = %v, want [weak improved]", names)
			}
		}
		if pkg.ID != config.DefaultPackage {
			continue
		}
		if pkg.FirewallConfigPath != "firewall/substation-weak.json" {
			t.Errorf("US firewall config path = %q, want firewall/substation-weak.json", pkg.FirewallConfigPath)
		}
		wantPolicies := map[string]string{"weak": "firewall/substation-weak.json", "improved": "firewall/substation-improved.json"}
		if !reflect.DeepEqual(pkg.Policies, wantPolicies) || pkg.ICSRules != labs.ICSRulesAllowed {
			t.Errorf("US policies = %v, ics_rules = %q; want %v, allowed", pkg.Policies, pkg.ICSRules, wantPolicies)
		}
		if name, ok := pkg.PolicyName(pkg.FirewallConfigPath); !ok || name != "weak" {
			t.Errorf("US default policy name = %q, %v; want weak", name, ok)
		}
		if names := policyNames(pkg); !reflect.DeepEqual(names, []string{"weak", "improved"}) {
			t.Errorf("US listed policies = %v, want [weak improved]", names)
		}
		if pkg.Template.ID != "substation-segmentation" {
			t.Errorf("US topology id = %q, want substation-segmentation", pkg.Template.ID)
		}
	}
	if !euListed {
		t.Errorf("catalog has no %s package", euPackageID)
	}

	want := map[string]struct {
		order     string
		validator string
		steps     []string
	}{
		"baseline-assessment": {"1.2", "us-baseline-assessment", []string{
			"walk-the-topology-and-inspect-the-crossings", "generate-scenario-traffic", "capture-traffic-at-the-firewall",
			"analyze-cross-zone-flows", "critical-conduits-what-must-flow", "findings-what-you-observed",
			"probe-for-latent-exposure", "transition-decide-what-to-fix-first",
		}},
		"segmentation-requirements": {"1.3", "us-segmentation-requirements", []string{
			"translate-findings-into-design-requirements", "resourcing-reality-check", "define-success-criteria-for-segmentation",
		}},
		"remediation-planning": {"1.4", "us-remediation-planning", []string{
			"review-the-findings-you-captured", "understand-the-team-budget", "choose-your-remediation-plan", "reflect-what-are-you-accepting",
		}},
		"firewall-implementation": {"2.2", "us-firewall-implementation", []string{
			"exercise-overview", "verify-access-to-containd", "phase-1-verify-current-access-under-the-weak-baseline",
			"phase-2-remove-broad-trust-and-establish-default-deny", "phase-3-create-minimal-required-rules",
			"phase-4-enable-logging-and-visibility", "phase-5-validate-allowed-traffic", "phase-6-validate-blocked-traffic",
			"export-your-policy",
		}},
		"hardening-configurations": {"2.3", "us-hardening-configurations", []string{
			"verify-normal-operations", "attack-dnp3-direct-operate-injection-against-the-recloser", "restore-the-substation",
			"apply-the-hardened-policy", "re-test-the-attack-under-the-hardened-policy", "validate",
		}},
		"vendor-rdp-compromise": {"2.3-bonus", "us-vendor-rdp-compromise", []string{
			"confirm-the-lateral-path-is-open", "pivot-via-rdp-or-vnc", "use-the-vendor-foothold-to-attack-field-devices",
			"restore-service", "apply-hardened-policy", "re-attempt-the-pivot", "validate",
		}},
		"validation-evidence": {"2.4", "us-validation-evidence", []string{
			"confirm-hardened-config-is-active", "generate-the-validation-report", "reflection-what-did-your-plan-close",
		}},
	}

	var scenarios []models.Scenario
	if err := database.Where("package_id = ?", "us-dnp3-substation").Find(&scenarios).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != len(want) {
		t.Fatalf("US scenarios = %d, want %d", len(scenarios), len(want))
	}
	used := map[string]bool{}
	for _, scenario := range scenarios {
		used[scenario.Validator] = true
		expected, ok := want[scenario.ID]
		if !ok {
			t.Errorf("unexpected US scenario %q", scenario.ID)
			continue
		}
		if scenario.Order != expected.order || scenario.Validator != expected.validator || scenario.LabTemplateID != "substation-segmentation" {
			t.Errorf("%s = (order %q, validator %q, template %q), want (%q, %q, substation-segmentation)",
				scenario.ID, scenario.Order, scenario.Validator, scenario.LabTemplateID, expected.order, expected.validator)
		}
		var steps []labs.ScenarioStep
		if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
			t.Fatalf("%s steps: %v", scenario.ID, err)
		}
		ids := make([]string, len(steps))
		for index, step := range steps {
			ids[index] = step.ID
		}
		if !reflect.DeepEqual(ids, expected.steps) {
			t.Errorf("%s step ids = %v, want %v", scenario.ID, ids, expected.steps)
		}
	}
	var euScenarios []models.Scenario
	if err := database.Where("package_id = ?", euPackageID).Find(&euScenarios).Error; err != nil {
		t.Fatal(err)
	}
	wantEU := map[string]struct {
		order     string
		validator string
		steps     []string
	}{
		"eu-baseline-assessment":       {"1.2", "eu-baseline-assessment", []string{"walk-the-topology-and-inspect-the-crossings", "generate-scenario-traffic", "capture-traffic-at-the-firewall", "analyze-cross-zone-flows", "critical-conduits-what-must-flow", "findings-what-you-observed", "probe-for-latent-exposure", "transition-decide-what-to-fix-first"}},
		"eu-firewall-implementation":   {"2.2", "eu-firewall-implementation", []string{"exercise-overview", "verify-access-to-containd", "phase-1-verify-current-access-under-the-weak-baseline", "phase-2-remove-broad-trust-and-establish-default-deny", "implement-rules", "phase-4-enable-logging-and-visibility", "test-positive", "test-negative", "export-your-policy"}},
		"eu-hardening-configurations":  {"2.3", "eu-hardening-configurations", []string{"verify-normal-operations", "attack-iec104-breaker-open", "restore-the-substation", "apply-the-hardened-policy", "re-test-the-attack-under-the-hardened-policy", "validate"}},
		"eu-remediation-planning":      {"1.4", "", []string{"review-the-findings-you-captured", "understand-the-team-budget", "choose-your-remediation-plan", "reflect-what-are-you-accepting"}},
		"eu-segmentation-requirements": {"1.3", "", []string{"translate-findings-into-design-requirements", "resourcing-reality-check", "define-success-criteria-for-segmentation"}},
		"eu-validation-evidence":       {"2.4", "eu-validation-evidence", []string{"confirm-hardened-config-is-active", "generate-the-validation-report", "reflection-what-did-your-plan-close"}},
		"eu-vendor-rdp-compromise":     {"2.3-bonus", "eu-vendor-rdp-compromise", []string{"confirm-the-lateral-path-is-open", "pivot-via-rdp-or-vnc", "use-the-vendor-foothold-to-attack-field-devices", "restore-service", "apply-hardened-policy", "re-attempt-the-pivot", "validate"}},
	}
	if len(euScenarios) != len(wantEU) {
		t.Fatalf("EU scenarios = %d, want %d", len(euScenarios), len(wantEU))
	}
	for _, scenario := range euScenarios {
		expected, ok := wantEU[scenario.ID]
		if !ok {
			t.Errorf("unexpected EU scenario %q", scenario.ID)
			continue
		}
		used[scenario.Validator] = true
		if scenario.Order != expected.order || scenario.Validator != expected.validator {
			t.Errorf("%s = (order %q, validator %q), want (%q, %q)", scenario.ID, scenario.Order, scenario.Validator, expected.order, expected.validator)
		}
		var steps []labs.ScenarioStep
		if err := json.Unmarshal([]byte(scenario.Steps), &steps); err != nil {
			t.Fatalf("%s steps: %v", scenario.ID, err)
		}
		ids := make([]string, len(steps))
		for index, step := range steps {
			ids[index] = step.ID
		}
		if !reflect.DeepEqual(ids, expected.steps) {
			t.Errorf("%s step ids = %v, want %v", scenario.ID, ids, expected.steps)
		}
	}

	for key := range scenarioValidators {
		if !used[key] {
			t.Errorf("registered validator %q is not declared by any shipped scenario", key)
		}
	}
}

func TestPackageCurriculumDTO(t *testing.T) {
	s := routedServer(t)
	catalog := s.currentCatalog()
	catalog.Packages[1].Curriculum = &labs.Curriculum{
		Schema: 1, ICSRules: labs.ICSRulesForbidden,
		CurriculumBinding: labs.CurriculumBinding{Roles: map[string]string{"planning": "package-planner"}, DynamicSteps: map[string]string{"stable-step": "rules"}},
	}
	rec := serve(s, http.MethodGet, "/api/packages")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET packages: %d %s", rec.Code, rec.Body)
	}
	var got []struct {
		ID         string          `json:"id"`
		Curriculum json.RawMessage `json:"curriculum"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Curriculum) != 0 {
		t.Fatalf("unbound package gained curriculum: %s", rec.Body)
	}
	for _, want := range []string{`"planning":"package-planner"`, `"stable-step":"rules"`, `"icsRules":"forbidden"`} {
		if !strings.Contains(string(got[1].Curriculum), want) {
			t.Fatalf("curriculum missing %s: %s", want, got[1].Curriculum)
		}
	}
}

// Every US break point has exactly one EU counterpart, in the same order.
func TestEUWorkshopStepParity(t *testing.T) {
	catalog, err := labs.NewLoader(filepath.Join(repoRoot(), "lab-definitions"), ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]labs.ScenarioYAML{}
	for _, pkg := range catalog.Packages {
		for _, sc := range pkg.Scenarios {
			all[sc.ID] = sc
		}
	}
	for _, id := range []string{"baseline-assessment", "segmentation-requirements", "remediation-planning", "firewall-implementation", "hardening-configurations", "vendor-rdp-compromise", "validation-evidence"} {
		t.Run(id, func(t *testing.T) {
			us, eu := all[id], all["eu-"+id]
			if eu.Order != us.Order || len(eu.Steps) != len(us.Steps) {
				t.Fatalf("break points differ: %s/%d vs %s/%d", us.Order, len(us.Steps), eu.Order, len(eu.Steps))
			}
			usDecisions, euDecisions := 0, 0
			for _, step := range us.Steps {
				usDecisions += strings.Count(step.Description, ":::decision ")
			}
			for _, step := range eu.Steps {
				euDecisions += strings.Count(step.Description, ":::decision ")
			}
			if usDecisions != euDecisions {
				t.Errorf("decision activity count changed: US %d EU %d", usDecisions, euDecisions)
			}
			for _, directive := range []string{":::generate-traffic-button", ":::track-picker", ":::validation-report"} {
				before, after := 0, 0
				for _, step := range us.Steps {
					before += strings.Count(step.Description, directive)
				}
				for _, step := range eu.Steps {
					after += strings.Count(step.Description, directive)
				}
				if before != after {
					t.Errorf("functional activity %s changed: US %d EU %d", directive, before, after)
				}
			}

			renames := map[string]string{"phase-3-create-minimal-required-rules": "implement-rules", "phase-5-validate-allowed-traffic": "test-positive", "phase-6-validate-blocked-traffic": "test-negative", "attack-dnp3-direct-operate-injection-against-the-recloser": "attack-iec104-breaker-open"}
			for i, before := range us.Steps {
				after := eu.Steps[i]
				wantID := before.ID
				if renamed, ok := renames[wantID]; ok {
					wantID = renamed
				}
				if after.ID != wantID {
					t.Errorf("step %d = %s want %s", i, after.ID, wantID)
				}
				for _, fence := range []string{":::guided", ":::technical"} {
					if strings.Contains(before.Description, fence) != strings.Contains(after.Description, fence) {
						t.Errorf("%s lost %s split", after.ID, fence)
					}
				}
				manualWire := id == "hardening-configurations" && (i == 1 || i == 4) || id == "vendor-rdp-compromise" && i == 2
				if manualWire {
					if after.Action != nil {
						t.Errorf("%s wire operation is not manual", after.ID)
					}
					continue
				}
				// US hotfix steps verify a reference already applied by the student;
				// EU still applies its package policy. Pin both complete actions,
				// rather than requiring unrelated packages to mutate identically.
				if before.ID == "phase-3-create-minimal-required-rules" || before.ID == "apply-the-hardened-policy" || before.ID == "apply-hardened-policy" {
					wantUS := &labs.StepAction{Type: "check", Expect: map[string]any{"firewall_config": "improved"}}
					wantEU := &labs.StepAction{Type: "firewall", Config: "improved"}
					if !reflect.DeepEqual(before.Action, wantUS) || !reflect.DeepEqual(after.Action, wantEU) {
						t.Errorf("%s policy actions = US %#v / EU %#v, want non-mutating US check / EU apply", after.ID, before.Action, after.Action)
					}
					continue
				}
				if (before.Action == nil) != (after.Action == nil) || before.Action != nil && before.Action.Type != after.Action.Type {
					t.Errorf("%s changed activity type", after.ID)
				}
			}
		})
	}
}
