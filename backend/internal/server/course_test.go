package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestFeaturelessCourseListsAndSuiteUsesCoursePosition(t *testing.T) {
	root := filepath.Join(repoRoot(), "backend/internal/labs/testdata/course-basic")
	s := routedServer(t)
	catalog, err := labs.NewLoader(root, ValidatorRequirements()).Seed(context.Background(), s.db, "basic")
	if err != nil {
		t.Fatal(err)
	}
	pkg := catalog.Packages[0]
	var man manifest.Manifest
	data, err := os.ReadFile(filepath.Join(root, "packages/basic/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	fw, _ := fakeContaind(t, 200, 200)
	gen := lifecycle.NewGeneration(1, pkg, &man, containd.NewClient(fw.URL), time.Time{})
	s.rng = rangeServer(gen, "weak").rng
	s.cfg.LabDefinitionsPath = root
	s.setCatalog(catalog)
	rec := serve(s, http.MethodGet, "/api/scenarios")
	var listed struct {
		Scenarios []models.Scenario `json:"scenarios"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(listed.Scenarios) != 2 || listed.Scenarios[0].ID != "first" || listed.Scenarios[0].Label != "B" || listed.Scenarios[1].ID != "second" || listed.Scenarios[1].Label != "A" {
		t.Fatalf("course listing: %d %s", rec.Code, rec.Body)
	}
	// The fixture exercises generic ordering, not a new production recipe. Its
	// reset has no device calls and all appliance requests use the local fake.
	packageRecipes["basic"] = &packageRecipe{testSuite: true}
	t.Cleanup(func() { delete(packageRecipes, "basic") })
	s.orchestrator = &orchestrator.Orchestrator{}
	rec = callRoute(s, s.handleWorkshopTestSuite, http.MethodPost, "/api/workshop/test-suite", "/api/workshop/test-suite", "")
	var suite testSuiteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &suite); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, sc := range suite.Scenarios {
		ids = append(ids, sc.ScenarioID)
	}
	if rec.Code != 200 || !reflect.DeepEqual(ids, []string{"first", "second"}) {
		t.Fatalf("suite order: %d %s", rec.Code, rec.Body)
	}
}

func TestPlanRoutesWithoutFeatureAreUnsupported(t *testing.T) {
	s := newTestServer(t, "")
	s.rng.(*fakeRange).gen.Package.Course.Features.SegmentationPlan = nil
	for _, path := range []string{"/api/plan/compile", "/api/firewall/apply-plan", "/api/workshop/probe-matrix"} {
		rec := callRoute(s, map[string]gin.HandlerFunc{
			"/api/plan/compile":          s.handlePlanCompile,
			"/api/firewall/apply-plan":   s.handleFirewallApplyPlan,
			"/api/workshop/probe-matrix": s.handleProbeMatrix,
		}[path], http.MethodPost, path, path, "not json")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
		}
	}
}
