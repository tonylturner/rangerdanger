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
			expect := map[string]any{"breaker_closed": true, "supply_present": true, "cc_link_fresh": true, "rmu_normal": true, "oltc_auto": true, "firewall_config": "improved"}
			results := s.executeCheck(t.Context(), gen, expect)
			if len(results) != 6 {
				t.Fatalf("results=%+v", results)
			}
			pass := true
			for _, result := range results {
				pass = pass && result.Success
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
			for _, expect := range []map[string]any{{"typo": true}, {"breaker_closed": "true"}, {}} {
				for _, r := range s.executeCheck(t.Context(), gen, expect) {
					if r.Success {
						t.Fatalf("invalid check passed: %+v", r)
					}
				}
			}
		})
	}
}

func TestEULinkDoesNotExpireHealthyConnections(t *testing.T) {
	ev := passingEUWorkshop(t, euWorkshopSlots[0])
	ev.now = ev.now.Add(time.Hour)
	for i := range ev.cc.Points {
		ev.cc.Points[i].ReceivedAt = ev.now.Add(-time.Second)
	}
	if got := euLink(ev.cc, ev.journal, ev.start, ev.now); got.Status != "pass" {
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

func TestEUCheckOnlyPolicyDoesNotFetchDevices(t *testing.T) {
	s := rangeServer(euGeneration(t, containd.NewClient(closedURL(t)), nil), "custom")
	got := s.executeCheck(t.Context(), s.rng.(*fakeRange).gen, map[string]any{"firewall_config": "improved"})
	if len(got) != 1 || !got[0].Success {
		t.Fatal(fmt.Sprint(got))
	}
}

func TestEUWorkshopCollectorUsesLivePolicyAndEUOnlySources(t *testing.T) {
	for _, mode := range []string{"healthy", "unrecorded policy", "changed policy", "custom weak document"} {
		t.Run(mode, func(t *testing.T) {
			ev := passingEUWorkshop(t, euWorkshopSlots[0])
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
			checks := euBaselineValidator.run(t.Context(), s, validatorInput{gen: gen})
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
