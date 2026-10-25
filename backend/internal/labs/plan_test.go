package labs

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEvaluatePlanRules(t *testing.T) {
	predicate := &PlanICS{Protocol: "modbus", FunctionCode: []int{1}}
	base := PlanRule{
		ID: "pinned", SourceZones: []string{"lan1"}, DestZones: []string{"lan2"},
		Sources: []string{"192.0.2.20/32"}, Destinations: []string{"198.51.100.0/24"},
		Protocols: []PlanProtocol{{Name: "tcp", Port: "500-502"}}, Action: "ALLOW", ICS: predicate, Log: true,
	}
	tuple := PlanTuple{"lan1", "lan2", "192.0.2.20", "198.51.100.10", "tcp", 502}
	for _, tc := range []struct {
		name   string
		change func(*PlanRule, *PlanTuple)
		want   Verdict
	}{
		{"all selectors", func(*PlanRule, *PlanTuple) {}, VerdictAllow},
		{"wrong source zone", func(_ *PlanRule, p *PlanTuple) { p.SrcZone = "wan" }, VerdictDeny},
		{"wrong destination zone", func(_ *PlanRule, p *PlanTuple) { p.DstZone = "dmz" }, VerdictDeny},
		{"wrong source host", func(_ *PlanRule, p *PlanTuple) { p.SrcIP = "192.0.2.21" }, VerdictDeny},
		{"wrong destination subnet", func(_ *PlanRule, p *PlanTuple) { p.DstIP = "198.51.101.10" }, VerdictDeny},
		{"wrong protocol", func(_ *PlanRule, p *PlanTuple) { p.Protocol = "udp" }, VerdictDeny},
		{"range low", func(_ *PlanRule, p *PlanTuple) { p.Port = 500 }, VerdictAllow},
		{"range inside", func(_ *PlanRule, p *PlanTuple) { p.Port = 501 }, VerdictAllow},
		{"below range", func(_ *PlanRule, p *PlanTuple) { p.Port = 499 }, VerdictDeny},
		{"above range", func(_ *PlanRule, p *PlanTuple) { p.Port = 503 }, VerdictDeny},
		{"bare IP", func(r *PlanRule, _ *PlanTuple) { r.Sources = []string{"192.0.2.20"} }, VerdictAllow},
		{"OR addresses", func(r *PlanRule, _ *PlanTuple) { r.Sources = []string{"203.0.113.2", "192.0.2.20"} }, VerdictAllow},
		{"OR zones", func(r *PlanRule, _ *PlanTuple) { r.SourceZones = []string{"wan", "lan1"} }, VerdictAllow},
		{"OR protocols", func(r *PlanRule, _ *PlanTuple) {
			r.Protocols = []PlanProtocol{{Name: "udp", Port: "123"}, {Name: "tcp", Port: "502"}}
		}, VerdictAllow},
		{"any port", func(r *PlanRule, _ *PlanTuple) { r.Protocols = []PlanProtocol{{Name: "tcp"}} }, VerdictAllow},
		{"empty selectors", func(r *PlanRule, _ *PlanTuple) { *r = PlanRule{ID: "any", Action: "ALLOW"} }, VerdictAllow},
		{"logging is metadata", func(r *PlanRule, _ *PlanTuple) { r.Log = false }, VerdictAllow},
		{"ICS is metadata", func(r *PlanRule, _ *PlanTuple) { r.ICS = nil }, VerdictAllow},
		{"invalid port fails closed", func(r *PlanRule, _ *PlanTuple) { r.Protocols = []PlanProtocol{{Name: "tcp", Port: "bad"}} }, VerdictDeny},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule, p := base, tuple
			tc.change(&rule, &p)
			if got := Evaluate([]PlanRule{rule}, "DENY", p); got != tc.want {
				t.Fatalf("Evaluate = %s, want %s", got, tc.want)
			}
		})
	}
	deny := PlanRule{ID: "first", Action: "DENY"}
	if Evaluate([]PlanRule{deny, base}, "ALLOW", tuple) != VerdictDeny ||
		Evaluate([]PlanRule{base, deny}, "DENY", tuple) != VerdictAllow {
		t.Fatal("first matching rule must win")
	}
	if Evaluate(nil, "ALLOW", tuple) != VerdictAllow || Evaluate(nil, "DENY", tuple) != VerdictDeny {
		t.Fatal("default action is not respected")
	}
}

func TestPlanRouteResolution(t *testing.T) {
	pkg := planPackage(t, "us-dnp3-substation")
	for _, tc := range []struct {
		id      string
		bridged bool
	}{
		{"A1", false}, {"A2", false}, {"A3", false},
		{"A4", true}, {"A5", true}, {"A6", true}, {"U10", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			i := slices.IndexFunc(pkg.Curriculum.Plan.Proof, func(p PlanProof) bool { return p.ID == tc.id })
			route, err := ResolvePlanTuple(pkg, pkg.Curriculum.Plan.Proof[i])
			if err != nil || route.Bridged != tc.bridged || route.SourceNetwork != "ot_ops_net" {
				t.Fatalf("route = %+v, %v", route, err)
			}
			if strings.HasPrefix(tc.id, "A") && tc.id != "A5" && tc.id != "A6" && route.Tuple.SrcIP != "10.30.30.20" {
				t.Fatal("RTAC's field attachment must not replace its wire-visible source")
			}
		})
	}
}

func TestCompilePlanSelectionsAndOwnership(t *testing.T) {
	pkg := planPackage(t, "us-dnp3-substation")
	before, err := json.Marshal(pkg.Curriculum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompilePlan(pkg, pkg.Curriculum, []string{"not-an-action"}); err == nil {
		t.Fatal("unknown action accepted")
	}
	none := mustCompilePlan(t, pkg, nil)
	if none.Label != "weak" || mustCompilePlan(t, pkg, []string{"positive-validation", "tighten-firewall-objects"}).Label != "weak" {
		t.Fatal("empty or planning-only plan must be Weak")
	}
	if mustCompilePlan(t, pkg, []string{"improve-logging"}).Label != "partial" {
		t.Fatal("detect-only selection must be Partial")
	}
	ids := []string{}
	for _, action := range pkg.Curriculum.Catalog {
		ids = append(ids, action.ID)
	}
	all := mustCompilePlan(t, pkg, ids)
	reversed := slices.Clone(ids)
	slices.Reverse(reversed)
	reversed = append(reversed, reversed...)
	if got := mustCompilePlan(t, pkg, reversed); !reflect.DeepEqual(got, all) || got.Label != "hardened" {
		t.Fatal("canonicalization must ignore input order and duplicates")
	}
	for i, action := range all.Actions {
		if action.ID != pkg.Curriculum.Catalog[i].ID || !action.Chosen {
			t.Fatal("actions must retain catalog order and chosen flags")
		}
	}
	for _, entry := range none.Rules {
		if entry.Origin.Kind == "selected" || entry.Origin.Kind == "required" && entry.Origin.Action != "" {
			t.Fatalf("incorrect empty-plan origin: %+v", entry)
		}
	}
	for _, entry := range all.Rules {
		if entry.Origin.Kind == "deferred" || entry.Origin.Kind == "selected" && entry.Origin.Action == "" {
			t.Fatalf("incorrect full-plan origin: %+v", entry)
		}
	}
	logOnly := mustCompilePlan(t, pkg, []string{"improve-logging"})
	for _, entry := range logOnly.Rules {
		if entry.Origin.Kind == "deferred" && entry.Rule.Log {
			t.Fatalf("logging targets absent selected rules, not deferred substitutes: %+v", entry)
		}
	}
	// Editing every nested kind of output must leave future compilation intact.
	snapshot, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	all.Rules[0].Rule.Protocols[0].Port = "9999"
	all.Rules[5].Rule.Sources[0] = "192.0.2.2/32"
	all.Rules[5].Rule.ICS.FunctionCode[0] = 255
	all.Expectations[8].Owners[0] = "changed"
	all.Config[0] = '!'
	fresh, err := json.Marshal(mustCompilePlan(t, pkg, ids))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(pkg.Curriculum)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("compilation aliases mutable curriculum data")
	}
	if string(fresh) != string(snapshot) {
		t.Fatalf("compilation aliases mutable result data:\n%s\nwant:\n%s", fresh, snapshot)
	}
	// Resource feasibility is advisory; a technically valid plan still compiles.
	pkg.Curriculum.BudgetHours = 1
	if _, err := CompilePlan(pkg, pkg.Curriculum, ids); err != nil {
		t.Fatalf("budget overrun rejected: %v", err)
	}
}

func TestPlanRequiredPredicateIndependentOfLogging(t *testing.T) {
	pkg := planPackage(t, "us-dnp3-substation")
	const id = "rtac-to-field-http"
	for action, detect := range pkg.Curriculum.Plan.Detect {
		if _, exists := detect.ICS[id]; exists {
			t.Fatalf("%s owns the required HTTP rule's predicate", action)
		}
	}
	httpRule := func(actions []string) PlanRule {
		t.Helper()
		for _, entry := range mustCompilePlan(t, pkg, actions).Rules {
			if entry.Rule.ID == id {
				if entry.Origin.Kind != "required" {
					t.Fatalf("HTTP rule origin = %+v, want required", entry.Origin)
				}
				return entry.Rule
			}
		}
		t.Fatal("required HTTP rule missing")
		return PlanRule{}
	}
	base, logged := httpRule(nil), httpRule([]string{"improve-logging"})
	if !reflect.DeepEqual(base.ICS, &PlanICS{}) {
		t.Fatalf("required HTTP predicate = %+v, want empty nonnil predicate", base.ICS)
	}
	if base.Log || !logged.Log {
		t.Fatal("logging selection must only enable logging on the required HTTP rule")
	}
	logged.Log = false
	if !reflect.DeepEqual(base, logged) {
		t.Fatal("logging selection changed the required HTTP rule beyond its log flag")
	}
}

func TestPlanDoesNotReadLegacyCompilerFields(t *testing.T) {
	pkg := planPackage(t, "eu-iec104-substation")
	want := mustCompilePlan(t, pkg, nil)
	pkg.Curriculum.Policy = nil
	pkg.Curriculum.Groups = nil
	pkg.Curriculum.PlanningOnly = nil
	pkg.Curriculum.BaselineRules = nil
	pkg.Curriculum.EnterpriseToVendorRule = nil
	pkg.Curriculum.Flags = CurriculumFlags{}
	pkg.Curriculum.Tests = nil
	pkg.Curriculum.Interfaces = nil
	pkg.Curriculum.Zones = nil
	if got := mustCompilePlan(t, pkg, nil); !reflect.DeepEqual(got, want) {
		t.Fatal("legacy fields influence shared plan compilation")
	}
}

func TestPlanProofLabSelections(t *testing.T) {
	pkg := planPackage(t, "eu-iec104-substation")
	want := map[string][]string{
		"implementation": {
			"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8", "U9", "U10",
			"U11", "U12", "U13", "U14", "U15", "U16", "U17", "U18", "U19",
		},
		"hardening": {"U1"},
		"vendor":    {"U3", "U4", "U6", "U9", "U12", "U15", "U18"},
	}
	for role, ids := range want {
		got := []string{}
		for _, proof := range pkg.Curriculum.Plan.Proof {
			if slices.Contains(proof.Labs, role) {
				got = append(got, proof.ID)
			}
		}
		if !slices.Equal(got, ids) {
			t.Errorf("%s proofs = %v, want preserved selections %v", role, got, ids)
		}
	}
}
