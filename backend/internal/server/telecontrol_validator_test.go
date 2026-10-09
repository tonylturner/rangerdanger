package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// telecontrolRange is an EU range whose firewall runs runningPolicy (a file
// of the EU package) and whose CC and breaker answer with the passing
// fixtures, except where overrides (by service key, then route) replace a
// route.
func telecontrolRange(t *testing.T, runningPolicy string, overrides map[string]map[string]stubResponse) (*Server, *lifecycle.Generation, map[string]*upstreamStub) {
	t.Helper()
	policy, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", runningPolicy))
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]map[string]stubResponse{
		"firewall": {
			"GET /api/v1/config":            {http.StatusOK, string(policy)},
			"GET /api/v1/events?limit=5000": {http.StatusOK, passEvents},
		},
		"control_centre": {
			"GET /api/state":           {http.StatusOK, passCCState},
			"GET /api/journal?after=0": {http.StatusOK, passJournal},
		},
		"feeder_breaker": {
			"GET /api/state":         {http.StatusOK, passDevice},
			"GET /api/audit?after=0": {http.StatusOK, passAudit},
		},
	}
	for service, replaced := range overrides {
		for route, response := range replaced {
			routes[service][route] = response
		}
	}
	stubs := map[string]*upstreamStub{}
	urls := map[string]string{}
	for service, serviceRoutes := range routes {
		stub, srv := newUpstreamStub(t, serviceRoutes)
		stubs[service], urls[service] = stub, srv.URL
	}
	gen := euGeneration(t, containd.NewClient(urls["firewall"]), map[string]string{
		"control_centre": urls["control_centre"], "feeder_breaker": urls["feeder_breaker"],
	})
	return rangeServer(gen, "improved"), gen, stubs
}

// TestTelecontrolValidatorCollectsFromTheManifest runs the validator over
// test services: it passes on the passing exercise, and reads every source
// through the manifest's endpoints and the generation's start.
func TestTelecontrolValidatorCollectsFromTheManifest(t *testing.T) {
	s, gen, stubs := telecontrolRange(t, "improved.json", nil)
	checks := scenarioValidators["eu-iec104-telecontrol"].run(t.Context(), s, validatorInput{gen: gen, activeConfig: "improved"})
	for _, check := range checks {
		if check.Status != "pass" {
			t.Errorf("%s: %s %s", check.Name, check.Status, check.Detail)
		}
	}
	ev := s.collectTelecontrolEvidence(t.Context(), gen, "improved")
	if ev.rtuIP != "10.30.30.20" || ev.ccIP != "10.60.60.20" || !ev.rangeStart.Equal(euStartedAt) {
		t.Errorf("evidence identities = rtu %s, cc %s, start %s", ev.rtuIP, ev.ccIP, ev.rangeStart)
	}
	for service, stub := range stubs {
		if len(stub.requests) == 0 {
			t.Errorf("validator never read %s", service)
		}
	}
}

func TestTelecontrolValidatorFailsOnMissingSources(t *testing.T) {
	tests := []struct {
		name          string
		runningPolicy string
		overrides     map[string]map[string]stubResponse
		failing       []string
	}{
		{"weak policy running", "weak.json", nil, []string{checkPolicy}},
		{"control centre down", "improved.json", map[string]map[string]stubResponse{"control_centre": {
			"GET /api/state":           {http.StatusServiceUnavailable, `{}`},
			"GET /api/journal?after=0": {http.StatusServiceUnavailable, `{}`},
		}}, []string{checkOutage, checkTrip, checkOperator}},
		{"device audit down", "improved.json", map[string]map[string]stubResponse{"feeder_breaker": {
			"GET /api/audit?after=0": {http.StatusInternalServerError, `boom`},
		}}, []string{checkTrip}},
		{"events down", "improved.json", map[string]map[string]stubResponse{"firewall": {
			"GET /api/v1/events?limit=5000": {http.StatusBadGateway, `{"error":"engine down"}`},
		}}, []string{checkDeny}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, gen, _ := telecontrolRange(t, test.runningPolicy, test.overrides)
			checks := s.validateTelecontrol(t.Context(), gen, "improved")
			failing := map[string]bool{}
			for _, check := range checks {
				if check.Status != "pass" {
					failing[check.Name] = true
				}
			}
			if len(failing) != len(test.failing) {
				t.Errorf("failing = %v, want %v", failing, test.failing)
			}
			for _, name := range test.failing {
				if !failing[name] {
					t.Errorf("%s passed, want it to fail", name)
				}
			}
		})
	}
}
