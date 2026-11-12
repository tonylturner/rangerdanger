package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

func TestScenarioPackagePrecondition(t *testing.T) {
	for _, path := range []string{
		"/api/scenarios/shared/validate",
		"/api/scenarios/shared/steps/invalid/execute",
		"/api/workshop/test-suite",
	} {
		for _, expected := range []string{"", "other-package", testPackageID} {
			t.Run(path+"/"+expected, func(t *testing.T) {
				s := routedServer(t)
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("not json"))
				req.Header.Set("X-RangerDanger-Package", expected)
				rec := httptest.NewRecorder()
				s.engine.ServeHTTP(rec, req)
				if expected == testPackageID {
					if rec.Code == http.StatusPreconditionFailed {
						t.Fatalf("matching package rejected: %s", rec.Body)
					}
				} else if rec.Code != http.StatusPreconditionFailed ||
					!strings.Contains(rec.Body.String(), `"code":"package_precondition"`) ||
					!strings.Contains(rec.Body.String(), testPackageID) ||
					(expected != "" && !strings.Contains(rec.Body.String(), expected)) {
					t.Fatalf("response = %d %s", rec.Code, rec.Body)
				}
			})
		}
	}
}

func TestScenarioLookupsUseBoundGeneration(t *testing.T) {
	s := routedServer(t)
	old := usGeneration(nil)
	s.rng.(*fakeRange).gen = old
	// A switch has already recorded the target, while Enter still admits the
	// old serving generation. Both packages intentionally own the same slug.
	s.rng.(*fakeRange).active = "other-package"
	s.rng.(*fakeRange).status.Phase = lifecycle.PhasePreflight
	for _, row := range []models.Scenario{
		{PackageID: old.Package.ID, ID: "shared", Name: "Old", Steps: `[{"id":"manual","title":"Old generation"}]`},
		{PackageID: "other-package", ID: "shared", Name: "New", Validator: "unknown", Steps: `[]`},
		{PackageID: "other-package", ID: "target-only", Steps: `[]`},
	} {
		if err := s.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Read routes capture the active package, not the draining generation.
	if rec := serve(s, http.MethodGet, "/api/scenarios/shared"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"New"`) {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body)
	}
	rec, _ := rangeRequest(s, http.MethodPost, "/api/scenarios/shared/steps/0/execute", "{}")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"step_title":"Old generation"`) {
		t.Fatalf("execute = %d %s", rec.Code, rec.Body)
	}
	rec, _ = rangeRequest(s, http.MethodPost, "/api/scenarios/shared/validate", "{}")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "scenario has no validator") {
		t.Fatalf("validate = %d %s", rec.Code, rec.Body)
	}
	// Observe the suite's real SQL result and stop before its first reset.
	var selected []models.Scenario
	if err := s.db.Callback().Query().After("gorm:query").Register("observe-suite", func(tx *gorm.DB) {
		if rows, ok := tx.Statement.Dest.(*[]models.Scenario); ok {
			selected = append(selected, (*rows)...)
			tx.AddError(errors.New("stop before suite reset"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	rec, _ = rangeRequest(s, http.MethodPost, "/api/workshop/test-suite", "{}")
	if rec.Code != 500 || len(selected) != 1 || selected[0].PackageID != old.Package.ID {
		t.Fatalf("suite selected %#v, response %d %s", selected, rec.Code, rec.Body)
	}
	if err := s.db.Callback().Query().Remove("observe-suite"); err != nil {
		t.Fatal(err)
	}
	// Status counts the same bound package; no supervisory services are
	// declared here, so only the private HTTP health fixture is contacted.
	fw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fw.Close()
	gen := lifecycle.NewGeneration(2, old.Package, &manifest.Manifest{}, containd.NewClient(fw.URL), old.StartedAt)
	s.rng.(*fakeRange).gen = gen
	rec, body := rangeRequest(s, http.MethodGet, "/api/workshop/status", "")
	if rec.Code != 200 || body["scenario_count"] != float64(1) {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if err := s.db.Callback().Query().Before("gorm:query").Register("fail-count", func(tx *gorm.DB) {
		tx.AddError(errors.New("count unavailable"))
	}); err != nil {
		t.Fatal(err)
	}
	rec, _ = rangeRequest(s, http.MethodGet, "/api/workshop/status", "")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "count unavailable") {
		t.Fatalf("failed count = %d %s", rec.Code, rec.Body)
	}
}

func TestScenarioPackageHeaderAllowedByCORS(t *testing.T) {
	s := routedServer(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/scenarios/shared/validate", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	s.corsMiddleware()(c)
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-RangerDanger-Package") {
		t.Fatal("package header missing from CORS")
	}
}
