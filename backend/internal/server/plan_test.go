package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func planRequest(s *Server, ids ...string) any {
	pkg := s.rng.(*fakeRange).gen.Package
	return map[string]any{"packageId": pkg.ID, "revision": pkg.Revision, "actionIds": ids}
}

func TestPlanHandlersAndProvenance(t *testing.T) {
	var running []byte
	failCommit, mismatch := false, false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/config/candidate":
			running, _ = io.ReadAll(r.Body)
		case "/api/v1/config/commit":
			if failCommit {
				w.WriteHeader(500)
				return
			}
			w.Header().Add("X-Containd-Warnings", "example warning")
		case "/api/v1/config":
			if mismatch {
				io.WriteString(w, `{"firewall":{}}`)
				return
			}
			w.Write(running)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	s := newTestServer(t, fake.URL)
	for _, tc := range []struct {
		body any
		code int
	}{
		{"{", 400}, {map[string]any{"packageId": "other", "revision": 1}, 409},
		{map[string]any{"packageId": usPackageID, "revision": s.rng.(*fakeRange).gen.Package.Revision + 1}, 409},
		{planRequest(s, "missing"), 400}, {planRequest(s), 200},
	} {
		rec, _ := invoke(s, s.handlePlanCompile, "POST", "/api/plan/compile", tc.body)
		if rec.Code != tc.code {
			t.Fatalf("compile %v = %d %s", tc.body, rec.Code, rec.Body)
		}
	}
	ids := []string{}
	for _, action := range s.rng.(*fakeRange).gen.Package.Curriculum.Catalog {
		ids = append(ids, action.ID)
	}
	for _, selected := range [][]string{nil, {"block-enterprise-to-field"}, ids} {
		rec, body := invoke(s, s.handleFirewallApplyPlan, "POST", "/api/firewall/apply-plan", planRequest(s, selected...))
		if rec.Code != 200 || body["applied"] != true {
			t.Fatalf("apply = %d %s", rec.Code, rec.Body)
		}
		if len(body["warnings"].([]any)) != 1 {
			t.Fatal("warnings lost")
		}
		var result struct {
			Plan planResponse `json:"plan"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Plan.Expectations) != 21 || result.Plan.Expectations[0].FromIP == "" || len(result.Plan.Config) == 0 {
			t.Fatal("incomplete compilation response")
		}
		if s.activeConfig != "custom" || s.policySource != "plan" || s.appliedPlan == nil || s.planProvenance == nil {
			t.Fatal("missing provenance/snapshot")
		}
		s.lastAppliedAt = time.Now().Add(-time.Minute)
		s.classifyObservedPolicy(result.Plan.PolicyHash, time.Now())
		if s.policySource != "plan" {
			t.Fatal("matching plan relabelled")
		}
	}
	improved, err := os.ReadFile(filepath.Join(s.cfg.LabDefinitionsPath, s.rng.(*fakeRange).gen.Package.Policies["improved"]))
	if err != nil {
		t.Fatal(err)
	}
	improvedHash, _ := containd.FirewallHashFromBytes(improved)
	if s.planProvenance.PolicyHash != improvedHash {
		t.Fatal("full plan not Hardened")
	}
	rec, body := invoke(s, s.handleFirewallActive, "GET", "/api/firewall/active", nil)
	if rec.Code != 200 || body["plan"] == nil {
		t.Fatal("active lacks provenance")
	}
	s.classifyObservedPolicy("different", time.Now())
	if s.planProvenance != nil || s.appliedPlan != nil || s.policySource != "manual-custom" {
		t.Fatal("divergence retained plan")
	}
	invoke(s, s.handleFirewallApplyPlan, "POST", "/api/firewall/apply-plan", planRequest(s))
	invoke(s, s.handleFirewallApply, "POST", "/api/firewall/apply", map[string]string{"config": "weak"})
	if s.planProvenance != nil || s.appliedPlan != nil {
		t.Fatal("named apply retained plan")
	}
	for _, mode := range []string{"import", "hash"} {
		failCommit, mismatch = mode == "import", mode == "hash"
		rec, body := invoke(s, s.handleFirewallApplyPlan, "POST", "/api/firewall/apply-plan", planRequest(s))
		if rec.Code != 422 || body["warnings"] == nil || s.planProvenance != nil {
			t.Fatalf("%s: %d %s", mode, rec.Code, rec.Body)
		}
	}
}
