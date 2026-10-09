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
			{ID: "other-package", Title: "Other", Revision: 2, Template: labs.LabYAML{ID: "other-workshop"}},
			{ID: testPackageID, Title: "Test", Revision: 1, Template: labs.LabYAML{ID: testTemplateID}},
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
	want := []packageSummary{
		{ID: "other-package", Title: "Other", Revision: 2},
		{ID: testPackageID, Title: "Test", Revision: 1, Active: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %#v, want %#v", got, want)
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
		ID: "us-dnp3-substation", Title: "US distribution substation (DNP3)", Revision: 1,
		Capabilities: []string{"process.electrical", "policy.containd", "audit.device-control", "capture.firewall"},
	}
	if !ok || !reflect.DeepEqual(info, wantInfo) {
		t.Errorf("default package = %#v, %v; want %#v", info, ok, wantInfo)
	}
	for _, pkg := range catalog.Packages {
		if pkg.ID != config.DefaultPackage {
			continue
		}
		if pkg.FirewallConfigPath != "firewall/substation-weak.json" {
			t.Errorf("US firewall config path = %q, want firewall/substation-weak.json", pkg.FirewallConfigPath)
		}
		if pkg.Template.ID != "substation-segmentation" {
			t.Errorf("US topology id = %q, want substation-segmentation", pkg.Template.ID)
		}
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
	for key := range scenarioValidators {
		if !used[key] {
			t.Errorf("registered validator %q is not declared by any shipped scenario", key)
		}
	}
}
