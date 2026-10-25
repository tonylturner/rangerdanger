package labs

import (
	"reflect"
	"slices"
	"testing"
)

const vendorOTDenyID = "deny-vendor-to-ot"

func withoutVendorOTDeny(rules []PlanRule) []PlanRule {
	return slices.DeleteFunc(slices.Clone(rules), func(rule PlanRule) bool {
		return rule.ID == vendorOTDenyID
	})
}

func TestUSVendorOTDenyLoggingSelections(t *testing.T) {
	pkg := planPackage(t, "us-dnp3-substation")
	weak, _, _ := planPolicy(t, pkg, "weak")
	weakIndex := slices.IndexFunc(weak, func(rule PlanRule) bool { return rule.ID == "vendor-to-ot-weak" })
	if weakIndex < 0 {
		t.Fatal("Weak vendor exposure missing")
	}
	ports := []PlanProtocol{}
	for _, protocol := range weak[weakIndex].Protocols {
		if protocol.Port != "443" && protocol.Port != "22" {
			ports = append(ports, protocol)
		}
	}
	wantPorts := []PlanProtocol{{Name: "tcp", Port: "8080"}, {Name: "tcp", Port: "1881"}, {Name: "tcp", Port: "502"}, {Name: "tcp", Port: "20000"}}
	if !reflect.DeepEqual(ports, wantPorts) {
		t.Fatalf("Weak non-management protocols = %+v, want %+v", ports, wantPorts)
	}
	for _, tc := range []struct {
		name              string
		restrict, logging bool
	}{
		{"deferred", false, false},
		{"logging-only", false, true},
		{"restriction-only", true, false},
		{"restriction-and-logging", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := []string{}
			if tc.restrict {
				actions = append(actions, "restrict-vendor-to-ot")
			}
			if tc.logging {
				actions = append(actions, "improve-logging")
			}
			compiled := mustCompilePlan(t, pkg, actions)
			index := slices.IndexFunc(compiled.Rules, func(entry CompiledPlanRule) bool { return entry.Rule.ID == vendorOTDenyID })
			if !tc.restrict {
				if index >= 0 {
					t.Fatal("deferred restriction emitted a deny")
				}
				exposure := slices.IndexFunc(compiled.Rules, func(entry CompiledPlanRule) bool { return entry.Rule.ID == "vendor-to-ot-weak" })
				if exposure < 0 || !reflect.DeepEqual(compiled.Rules[exposure].Rule, weak[weakIndex]) {
					t.Fatal("deferred Weak exposure changed")
				}
				return
			}
			if index <= 0 || compiled.Rules[index-1].Rule.ID != "vendor-to-ot-restricted" {
				t.Fatal("deny must immediately follow the management allow")
			}
			entry := compiled.Rules[index]
			want := PlanRule{
				ID:          vendorOTDenyID,
				Description: "BLOCK: Vendor cannot reach OT Operations on Modbus, DNP3, or HMI control ports",
				SourceZones: []string{"dmz"}, DestZones: []string{"lan1"},
				Protocols: ports, ICS: &PlanICS{}, Action: "DENY", Log: tc.logging,
			}
			if !reflect.DeepEqual(entry.Rule, want) || entry.Origin != (PlanOrigin{Kind: "selected", Action: "restrict-vendor-to-ot"}) {
				t.Fatalf("deny = %+v, want %+v with selected restriction origin", entry, want)
			}
			if compiled.Rules[index-1].Rule.Log != tc.logging {
				t.Fatal("management allow logging must follow improve-logging")
			}
		})
	}
}

func TestUSVendorOTDenyPreservesVerdicts(t *testing.T) {
	pkg := planPackage(t, "us-dnp3-substation")
	hardened, defaultAction, reference := planPolicy(t, pkg, "improved")
	denyIndex := slices.IndexFunc(hardened, func(rule PlanRule) bool { return rule.ID == vendorOTDenyID })
	if denyIndex <= 0 || hardened[denyIndex-1].ID != "vendor-to-ot-restricted" || !hardened[denyIndex].Log {
		t.Fatal("Hardened must log the deny immediately after its management allow")
	}
	all := []string{}
	for _, action := range pkg.Curriculum.Catalog {
		all = append(all, action.ID)
	}
	for _, tc := range []struct {
		name    string
		actions []string
	}{
		{"empty", nil},
		{"partial", []string{"restrict-enterprise-to-vendor"}},
		{"restricted-partial", []string{"restrict-vendor-to-ot"}},
		{"full", all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := mustCompilePlan(t, pkg, tc.actions)
			rules := []PlanRule{}
			for _, entry := range compiled.Rules {
				rules = append(rules, entry.Rule)
			}
			auth, unauth := 0, 0
			for i, proof := range pkg.Curriculum.Plan.Proof {
				route, err := ResolvePlanTuple(pkg, proof)
				if err != nil {
					t.Fatal(err)
				}
				row := compiled.Expectations[i]
				beforePlan := evaluatePlanRoute(withoutVendorOTDeny(rules), defaultAction, route)
				beforeHardened := evaluatePlanRoute(withoutVendorOTDeny(hardened), defaultAction, route)
				if row.PlanVerdict != beforePlan || row.HardenedVerdict != beforeHardened {
					t.Errorf("%s verdict changed: plan %s -> %s, Hardened %s -> %s", row.ID, beforePlan, row.PlanVerdict, beforeHardened, row.HardenedVerdict)
				}
				if proof.Category == "authorized" {
					auth++
				} else {
					unauth++
				}
			}
			if auth != 8 || unauth != 13 {
				t.Fatalf("proof counts = %d + %d, want 8 + 13", auth, unauth)
			}
			// The new rule changes metadata, not the management envelope.
			if !reflect.DeepEqual(planEnvelopeWithoutRules(t, compiled.Config), planEnvelopeWithoutRules(t, reference)) {
				t.Fatal("management envelope changed")
			}
			if err := CheckManagementSafety(compiled.Config); err != nil {
				t.Fatal(err)
			}
			// Cover all newly explicit ports, retained management ports and
			// unlisted TCP/UDP traffic, beyond the finite proof universe.
			for _, protocol := range []string{"tcp", "udp"} {
				for _, port := range []int{22, 443, 502, 1881, 8080, 20000, 12345} {
					tuple := PlanTuple{"dmz", "lan1", "10.20.20.10", "10.30.30.20", protocol, port}
					for _, policy := range [][]PlanRule{rules, hardened} {
						before := Evaluate(withoutVendorOTDeny(policy), defaultAction, tuple)
						if got := Evaluate(policy, defaultAction, tuple); got != before {
							t.Errorf("%s/%d changed verdict %s -> %s", protocol, port, before, got)
						}
					}
				}
			}
		})
	}
}
