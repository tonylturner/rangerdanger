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
// traffic generator, test suite and policy comparison.
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
		{http.MethodGet, "/api/firewall/compare", s.handleFirewallCompare},
	}
	for _, route := range routes {
		rec := callRoute(s, route.handler, route.method, route.path, route.path, "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"not supported by this package"}` {
			t.Errorf("%s %s on EU = %d %s", route.method, route.path, rec.Code, rec.Body)
		}
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

// TestEUWorkshopReset applies the default policy and resets the breaker
// through its instructor API; no RTAC command is sent.
func TestEUWorkshopReset(t *testing.T) {
	fw, counters := fakeContaind(t, http.StatusOK, http.StatusOK)
	device, dev := newUpstreamStub(t, map[string]stubResponse{
		"POST /api/instructor/reset": {http.StatusOK, deviceStateFixture},
	})
	s := rangeServer(euGeneration(t, containd.NewClient(fw.URL), map[string]string{"feeder_breaker": dev.URL}), "improved")
	s.orchestrator = &orchestrator.Orchestrator{}
	rec := callRoute(s, s.handleWorkshopReset, http.MethodPost, "/api/workshop/reset", "/api/workshop/reset", "")
	want := `{"actions":[` +
		`{"action":"Apply weak firewall baseline","success":true,"detail":"weak config applied"},` +
		`{"action":"Reset the instructor device to its initial state","success":true,"detail":"device reset"}` +
		`],"success":true}`
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Errorf("POST reset = %d %s\nwant %s", rec.Code, rec.Body, want)
	}
	if counters.commitCount != 1 || s.activeConfig != "weak" {
		t.Errorf("commits = %d, active = %q; want the weak policy applied", counters.commitCount, s.activeConfig)
	}
	if !reflect.DeepEqual(device.requests, []string{"POST /api/instructor/reset"}) || device.bodies[0] != `{}` {
		t.Errorf("device requests = %q %q", device.requests, device.bodies)
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
	if !got.Success || got.Detail != "containd policy set to improved (control centre to RTU IEC 104, RTU to breaker Modbus only)" {
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
