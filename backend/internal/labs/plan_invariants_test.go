package labs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func planPackage(t *testing.T, id string) Package {
	t.Helper()
	definitions := filepath.Join("..", "..", "..", "lab-definitions")
	pkg, err := ReadDefinition(definitions, filepath.Join(definitions, "packages", id, "package.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func planPolicy(t *testing.T, pkg Package, name string) ([]PlanRule, string, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "lab-definitions", pkg.Policies[name]))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Firewall struct {
			DefaultAction string          `json:"defaultAction"`
			Rules         json.RawMessage `json:"rules"`
		} `json:"firewall"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	rules, err := decodePlanRules(document.Firewall.Rules)
	if err != nil {
		t.Fatal(err)
	}
	return rules, document.Firewall.DefaultAction, data
}

func mustCompilePlan(t *testing.T, pkg Package, actions []string) CompiledPlan {
	t.Helper()
	compiled, err := CompilePlan(pkg, pkg.Curriculum, actions)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestPlanInvariants(t *testing.T) {
	for _, tc := range []struct {
		id                    string
		authorized, negatives int
		bits                  int
	}{
		{"us-dnp3-substation", 8, 13, 9},
		{"eu-iec104-substation", 10, 27, 7},
	} {
		t.Run(tc.id, func(t *testing.T) {
			pkg := planPackage(t, tc.id)
			c := pkg.Curriculum
			kinds, err := validatePlan(pkg, c)
			if err != nil {
				t.Fatal(err)
			}
			var technical []string
			for _, action := range c.Catalog {
				if kinds[action.ID] != PlanActionOnly {
					technical = append(technical, action.ID)
				}
			}
			if len(technical) != tc.bits {
				t.Fatalf("technical actions = %d, want %d", len(technical), tc.bits)
			}
			hardened, hardenedDefault, reference := planPolicy(t, pkg, "improved")
			weak, weakDefault, _ := planPolicy(t, pkg, "weak")
			all := mustCompilePlan(t, pkg, technical)
			none := mustCompilePlan(t, pkg, nil)
			t.Run("C1", func(t *testing.T) {
				var config struct {
					Firewall struct {
						Rules []PlanRule `json:"rules"`
					} `json:"firewall"`
				}
				if err := json.Unmarshal(all.Config, &config); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(config.Firewall.Rules, hardened) {
					t.Fatalf("full plan does not exactly equal ordered reference rules:\n%+v\nwant %+v", config.Firewall.Rules, hardened)
				}
				// Every other field, including firewall defaultAction and NAT,
				// remains the exact decoded reference envelope.
				if !reflect.DeepEqual(planEnvelopeWithoutRules(t, all.Config), planEnvelopeWithoutRules(t, reference)) {
					t.Fatal("compilation changed the non-rule envelope")
				}
			})
			t.Run("C2", func(t *testing.T) {
				for i, proof := range c.Plan.Proof {
					route, err := ResolvePlanTuple(pkg, proof)
					if err != nil {
						t.Fatal(err)
					}
					if want := evaluatePlanRoute(weak, weakDefault, route); none.Expectations[i].PlanVerdict != want {
						t.Errorf("%s: empty plan = %s; Weak = %s", proof.ID, none.Expectations[i].PlanVerdict, want)
					}
				}
			})
			t.Run("C4-reference", func(t *testing.T) {
				auth, negatives := 0, 0
				for _, proof := range c.Plan.Proof {
					route, err := ResolvePlanTuple(pkg, proof)
					if err != nil {
						t.Fatal(err)
					}
					if want := evaluatePlanRoute(hardened, hardenedDefault, route); proof.Expect != want {
						t.Errorf("%s: authored expect %s != reference %s", proof.ID, proof.Expect, want)
					}
					if proof.Category == "authorized" {
						auth++
					} else {
						negatives++
					}
				}
				if auth != tc.authorized || negatives != tc.negatives {
					t.Fatalf("proof counts = %d + %d, want %d + %d", auth, negatives, tc.authorized, tc.negatives)
				}
			})
			t.Run("C6", func(t *testing.T) {
				assertPlanSourcesExecCapable(t, pkg)
				for _, action := range technical {
					if kinds[action] != PlanActionEnforce {
						continue
					}
					owns := false
					for i, proof := range c.Plan.Proof {
						if slices.Contains(proof.Owners, action) && !all.Expectations[i].Bridged {
							owns = true
						}
					}
					if !owns {
						t.Errorf("%s owns no non-bridged proof", action)
					}
				}
			})
			// Enumerate every enforce/detect subset once. Comparison of every
			// adjacent pair then proves locality over this proof universe only.
			results := make([][]PlanExpectation, 1<<len(technical))
			t.Run("C4-C5-subsets", func(t *testing.T) {
				for mask := range results {
					t.Run(fmt.Sprintf("%03x", mask), func(t *testing.T) {
						ids := []string{}
						for bit, id := range technical {
							if mask&(1<<bit) != 0 {
								ids = append(ids, id)
							}
						}
						compiled := mustCompilePlan(t, pkg, ids)
						results[mask] = compiled.Expectations
						for _, row := range compiled.Expectations {
							if row.Category == "authorized" && row.PlanVerdict != VerdictAllow {
								t.Errorf("%s: authorized tuple denied", row.ID)
							}
						}
						for _, entry := range compiled.Rules {
							data, err := json.Marshal([]PlanRule{entry.Rule})
							if err != nil {
								t.Fatal(err)
							}
							decoded, err := decodePlanRules(data)
							if err != nil || !reflect.DeepEqual(decoded[0], entry.Rule) {
								t.Fatalf("rule %s strict round trip: %v", entry.Rule.ID, err)
							}
						}
						if err := LintPolicy("subset", compiled.Config, pkg.ICSRules); err != nil {
							t.Fatal(err)
						}
						if err := CheckManagementSafety(compiled.Config); err != nil {
							t.Fatal(err)
						}
					})
				}
			})
			t.Run("C3-C4-locality", func(t *testing.T) {
				witnesses := map[string]map[string]bool{}
				for mask, before := range results {
					if before == nil {
						t.Fatalf("subset %x was not compiled", mask)
					}
					for bit, action := range technical {
						if mask&(1<<bit) != 0 {
							continue
						}
						after := results[mask|(1<<bit)]
						for i, row := range before {
							if row.PlanVerdict == after[i].PlanVerdict {
								continue
							}
							if kinds[action] == PlanActionDetect {
								t.Errorf("detect %s changes %s in subset %x", action, row.ID, mask)
							}
							if !slices.Contains(row.Owners, action) || row.Bridged {
								t.Errorf("%s changes unowned %s in subset %x", action, row.ID, mask)
							}
							if witnesses[action] == nil {
								witnesses[action] = map[string]bool{}
							}
							witnesses[action][row.ID] = true
						}
					}
				}
				for _, proof := range c.Plan.Proof {
					for _, owner := range proof.Owners {
						if !witnesses[owner][proof.ID] {
							t.Errorf("%s claims %s without a verdict-change witness", proof.ID, owner)
						}
					}
				}
			})
		})
	}
}

func planEnvelopeWithoutRules(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	delete(document["firewall"].(map[string]any), "rules")
	return document
}

func assertPlanSourcesExecCapable(t *testing.T, pkg Package) {
	t.Helper()
	// These runtime images provide bash (/dev/tcp), except control_centre
	// and iec104_rtu, whose Alpine runtime provides BusyBox nc. The sim-base
	// explicitly installs bash (services/Dockerfile); GPS uses sim-base too.
	// This is a static image contract, not a claim of live probe receipts.
	probers := map[string]bool{
		"kali_pentest": true, "vendor_jumpbox": true, "eng_workstation": true,
		"rtac_sim": true, "fuxa_hmi": true, "historian_sim": true,
		"openplc": true, "gps_sim": true, "control_centre": true, "iec104_rtu": true,
	}
	for _, proof := range pkg.Curriculum.Plan.Proof {
		found := false
		for _, node := range pkg.Template.Nodes {
			if node.ID == proof.From {
				found = probers[node.Type] && node.Container != "" && node.Name != ""
			}
		}
		if !found {
			t.Errorf("%s: source %s has no known exec-capable probe image", proof.ID, proof.From)
		}
	}
}

func TestPlanDeferredScopesMatchWeak(t *testing.T) {
	for _, id := range []string{"us-dnp3-substation", "eu-iec104-substation"} {
		t.Run(id, func(t *testing.T) {
			pkg := planPackage(t, id)
			weak, _, _ := planPolicy(t, pkg, "weak")
			byID := map[string]PlanRule{}
			for _, rule := range weak {
				byID[rule.ID] = rule
			}
			gotZones := map[string][]string{}
			for _, slot := range pkg.Curriculum.Plan.Rules {
				for _, rule := range slot.Deferred {
					original := rule.ID
					if strings.HasPrefix(id, "eu-") {
						for _, suffix := range []string{"-enterprise", "-vendor", "-control-centre"} {
							original = strings.TrimSuffix(original, suffix)
						}
					}
					reference, exists := byID[original]
					if !exists {
						t.Fatalf("deferred rule %s has no Weak authority", rule.ID)
					}
					gotZones[original] = append(gotZones[original], rule.SourceZones...)
					// Splitting may only change the ID and source-zone list.
					rule.ID, rule.SourceZones = reference.ID, reference.SourceZones
					if !reflect.DeepEqual(rule, reference) {
						t.Errorf("deferred %s differs from Weak beyond its source split", original)
					}
				}
			}
			for _, rule := range weak {
				if id == "us-dnp3-substation" && (rule.ID == "ot-intrazone" || rule.ID == "field-intrazone") {
					continue // required equivalent OT rule; same-bridge field traffic
				}
				zones := gotZones[rule.ID]
				want := slices.Clone(rule.SourceZones)
				slices.Sort(zones)
				slices.Sort(want)
				if !slices.Equal(zones, want) {
					t.Errorf("%s splits %v, want Weak %v", rule.ID, zones, want)
				}
			}
		})
	}
}
