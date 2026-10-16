package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
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

// EU now uses the same suite route; verify capability dispatch without a live range.
func TestEURecipeSuiteRoute(t *testing.T) {
	s := rangeServer(euGeneration(t, containd.NewClient(closedURL(t)), nil), "weak")
	database, err := db.Connect(filepath.Join(t.TempDir(), "suite.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.db = database
	rec := callRoute(s, s.handleWorkshopTestSuite, http.MethodPost, "/api/workshop/test-suite", "/api/workshop/test-suite", "")
	var result testSuiteResult
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.TotalTests != 0 {
		t.Fatalf("suite dispatch=%d %s", rec.Code, rec.Body)
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
	for _, pair := range []string{"Control Centre → Station", "Station → Field", "Vendor → Station", "Control Centre → Operations DMZ", "Enterprise → Operations DMZ"} {
		diff := byPair[pair]
		if diff.WeakAction != "ALLOW" || diff.ImprovedAction != "MIXED" {
			t.Errorf("%s actions = weak %q / improved %q; want ALLOW / MIXED", pair, diff.WeakAction, diff.ImprovedAction)
		}
	}
	for _, pair := range []string{"Enterprise → Station"} {
		diff := byPair[pair]
		if diff.WeakAction != "ALLOW" || diff.ImprovedAction != "DENY" {
			t.Errorf("%s actions = weak %q / improved %q; want ALLOW / DENY", pair, diff.WeakAction, diff.ImprovedAction)
		}
	}
	for _, pair := range []string{
		"Enterprise → Field",
		"Vendor → Field",
	} {
		diff := byPair[pair]
		if diff.WeakAction != "ALLOW" || diff.ImprovedAction != "DENY" {
			t.Errorf("%s actions = weak %q / improved %q; want ALLOW / DENY", pair, diff.WeakAction, diff.ImprovedAction)
		}
		if diff.WeakRule != "WEAK teaching exposure: enterprise and vendor direct Modbus access to field devices (should be blocked)" {
			t.Errorf("%s weak rule = %q; want the direct Modbus teaching exposure", pair, diff.WeakRule)
		}
	}
	for _, pair := range []string{
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

	// MIXED must mean exactly the §8 engineering maintenance exception,
	// not an accidental broad vendor allow hidden behind a deny rule.
	data, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "improved.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Firewall struct {
			DefaultAction string `json:"defaultAction"`
			Rules         []struct {
				containd.FirewallRule
				Destinations []string `json:"destinations"`
			} `json:"rules"`
		} `json:"firewall"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	matchesZone := func(zones []string, zone string) bool {
		return len(zones) == 0 || slices.Contains(zones, zone) || slices.Contains(zones, "any")
	}
	allows := 0
	for _, rule := range policy.Firewall.Rules {
		if rule.Action != "ALLOW" || !matchesZone(rule.SourceZones, "dmz") || !matchesZone(rule.DestZones, "lan1") {
			continue
		}
		allows++
		if !reflect.DeepEqual(rule.SourceZones, []string{"dmz"}) ||
			!reflect.DeepEqual(rule.DestZones, []string{"lan1"}) ||
			!reflect.DeepEqual(rule.Sources, []string{"10.20.20.20/32"}) ||
			!reflect.DeepEqual(rule.Destinations, []string{"10.30.30.30/32"}) ||
			!reflect.DeepEqual(rule.Protocols, []containd.Protocol{{Name: "tcp", Port: "8080"}}) {
			t.Errorf("unexpected Vendor → Station allow: %+v", rule)
		}
		if !strings.Contains(byPair["Vendor → Station"].ImprovedRule, rule.Description) {
			t.Errorf("comparison omits engineering maintenance rule: %+v", byPair["Vendor → Station"])
		}
	}
	if allows != 1 || policy.Firewall.DefaultAction != "DENY" {
		t.Errorf("Vendor → Station allows = %d, default = %s; want one pinned exception and default DENY", allows, policy.Firewall.DefaultAction)
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
	if euRecipe.captureFilter != "tcp port 2404 or tcp port 502 or tcp port 8080 or tcp port 8081" {
		t.Fatalf("EU capture must cover IEC 104, Modbus, HTTP reads/maintenance and historian ingest: %q", euRecipe.captureFilter)
	}
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

func TestEURecipeEnginesResolveAndReachTheirRoutes(t *testing.T) {
	gen := euGeneration(t, containd.NewClient(closedURL(t)), nil)
	matrix, err := euRecipe.resolveValidation(gen)
	if err != nil {
		t.Fatal(err)
	}
	allows, denies := 0, 0
	for _, row := range matrix {
		if row.Expected == "allow" {
			allows++
		} else {
			denies++
		}
		if strings.Contains(row.Note, "RTAC") || row.Port == "20000" {
			t.Fatalf("US row leaked: %+v", row)
		}
	}
	if allows != 9 || denies != 16 {
		t.Fatalf("matrix=%d allow, %d deny", allows, denies)
	}
	hosts, network, err := euRecipe.resolveCapture(gen)
	if err != nil || len(hosts) != 4 || strings.Join(network, ".") != "10.40.40" {
		t.Fatalf("capture=%v %v %v", hosts, network, err)
	}
	traffic, err := euRecipe.resolveTraffic(gen)
	if err != nil || len(traffic) != 3 {
		t.Fatalf("traffic=%v err=%v", traffic, err)
	}
	for _, target := range euRecipe.traffic {
		if target.fromNode == target.toNode {
			t.Fatal("traffic generator includes a local loopback read")
		}
	}
	for _, row := range traffic {
		if strings.Contains(row.cmd, "POST") || strings.Contains(row.cmd, "mbpoll") {
			t.Fatalf("generator must not invent write evidence: %+v", row)
		}
	}
	s := rangeServer(gen, "weak")
	s.orchestrator = &orchestrator.Orchestrator{}
	for _, route := range []struct {
		path    string
		handler gin.HandlerFunc
		code    int
	}{
		{"/api/firewall/validation-report", s.handleValidationReport, http.StatusBadGateway},
		{"/api/traffic/generate", s.handleTrafficGenerate, http.StatusServiceUnavailable},
	} {
		rec := callRoute(s, route.handler, http.MethodPost, route.path, route.path, "")
		if rec.Code != route.code || !strings.Contains(rec.Body.String(), "docker client not available") {
			t.Fatalf("%s=%d %s", route.path, rec.Code, rec.Body)
		}
	}
	if !euRecipe.testSuite {
		t.Fatal("EU authored checks require the suite")
	}
}
