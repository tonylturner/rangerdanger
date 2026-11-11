package labs

import (
	"strings"
	"testing"
)

func TestDecideResetFirstMatchAndAttribution(t *testing.T) {
	reset := PlanRule{ID: "student-reset", Action: "RESET"}
	allow := PlanRule{ID: "admit", Action: "ALLOW"}
	effects := []PlanRule{{ID: "rate", Action: "RATE_LIMIT"}, {ID: "tag", Action: "TAG"}, {ID: "observe", Action: "DENY", ICS: &PlanICS{Mode: "monitor"}}}
	for _, tc := range []struct {
		name     string
		rules    []PlanRule
		fallback string
		route    PlanRoute
		want     Decision
	}{
		{"reset first", []PlanRule{reset, allow}, "ALLOW", PlanRoute{}, Decision{VerdictReset, "student-reset"}},
		{"allow first", []PlanRule{allow, reset}, "DENY", PlanRoute{}, Decision{VerdictAllow, "admit"}},
		{"effects continue to reset", append(effects, reset, allow), "DENY", PlanRoute{}, Decision{VerdictReset, "student-reset"}},
		{"bridged ignores reset", []PlanRule{reset}, "DENY", PlanRoute{Bridged: true}, Decision{Verdict: VerdictAllow}},
		{"default allow has no id", effects, "ALLOW", PlanRoute{}, Decision{Verdict: VerdictAllow}},
		{"default deny has no id", effects, "DENY", PlanRoute{}, Decision{Verdict: VerdictDeny}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideRoute(tc.rules, tc.fallback, tc.route); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAuthoredPlansRequireDenyNotReset(t *testing.T) {
	for _, pkgID := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		for _, field := range []string{"rule", "expect"} {
			t.Run(pkgID+"/"+field, func(t *testing.T) {
				pkg := planPackage(t, pkgID)
				if field == "rule" {
					for i := range pkg.Curriculum.Plan.Rules {
						slot := &pkg.Curriculum.Plan.Rules[i]
						if slot.Required != nil {
							slot.Required.Action = "RESET"
							break
						}
						if len(slot.Selected) > 0 {
							slot.Selected[0].Action = "RESET"
							break
						}
					}
				} else {
					pkg.Curriculum.Plan.Proof[0].Expect = VerdictReset
				}
				_, err := validatePlan(pkg, pkg.Curriculum)
				if err == nil || !strings.Contains(err.Error(), "invalid") {
					t.Fatalf("authored RESET accepted: %v", err)
				}
			})
		}
	}
}
