package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestPolicyMismatchMessages(t *testing.T) {
	for _, pkg := range []string{usPackageID, euPackageID} {
		t.Run(pkg, func(t *testing.T) {
			running := []byte(`{"firewall":{"defaultAction":"ALLOW","rules":[]}}`)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(running)
			}))
			defer upstream.Close()
			gen := usGeneration(containd.NewClient(upstream.URL))
			if pkg == euPackageID {
				gen = euGeneration(t, containd.NewClient(upstream.URL), nil)
			}
			s := rangeServer(gen, "custom")
			s.policySource = "manual-custom"
			var err error
			s.lastAppliedHash, err = containd.FirewallHashFromBytes(running)
			if err != nil {
				t.Fatal(err)
			}
			all := []string{}
			for _, action := range gen.Package.SegmentationPlan().Catalog {
				all = append(all, action.ID)
			}
			plan, err := labs.CompilePlan(gen.Package, gen.Package.Course.Features.SegmentationPlan.Model, all)
			if err != nil {
				t.Fatal(err)
			}
			var denied []labs.PlanExpectation
			for _, e := range plan.Expectations {
				if e.PlanVerdict == labs.VerdictDeny && !e.Bridged && len(e.Owners) > 0 {
					denied = append(denied, e)
				}
			}
			if len(denied) < 3 {
				t.Fatal("fixture needs several distinct owned denied paths")
			}
			owned := denied[0]
			title := ""
			for _, action := range gen.Package.SegmentationPlan().Catalog {
				if action.ID == owned.Owners[0] {
					title = action.Title
				}
			}
			for _, count := range []int{1, 3, 8, 11} {
				t.Run(fmt.Sprint(count), func(t *testing.T) {
					target := plan
					target.Expectations = make([]labs.PlanExpectation, count)
					for i := range target.Expectations {
						target.Expectations[i] = denied[i%len(denied)]
					}
					ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{target})
					check := s.policyJudgment(ctx, gen)
					if check.Status != "fail" || !strings.Contains(check.Detail, fmt.Sprintf("%d policy mismatches:", count)) ||
						strings.Count(check.Detail, ": running allow; judging target deny") != min(count, 8) || title == "" || !strings.Contains(check.Detail, title) ||
						strings.Contains(check.Detail, "tuple "+owned.ID) || !strings.Contains(check.Detail, "running allow; judging target deny") {
						t.Fatal(check)
					}
					for _, e := range target.Expectations[:min(count, 8)] {
						if !strings.Contains(check.Detail, fmt.Sprintf("%s -> %s:%d (", e.From, e.To, e.Port)) {
							t.Fatalf("missing path %s: %+v", e.ID, check)
						}
					}
					if strings.Contains(check.Detail, "more not shown") != (count > 8) ||
						count > 8 && !strings.Contains(check.Detail, "3 more not shown") {
						t.Fatal(check)
					}
				})
			}
			// Required conduits have no owning remediation action.
			for _, e := range plan.Expectations {
				if len(e.Owners) == 0 && !e.Bridged && e.PlanVerdict == labs.VerdictAllow {
					running = []byte(`{"firewall":{"defaultAction":"DENY","rules":[]}}`)
					s.lastAppliedHash, err = containd.FirewallHashFromBytes(running)
					if err != nil {
						t.Fatal(err)
					}
					plan.Expectations = []labs.PlanExpectation{e}
					ctx := context.WithValue(t.Context(), changeRequestKey{}, compiledChangeRequest{plan})
					check := s.policyJudgment(ctx, gen)
					if check.Status != "fail" || !strings.Contains(check.Detail, "(required conduit)") {
						t.Fatal(check)
					}
					return
				}
			}
			t.Fatal("fixture needs a required conduit")
		})
	}
}

func TestEUValidatorDetailsNeverPrintNil(t *testing.T) {
	ev := passingEUWorkshop(t, euWorkshopSlots[0])
	ev.policy = "custom"
	ev.devices["breaker-1"] = nil
	ev.process.Buses["bus20"] = ev.process.Buses["missing"]
	ev.negativeExpected++
	checks := []ValidationCheck{
		euWorkshopPolicy(ev, true),
		euWorkshopDeviceCheck(ev, "breaker-1", "Breaker closed", func(*euWorkshopDevice) bool { return true }),
		euWorkshopSupply(ev),
		euWorkshopNegative(ev),
		conduitPLC(conduitEvidence{}, ev.start),
		conduitFeed(conduitEvidence{}, ev.start),
		euNoForeignBreaker(euWorkshopEvidence{}),
	}
	_, window := conduitWindow(conduitEvidence{})
	checks = append(checks, window)
	// A fresh measured loss of supply is different from missing observations.
	ev = passingEUWorkshop(t, euWorkshopSlots[0])
	bus := ev.process.Buses["bus20"]
	live := false
	bus.Live = &live
	ev.process.Buses["bus20"] = bus
	supply := euWorkshopSupply(ev)
	if !strings.Contains(supply.Detail, "supply absent") || !strings.Contains(supply.Detail, "restore") {
		t.Fatal(supply)
	}
	checks = append(checks, supply)
	ev.processErr = errors.New("solver unavailable")
	checks = append(checks, euWorkshopSupply(ev))
	for _, check := range checks {
		if check.Status != "fail" || strings.Contains(check.Detail, "<nil>") || strings.HasSuffix(check.Detail, "; ") {
			t.Fatal(check)
		}
	}
	if !strings.Contains(checks[len(checks)-1].Detail, "solver unavailable") {
		t.Fatal("real error was lost")
	}
}

func TestIsolationProbeFailureDoesNotPrintNil(t *testing.T) {
	gen := multiDeviceGeneration(t, "http://127.0.0.1:1")
	s := rangeServer(gen, "custom")
	s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
		return "", "python3 unavailable", 127, nil
	}
	check := s.conduitIsolation(t.Context(), gen, "eng-ws-1")
	if check.Status != "fail" || strings.Contains(check.Detail, "<nil>") || !strings.Contains(check.Detail, "python3 unavailable") {
		t.Fatal(check)
	}
}

func TestMissingEvidencePrefix(t *testing.T) {
	for _, text := range []string{"no GI", "missing evidence: no GI", "Missing evidence: missing evidence: no GI"} {
		if got := missingEvidenceDetail(errors.New(text)); got != "Missing evidence: no GI" {
			t.Fatal(got)
		}
	}
}
