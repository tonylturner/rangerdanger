package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestEUCanaryResolvesFromTheManifest(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	from, err := nodeService(gen, euRecipe.canary.fromNode)
	if err != nil || from.Container != "rangerdanger-kali" {
		t.Errorf("canary source = %+v, %v", from, err)
	}
	if ip, err := nodeIP(gen, euRecipe.canary.toNode, euRecipe.canary.network); err != nil || ip != "10.30.30.20" || euRecipe.canary.port != 2404 {
		t.Errorf("canary target = %s:%d, %v; want the RTU's IEC 104 port", ip, euRecipe.canary.port, err)
	}
	for _, name := range []string{euRecipe.canary.allowUnder, euRecipe.canary.denyUnder} {
		if _, ok := gen.Package.Policies[name]; !ok {
			t.Errorf("canary policy %q is not an EU package policy", name)
		}
	}
	if policy, err := defaultPolicy(gen); err != nil || policy != "weak" {
		t.Errorf("EU default policy = %q, %v", policy, err)
	}
}

// TestEURecipeUnsupportedRoutes answers 404 for the US-built report,
// traffic generator and test suite.
func TestEURecipeUnsupportedRoutes(t *testing.T) {
	s := rangeServer(euGeneration(t, containd.NewClient(closedURL(t)), nil), "weak")
	s.orchestrator = &orchestrator.Orchestrator{}
	routes := []struct {
		method, path string
		handler      gin.HandlerFunc
	}{
		{http.MethodPost, "/api/firewall/validation-report", s.handleValidationReport},
		{http.MethodPost, "/api/traffic/generate", s.handleTrafficGenerate},
		{http.MethodPost, "/api/workshop/test-suite", s.handleWorkshopTestSuite},
	}
	for _, route := range routes {
		rec := callRoute(s, route.handler, route.method, route.path, route.path, "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not supported by this package"}` {
			t.Errorf("%s %s on EU = %d %s", route.method, route.path, rec.Code, rec.Body)
		}
	}
}

func TestFirewallCompareEU(t *testing.T) {
	s := rangeServer(euGeneration(t, containd.NewClient(closedURL(t)), nil), "weak")
	rec := callRoute(s, s.handleFirewallCompare, http.MethodGet, "/api/firewall/compare", "/api/firewall/compare", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/firewall/compare on EU = %d %s; want 200", rec.Code, rec.Body)
	}

	var got PolicyComparison
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WeakConfig != "weak.json" || got.ImprovedConfig != "improved.json" {
		t.Errorf("comparison policies = %q / %q; want EU package weak.json / improved.json", got.WeakConfig, got.ImprovedConfig)
	}
	wantPairs := []string{
		"Control Centre → Station",
		"Station → Field",
		"Enterprise → Station",
		"Vendor → Station",
		"Enterprise → Field",
		"Vendor → Field",
		"Control Centre → Field",
		"Field → Station",
		"Control Centre → Operations DMZ",
		"Enterprise → Operations DMZ",
	}
	if len(got.Diffs) != len(wantPairs) {
		t.Fatalf("comparison rows = %d; want %d: %+v", len(got.Diffs), len(wantPairs), got.Diffs)
	}
	for i, want := range wantPairs {
		if got.Diffs[i].ZonePair != want {
			t.Errorf("comparison row %d = %q; want %q", i, got.Diffs[i].ZonePair, want)
		}
	}

	byPair := make(map[string]PolicyRuleDiff, len(got.Diffs))
	for _, diff := range got.Diffs {
		byPair[diff.ZonePair] = diff
	}
	for _, pair := range []string{"Control Centre → Station", "Station → Field"} {
		diff := byPair[pair]
		if diff.WeakAction != "ALLOW" || diff.ImprovedAction != "MIXED" {
			t.Errorf("%s actions = weak %q / improved %q; want ALLOW / MIXED", pair, diff.WeakAction, diff.ImprovedAction)
		}
	}
	for _, pair := range []string{"Enterprise → Station", "Vendor → Station"} {
		diff := byPair[pair]
		if diff.WeakAction != "ALLOW" || diff.ImprovedAction != "DENY" {
			t.Errorf("%s actions = weak %q / improved %q; want ALLOW / DENY", pair, diff.WeakAction, diff.ImprovedAction)
		}
	}
	for _, pair := range []string{
		"Enterprise → Field",
		"Vendor → Field",
		"Control Centre → Field",
		"Field → Station",
	} {
		diff := byPair[pair]
		if diff.WeakAction != "DENY" || diff.ImprovedAction != "DENY" {
			t.Errorf("%s actions = weak %q / improved %q; want DENY / DENY", pair, diff.WeakAction, diff.ImprovedAction)
		}
		if !strings.Contains(diff.WeakRule, "implicit default-deny") {
			t.Errorf("%s weak rule = %q; want the implicit default-deny distinction", pair, diff.WeakRule)
		}
	}
}

func TestFirewallCompareRequiresDeclaredPolicies(t *testing.T) {
	gen := euGeneration(t, containd.NewClient(closedURL(t)), nil)
	delete(gen.Package.Policies, euRecipe.compare.weakPolicy)
	s := rangeServer(gen, "weak")
	rec := callRoute(s, s.handleFirewallCompare, http.MethodGet, "/api/firewall/compare", "/api/firewall/compare", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "config must be one of") {
		t.Errorf("compare with undeclared recipe policy = %d %s; want 404", rec.Code, rec.Body)
	}
}

// TestFirewallRulesFallbackIsUSOnly: with containd down the US package
// shows its static summaries; the EU package fails.
func TestFirewallRulesFallbackIsUSOnly(t *testing.T) {
	down := containd.NewClient(closedURL(t))
	us := rangeServer(usGeneration(down), "weak")
	rec := callRoute(us, us.handleGetFirewallRules, http.MethodGet, "/api/firewall/rules", "/api/firewall/rules", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"source":"static"`) {
		t.Errorf("US rules with containd down = %d %s", rec.Code, rec.Body)
	}
	eu := rangeServer(euGeneration(t, down, nil), "weak")
	rec = callRoute(eu, eu.handleGetFirewallRules, http.MethodGet, "/api/firewall/rules", "/api/firewall/rules", "")
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "summaries") {
		t.Errorf("EU rules with containd down = %d %s, want 502", rec.Code, rec.Body)
	}
}

func workshopStatus(t *testing.T, s *Server) map[string]any {
	t.Helper()
	database, err := db.Connect(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.db = database
	rec := callRoute(s, s.handleGetWorkshopStatus, http.MethodGet, "/api/workshop/status", "/api/workshop/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/workshop/status = %d %s", rec.Code, rec.Body)
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestWorkshopStatusEU(t *testing.T) {
	down := containd.NewClient(closedURL(t))
	_, cc := newUpstreamStub(t, map[string]stubResponse{"GET /api/state": {http.StatusOK, ccStateFixture}})
	status := workshopStatus(t, rangeServer(euGeneration(t, down, map[string]string{"control_centre": cc.URL}), "weak"))
	want := map[string]any{
		"workshop_id":     "telecontrol-segmentation",
		"workshop_name":   "European Feeder Telecontrol Segmentation",
		"firewall_online": false,
		"firewall_config": "weak",
		"scenario_count":  float64(0),
		"control_centre":  map[string]any{"link": "started", "gi": "complete"},
	}
	if !reflect.DeepEqual(status, want) {
		t.Errorf("EU status = %v\nwant %v", status, want)
	}

	status = workshopStatus(t, rangeServer(euGeneration(t, down, map[string]string{"control_centre": closedURL(t)}), "weak"))
	if cc := status["control_centre"]; !reflect.DeepEqual(cc, map[string]any{"link": nil, "gi": nil}) {
		t.Errorf("EU status with the CC down: control_centre = %v, want null link and gi", cc)
	}
}

// TestWorkshopStatusUSUnchanged keeps the US status shape and name.
func TestWorkshopStatusUSUnchanged(t *testing.T) {
	gen := usGeneration(containd.NewClient(closedURL(t)))
	for i, svc := range gen.Manifest.Services {
		if svc.Key == "rtac_sim" {
			gen.Manifest.Services[i].Endpoints = map[string]string{"api": closedURL(t)}
		}
	}
	status := workshopStatus(t, rangeServer(gen, "weak"))
	want := map[string]any{
		"workshop_id":     "substation-segmentation",
		"workshop_name":   "Distribution Substation Segmentation",
		"rtac_online":     false,
		"firewall_online": false,
		"firewall_config": "weak",
		"scenario_count":  float64(0),
		"device_comms":    map[string]any{},
	}
	if !reflect.DeepEqual(status, want) {
		t.Errorf("US status = %v\nwant %v", status, want)
	}
}

// TestEUPcapUsesTheCaptureFilter skips containd's capture API, whose
// structured filter cannot name ports, and goes to tcpdump.
func TestEUPcapUsesTheCaptureFilter(t *testing.T) {
	stub, fw := newUpstreamStub(t, nil)
	s := rangeServer(euGeneration(t, containd.NewClient(fw.URL), nil), "weak")
	s.orchestrator = &orchestrator.Orchestrator{}
	rec := callRoute(s, s.handlePcapStart, http.MethodPost, "/api/pcap/start", "/api/pcap/start", `{"duration_sec":5}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "docker client not available") {
		t.Errorf("EU pcap start without Docker = %d %s, want the tcpdump path", rec.Code, rec.Body)
	}
	if len(stub.requests) != 0 {
		t.Errorf("containd capture API called: %q", stub.requests)
	}
}

func TestFirewallStepLabelsFollowTheRecipe(t *testing.T) {
	fw, _ := fakeContaind(t, http.StatusOK, http.StatusOK)
	eu := rangeServer(euGeneration(t, containd.NewClient(fw.URL), nil), "weak")
	got := eu.executeFirewallAction(t.Context(), eu.rng.(*fakeRange).gen, "improved")
	if !got.Success || got.Detail != "containd policy set to improved (source-pinned IEC 104 and Modbus control, PLC maintenance and historian conduits)" {
		t.Errorf("EU firewall step = %+v", got)
	}
	us := rangeServer(usGeneration(containd.NewClient(fw.URL)), "weak")
	for policy, want := range map[string]string{"weak": "weak baseline", "improved": "hardened (RTAC-only field access)"} {
		got := us.executeFirewallAction(t.Context(), us.rng.(*fakeRange).gen, policy)
		if got.Detail != "containd policy set to "+want {
			t.Errorf("US firewall step %s = %+v", policy, got)
		}
	}
}
