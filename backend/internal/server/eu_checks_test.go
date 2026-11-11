package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

func TestEUCheckDispatchAndMissingEvidence(t *testing.T) {
	for _, failure := range []string{"", "device down", "missing position", "bad identity", "stale solve", "bad CC quality", "no GI", "null flag"} {
		t.Run(failure, func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[0])
			device := ev.devices["breaker-1"]
			switch failure {
			case "missing position":
				device.Position = ""
			case "bad identity":
				device.DeviceID = "some-other-device"
			case "stale solve":
				ev.process.SolvedAt = time.Now().Add(-time.Minute)
			case "bad CC quality":
				ev.cc.Points[0].Quality.IV = true
			case "no GI":
				ev.journal.Entries = nil
			case "null flag":
				ev.devices["oltc-1"].Remote = nil
			}
			if failure != "stale solve" {
				ev.process.SolvedAt = time.Now().UTC()
			}
			for i := range ev.cc.Points {
				ev.cc.Points[i].ReceivedAt = time.Now().UTC()
			}
			// Keep the real generation start but make fixture GI belong to it.
			for i := range ev.journal.Entries {
				ev.journal.Entries[i].At = time.Now().Add(-time.Second)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failure == "device down" && r.URL.Path == "/breaker-1/api/state" {
					http.Error(w, "down", 503)
					return
				}
				var body any
				switch r.URL.Path {
				case "/process/api/electrical":
					body = ev.process
				case "/cc/api/state":
					body = ev.cc
				case "/cc/api/journal":
					body = ev.journal
				default:
					node := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/api/state")
					body = ev.devices[node]
					if body == nil {
						t.Errorf("unexpected non-EU endpoint: %s", r.URL.Path)
					}
				}
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			gen := multiDeviceGeneration(t, upstream.URL)
			// multiDeviceGeneration binds device and process APIs. Bind the CC
			// explicitly; no RTAC endpoint exists in this EU manifest.
			for i := range gen.Manifest.Services {
				if gen.Manifest.Services[i].Key == "control_centre" {
					gen.Manifest.Services[i].Endpoints["api"] = upstream.URL + "/cc"
				}
			}
			s := rangeServer(gen, "custom")
			expect := map[string]any{"breaker_closed": true, "supply_present": true, "cc_link_fresh": true, "rmu_normal": true, "oltc_auto": true}
			results := s.executeCheck(t.Context(), gen, expect)
			if len(results) != 5 {
				t.Fatalf("results=%+v", results)
			}
			pass := true
			for _, result := range results {
				pass = pass && result.Success
				switch result.Action {
				case "Breaker closed", "Feeder supply present", "CC link, GI and station delivery", "RMU1 closed and RMU2 open", "OLTC remote automatic mode and tap in range":
				default:
					t.Fatalf("non-human check label: %+v", result)
				}
				if strings.Count(strings.ToLower(result.Detail), "missing evidence:") > 1 {
					t.Fatalf("doubled evidence prefix: %+v", result)
				}
			}
			if pass != (failure == "") {
				t.Fatalf("results=%+v", results)
			}
			if failure != "" {
				// Missing observations must not become successful 'false' checks.
				for key := range expect {
					if key != "firewall_config" {
						expect[key] = false
					}
				}
				results = s.executeCheck(t.Context(), gen, expect)
				for _, result := range results {
					if strings.HasPrefix(result.Detail, "Missing evidence:") && result.Success {
						t.Fatal("unknown became false")
					}
				}
			}
			for _, expect := range []map[string]any{{"typo": true}, {"breaker_closed": "true"}, {"breaker_closed": nil}, {"firewall_config": nil}, {}} {
				for _, r := range s.executeCheck(t.Context(), gen, expect) {
					if r.Success || strings.Contains(r.Detail, "<nil>") {
						t.Fatalf("invalid check passed: %+v", r)
					}
				}
			}
		})
	}
}

func TestEULinkRequiresRecentGIWithoutExpiringConnection(t *testing.T) {
	ev := passingEUWorkshop(t, euWorkshopSlots[0])
	ev.now = ev.now.Add(time.Hour)
	if got := euLink(ev.cc, ev.journal, ev.stationPoints, ev.start, ev.now); got.Status != "fail" || !strings.Contains(got.Detail, "Interrogate station") {
		t.Fatal(got)
	}
	for i := range ev.journal.Entries {
		ev.journal.Entries[i].At = ev.journal.Entries[i].At.Add(time.Hour)
	}
	for i := range ev.cc.Points {
		ev.cc.Points[i].ReceivedAt = ev.now.Add(-time.Second)
	}
	if got := euLink(ev.cc, ev.journal, ev.stationPoints, ev.start, ev.now); got.Status != "pass" {
		t.Fatal(got)
	}
}

func TestEUCheckNegativeAndBoundaryValues(t *testing.T) {
	now := at("12:05:00")
	ev := passingEUWorkshop(t, euWorkshopSlots[0])
	for _, delta := range []time.Duration{-4 * time.Second, -3 * time.Second, time.Second} {
		ev.process.SolvedAt = now.Add(delta)
		_, err := euSupply(ev.process, now)
		if (err == nil) != (delta == -3*time.Second) {
			t.Fatalf("age %s err=%v", delta, err)
		}
	}
	state := ev.devices["oltc-1"]
	for _, tap := range []int{-9, -8, 8, 9} {
		state.Tap = &tap
		ok, err := euOLTCAuto(state)
		if err != nil || ok != (tap >= -8 && tap <= 8) {
			t.Fatalf("tap %d ok=%t err=%v", tap, ok, err)
		}
	}
}

func TestEUFirewallConfigCheckUsesSharedPlanJudgment(t *testing.T) {
	for _, mode := range []string{"applied plan", "weak baseline", "divergent recorded identity", "divergent plan provenance"} {
		t.Run(mode, func(t *testing.T) {
			s, gen, setRunning, _ := euPolicyCheckFixture(t)
			compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-field"})
			if err != nil {
				t.Fatal(err)
			}
			hash, err := containd.FirewallHashFromBytes(compiled.Config)
			if err != nil {
				t.Fatal(err)
			}
			setRunning(compiled.Config)
			s.activeConfig, s.policySource, s.lastAppliedHash = "custom", "plan", hash
			s.planProvenance = &planProvenance{PolicyHash: hash}
			snapshot := compiled
			s.appliedPlan = &snapshot
			if mode == "weak baseline" {
				weak, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "weak.json"))
				if err != nil {
					t.Fatal(err)
				}
				weakHash, err := containd.FirewallHashFromBytes(weak)
				if err != nil {
					t.Fatal(err)
				}
				setRunning(weak)
				s.policySource, s.lastAppliedHash = "weak", weakHash
				s.planProvenance, s.appliedPlan = nil, nil
			}
			if mode == "divergent recorded identity" {
				s.lastAppliedHash = hash + "-different"
			}
			if mode == "divergent plan provenance" {
				s.planProvenance.PolicyHash = hash + "-different"
			}
			results := s.executeEUCheck(t.Context(), gen, map[string]any{"firewall_config": "improved"})
			wantPass := mode == "applied plan"
			if len(results) != 1 || results[0].Success != wantPass {
				t.Fatalf("results=%+v", results)
			}
			if mode == "applied plan" {
				labelOnly := s.executeEUCheck(t.Context(), gen, map[string]any{"firewall_config": "custom"})
				if len(labelOnly) != 1 || labelOnly[0].Success || !strings.Contains(labelOnly[0].Detail, "unsupported firewall_config expectation") {
					t.Fatalf("label-only expectation was not rejected: %+v", labelOnly)
				}
			}
			switch mode {
			case "weak baseline":
				if !strings.Contains(results[0].Detail, "weak baseline remains active") {
					t.Fatalf("weak baseline was not identified: %+v", results[0])
				}
			case "divergent recorded identity":
				if !strings.Contains(results[0].Detail, "differs from recorded identity") {
					t.Fatalf("identity divergence was not identified: %+v", results[0])
				}
			case "divergent plan provenance":
				if !strings.Contains(results[0].Detail, "applied plan identity missing or divergent") {
					t.Fatalf("plan provenance divergence was not identified: %+v", results[0])
				}
			}
		})
	}
}

func TestEUAppliedPlanMatchingWeakHashPassesWithPreventionGaps(t *testing.T) {
	s, gen, setRunning, _ := euPolicyCheckFixture(t)
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
	if err != nil {
		t.Fatal(err)
	}
	weak, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "weak.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Model an applied empty plan whose compiled policy is exactly Weak. The
	// checked-in EU curriculum currently compiles a different policy document.
	compiled.Config = weak
	hash, err := containd.FirewallHashFromBytes(compiled.Config)
	if err != nil {
		t.Fatal(err)
	}
	weakHash, err := containd.FirewallHashFromBytes(weak)
	if err != nil {
		t.Fatal(err)
	}
	if hash != weakHash {
		t.Fatalf("empty EU plan hash %s differs from Weak hash %s", hash, weakHash)
	}

	gaps := []string{}
	for _, expectation := range compiled.Expectations {
		if expectation.PlanVerdict != expectation.HardenedVerdict {
			gaps = append(gaps, expectation.ID)
		}
	}
	if len(gaps) == 0 {
		t.Fatal("empty EU plan has no Hardened prevention gaps")
	}

	setRunning(compiled.Config)
	s.activeConfig, s.policySource, s.lastAppliedHash = "custom", "plan", hash
	s.planProvenance = &planProvenance{PolicyHash: hash}
	snapshot := compiled
	s.appliedPlan = &snapshot
	results := s.executeEUCheck(t.Context(), gen, map[string]any{"firewall_config": "improved"})
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("weak-equivalent applied plan failed: %+v", results)
	}
	if !strings.Contains(results[0].Detail, "applied plan; Hardened prevention gaps:") {
		t.Fatalf("judgment omitted applied-plan gaps: %+v", results[0])
	}
	for _, gap := range gaps {
		if !strings.Contains(results[0].Detail, gap) {
			t.Fatalf("judgment omitted prevention gap %q: %+v", gap, results[0])
		}
	}
}

func TestEUFirewallPolicyPhaseExecuteDoesNotApply(t *testing.T) {
	s, gen, setRunning, applyCalls := euPolicyCheckFixture(t)
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, []string{"block-enterprise-to-field"})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := containd.FirewallHashFromBytes(compiled.Config)
	if err != nil {
		t.Fatal(err)
	}
	setRunning(compiled.Config)
	s.activeConfig, s.policySource, s.lastAppliedHash = "custom", "plan", hash
	s.planProvenance = &planProvenance{PolicyHash: hash}
	snapshot := compiled
	s.appliedPlan = &snapshot
	seedEUScenario(t, s)
	phases := []struct {
		scenario string
		stepID   string
	}{
		{"eu-firewall-implementation", "implement-rules"},
		{"eu-hardening-configurations", "apply-the-hardened-policy"},
		{"eu-vendor-rdp-compromise", "apply-hardened-policy"},
	}
	for _, phase := range phases {
		t.Run(phase.stepID, func(t *testing.T) {
			scenario, err := s.findActiveScenario(phase.scenario)
			if err != nil {
				t.Fatal(err)
			}
			var authored []labs.ScenarioStep
			if err := json.Unmarshal([]byte(scenario.Steps), &authored); err != nil {
				t.Fatal(err)
			}
			stepIndex := -1
			for index, step := range authored {
				if step.ID == phase.stepID {
					stepIndex = index
					if step.Action == nil || step.Action.Type != "check" {
						t.Fatalf("%s action = %#v, want check", step.ID, step.Action)
					}
					break
				}
			}
			if stepIndex < 0 {
				t.Fatalf("missing policy phase %s", phase.stepID)
			}
			path := fmt.Sprintf("/api/scenarios/%s/steps/%d/execute", phase.scenario, stepIndex)
			rec := callRoute(s, s.handleExecuteStep, http.MethodPost,
				"/api/scenarios/:id/steps/:stepIdx/execute", path, "")
			var result StepExecutionResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || result.ActionType != "check" || !result.Success {
				t.Fatalf("execution=%d %s", rec.Code, rec.Body)
			}
		})
	}
	if got := applyCalls(); got != 0 {
		t.Fatalf("Execute Step made %d policy mutation request(s)", got)
	}
}

func euPolicyCheckFixture(t *testing.T) (*Server, *lifecycle.Generation, func([]byte), func() int) {
	t.Helper()
	var running []byte
	reads, mutations := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/config" {
			mutations++
			http.Error(w, "unexpected policy mutation", http.StatusMethodNotAllowed)
			return
		}
		reads++
		_, _ = w.Write(running)
	}))
	t.Cleanup(upstream.Close)
	gen := euGeneration(t, containd.NewClient(upstream.URL), nil)
	s := rangeServer(gen, "custom")
	setRunning := func(data []byte) { running = data }
	mutationCount := func() int { return mutations }
	return s, gen, setRunning, mutationCount
}

func TestEUWorkshopCollectorUsesLivePolicyAndEUOnlySources(t *testing.T) {
	for _, mode := range []string{"healthy", "unrecorded policy", "changed policy", "custom weak document", "hash transport error"} {
		t.Run(mode, func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[0])
			for i := range ev.journal.Entries {
				ev.journal.Entries[i].At = time.Now().Add(-time.Second)
			}
			policy, err := os.ReadFile(filepath.Join(manifest.Dir(repoRoot(), euPackageID), "firewall", "weak.json"))
			if err != nil {
				t.Fatal(err)
			}
			hash, err := containd.FirewallHashFromBytes(policy)
			if err != nil {
				t.Fatal(err)
			}
			configReads := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("validator changed range: %s", r.Method)
				}
				var body any
				switch r.URL.Path {
				case "/api/v1/config":
					configReads++
					if mode == "hash transport error" && configReads == 1 {
						http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
						return
					}
					if mode == "changed policy" && configReads > 1 {
						fmt.Fprint(w, `{"firewall":{"defaultAction":"ALLOW","rules":[]}}`)
					} else {
						w.Write(policy)
					}
					return
				case "/api/v1/pcap/list":
					fmt.Fprint(w, "[]")
					return
				case "/breaker-1/api/state":
					body = ev.devices["breaker-1"]
				case "/process/api/electrical":
					ev.process.SolvedAt = time.Now().UTC()
					body = ev.process
				case "/cc-1/api/state":
					for i := range ev.cc.Points {
						ev.cc.Points[i].ReceivedAt = time.Now().UTC()
					}
					body = ev.cc
				case "/cc-1/api/journal":
					body = ev.journal
				default:
					t.Errorf("unexpected upstream (no US aggregate permitted): %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(body); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			base := multiDeviceGeneration(t, upstream.URL)
			gen := lifecycle.NewGeneration(base.ID, base.Package, base.Manifest, containd.NewClient(upstream.URL), base.StartedAt)
			s := rangeServer(gen, "weak")
			s.lastAppliedHash = hash
			s.pcap.FileReady = true
			s.orchestrator = &orchestrator.Orchestrator{}
			if mode == "unrecorded policy" {
				s.lastAppliedHash = ""
			}
			if mode == "custom weak document" {
				s.activeConfig = "custom"
			}
			collected := s.collectEUWorkshop(t.Context(), gen, euWorkshopSlots[0])
			if mode == "hash transport error" && (collected.policyErr == nil || !strings.Contains(collected.policyErr.Error(), "503") || strings.Contains(collected.policyErr.Error(), "identity mismatch")) {
				t.Fatalf("lost transport error: %v", collected.policyErr)
			}
			checks := evaluateEUWorkshop(euWorkshopSlots[0], collected)
			pass := true
			for _, check := range checks {
				pass = pass && check.Status == "pass"
			}
			if pass != (mode == "healthy") || configReads != 2 {
				t.Fatalf("checks=%+v configReads=%d", checks, configReads)
			}
		})
	}
}

func TestEUCCQualityPresence(t *testing.T) {
	for _, ioa := range []int{1001, 1301, 2001} {
		for _, key := range []string{"quality", "iv", "nt", "sb", "bl", "ov"} {
			for _, null := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%s/null=%t", ioa, key, null), func(t *testing.T) {
					q := map[string]any{"iv": false, "nt": false, "sb": false, "bl": false, "ov": false}
					point := map[string]any{"ioa": ioa, "quality": q}
					if key == "quality" {
						delete(point, key)
						if null {
							point[key] = nil
						}
					} else {
						delete(q, key)
						if null {
							q[key] = nil
						}
					}
					data, _ := json.Marshal(map[string]any{"points": []any{point}})
					var state euPresenceState
					err := json.Unmarshal(data, &state)
					legitimateDigitalOV := key == "ov" && null && ioa == 1001
					if (err == nil) != legitimateDigitalOV {
						t.Fatalf("err=%v data=%s", err, data)
					}
				})
			}
		}
	}
	data, _ := json.Marshal(passingEUWorkshop(t, euWorkshopSlots[0]).cc)
	var state euPresenceState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
}
