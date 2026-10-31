package labs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func boolPointer(value bool) *bool { return &value }

func TestPlanIECSelectors(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"all", `"typeIds":[47],"classes":["control"],"ca":[0,65534],"ioa":[{"start":0,"end":16777215}],"cot":[1,63],"originators":[0,255],"negative":false,"test":false,"selectExecute":"execute","commandStates":[1,2],"commandQualifiers":[0,31]`, true},
		{"type bounds", `"typeIds":[1,255]`, true},
		{"single state", `"typeIds":[45,58],"commandStates":[0,1]`, true},
		{"double state", `"typeIds":[46,47,59,60],"commandStates":[1,2]`, true},
		{"phase any type", `"selectExecute":"select"`, true},
		{"phase types", `"typeIds":[45,50,58,63],"selectExecute":"execute"`, true},
		{"adjacent ranges", `"ioa":[{"start":0,"end":0},{"start":1,"end":2}]`, true},
		{"duplicate list members", `"ca":[1,1]`, true},
		{"type zero", `"typeIds":[0]`, false},
		{"type high", `"typeIds":[256]`, false},
		{"class", `"classes":["invalid"]`, false},
		{"ca negative", `"ca":[-1]`, false},
		{"ca broadcast", `"ca":[65535]`, false},
		{"ioa negative", `"ioa":[{"start":-1,"end":1}]`, false},
		{"ioa high", `"ioa":[{"start":1,"end":16777216}]`, false},
		{"ioa reversed", `"ioa":[{"start":2,"end":1}]`, false},
		{"ioa overlaps", `"ioa":[{"start":1,"end":2},{"start":2,"end":3}]`, false},
		{"ioa duplicate", `"ioa":[{"start":1,"end":1},{"start":1,"end":1}]`, false},
		{"ioa missing start", `"ioa":[{"end":1}]`, false},
		{"ioa missing end", `"ioa":[{"start":1}]`, false},
		{"ioa null start", `"ioa":[{"start":null,"end":1}]`, false},
		{"ioa unknown", `"ioa":[{"start":0,"end":1,"other":1}]`, false},
		{"ioa duplicate start", `"ioa":[{"start":0,"start":1}]`, false},
		{"cot low", `"cot":[0]`, false},
		{"cot high", `"cot":[64]`, false},
		{"originator low", `"originators":[-1]`, false},
		{"originator high", `"originators":[256]`, false},
		{"phase empty", `"selectExecute":""`, false},
		{"phase invalid", `"selectExecute":"other"`, false},
		{"phase bitstring", `"typeIds":[51],"selectExecute":"select"`, false},
		{"phase timed bitstring", `"typeIds":[64],"selectExecute":"execute"`, false},
		{"states no type", `"commandStates":[1]`, false},
		{"states mixed", `"typeIds":[45,46],"commandStates":[1]`, false},
		{"states unsupported", `"typeIds":[48],"commandStates":[1]`, false},
		{"single high", `"typeIds":[45],"commandStates":[2]`, false},
		{"single negative", `"typeIds":[58],"commandStates":[-1]`, false},
		{"double low", `"typeIds":[46],"commandStates":[0]`, false},
		{"double high", `"typeIds":[47],"commandStates":[3]`, false},
		{"qualifiers no type", `"commandQualifiers":[0]`, false},
		{"qualifiers type", `"typeIds":[48],"commandQualifiers":[0]`, false},
		{"qualifiers low", `"typeIds":[47],"commandQualifiers":[-1]`, false},
		{"qualifiers high", `"typeIds":[47],"commandQualifiers":[32]`, false},
		{"unknown key", `"other":false`, false},
		{"duplicate key", `"typeIds":[46],"typeIds":[47]`, false},
		{"case sensitive", `"TypeIds":[47]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertICSDecoders(t, `{"protocol":"iec104","mode":"monitor",`+tc.fields+`}`, tc.valid)
		})
	}
}

func assertICSDecoders(t *testing.T, input string, valid bool) {
	t.Helper()
	for name, decode := range map[string]func([]byte, *PlanICS) error{
		"json": func(data []byte, out *PlanICS) error { return json.Unmarshal(data, out) },
		"yaml": func(data []byte, out *PlanICS) error { return decodeStrict(data, out) },
	} {
		t.Run(name, func(t *testing.T) {
			var predicate PlanICS
			err := decode([]byte(input), &predicate)
			if (err == nil) != valid {
				t.Fatalf("decode = %v, valid=%v: %s", err, valid, input)
			}
			if valid {
				data, err := json.Marshal(predicate)
				if err != nil {
					t.Fatal(err)
				}
				var copy PlanICS
				if err := json.Unmarshal(data, &copy); err != nil || !reflect.DeepEqual(predicate, copy) {
					t.Fatalf("round trip: %s: %v", data, err)
				}
			}
		})
	}
}

func TestPlanIECPresence(t *testing.T) {
	for _, key := range iecPlanLists {
		for _, value := range []string{"null", "[]", "[null]", "[1.5]"} {
			t.Run(key+"="+value, func(t *testing.T) {
				assertICSDecoders(t, fmt.Sprintf(`{"protocol":"iec104","%s":%s}`, key, value), false)
			})
		}
	}
	for _, key := range iecPlanScalars {
		t.Run(key+"=null", func(t *testing.T) {
			assertICSDecoders(t, fmt.Sprintf(`{"protocol":"iec104","%s":null}`, key), false)
		})
	}
	for _, key := range iecForbiddenKeys {
		for _, value := range []string{"null", "[]", "false", "0"} {
			t.Run(key+"="+value, func(t *testing.T) {
				assertICSDecoders(t, fmt.Sprintf(`{"protocol":"iec104","%s":%s}`, key, value), false)
			})
		}
	}
	for _, protocol := range []string{"", "modbus", "dnp3"} {
		for _, keys := range [][]string{iecPlanLists, iecPlanScalars} {
			for _, key := range keys {
				t.Run(protocol+"-"+key, func(t *testing.T) {
					assertICSDecoders(t, fmt.Sprintf(`{"protocol":%q,"%s":null}`, protocol, key), false)
				})
			}
		}
	}
	for _, input := range []string{
		`{"protocol":"iec104","negative":false,"test":false}`,
		`{"protocol":"modbus","writeOnly":false}`,
	} {
		var predicate PlanICS
		if err := json.Unmarshal([]byte(input), &predicate); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(predicate)
		if err != nil {
			t.Fatal(err)
		}
		if input != string(data) {
			t.Fatalf("false lost on round trip: %s => %s", input, data)
		}
	}
	// Real YAML numeric scalars must not be coerced into integer selectors.
	for _, input := range []string{"protocol: iec104\nca: [1.5]", "protocol: iec104\nnegative: 0"} {
		if err := decodeStrict([]byte(input), &PlanICS{}); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestPlanMonitorPlacement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Curriculum)
		want   string
	}{
		{"required", func(c *Curriculum) { c.Plan.Rules[0].Required.ICS = &PlanICS{Protocol: "iec104", Mode: "monitor"} }, "forbidden"},
		{"deferred", func(c *Curriculum) { c.Plan.Rules[6].Deferred[0].ICS = &PlanICS{Protocol: "iec104", Mode: "monitor"} }, "forbidden"},
		{"selected", func(c *Curriculum) {
			c.Plan.Rules[6].Selected = []PlanRule{{ID: "selected", Action: "ALLOW", ICS: &PlanICS{Protocol: "iec104", Mode: "monitor"}}}
		}, "forbidden"},
		{"overlay", func(c *Curriculum) {
			c.Plan.Detect["iec104-dpi"] = PlanDetect{ICS: map[string]PlanICS{"control-centre-to-rtu-iec104": {Protocol: "iec104", Mode: "monitor"}}}
		}, "forbidden"},
		{"missing predicate", func(c *Curriculum) { d := c.Plan.Detect["iec104-dpi"]; d.Rules[0].ICS = nil }, "require ics"},
		{"inherit", func(c *Curriculum) { c.Plan.Detect["iec104-dpi"].Rules[0].ICS.Mode = "" }, "mode monitor"},
		{"enforce", func(c *Curriculum) { c.Plan.Detect["iec104-dpi"].Rules[0].ICS.Mode = "enforce" }, "mode monitor"},
		{"duplicate slot", func(c *Curriculum) { c.Plan.Detect["iec104-dpi"].Rules[0].ID = c.Plan.Rules[0].Required.ID }, "duplicate rule"},
		{"duplicate observe", func(c *Curriculum) { d := c.Plan.Detect["iec104-dpi"]; d.Rules[1].ID = d.Rules[0].ID }, "duplicate rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := planPackage(t, "eu-iec104-substation")
			tc.change(pkg.Curriculum)
			if _, err := validatePlan(pkg, pkg.Curriculum); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %s", err, tc.want)
			}
		})
	}
	for _, input := range []string{
		"plan:\n  rules:\n  - required:\n      ics: null",
		"plan:\n  rules:\n  - selected:\n    - ics: {}",
		"plan:\n  detect:\n    iec104-dpi:\n      ics: null",
	} {
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(input), &document); err != nil {
			t.Fatal(err)
		}
		if err := checkPlanICSPlacement(&document, ICSRulesMonitor); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestPlanMonitorEmissionAndEvaluation(t *testing.T) {
	pkg := planPackage(t, "eu-iec104-substation")
	empty := mustCompilePlan(t, pkg, nil)
	selected := mustCompilePlan(t, pkg, []string{"iec104-dpi"})
	if selected.Label != "partial" || len(selected.Rules) != len(empty.Rules)+3 {
		t.Fatalf("detect-only label/rules: %s/%d", selected.Label, len(selected.Rules))
	}
	if !reflect.DeepEqual(selected.Expectations, empty.Expectations) {
		t.Fatal("observation changed L4 proof")
	}
	for _, rule := range selected.Rules[len(empty.Rules):] {
		if rule.Origin != (PlanOrigin{Kind: "observe", Action: "iec104-dpi"}) || rule.Rule.ICS.Mode != "monitor" {
			t.Fatalf("observation origin/mode: %+v", rule)
		}
	}
	// Detect map order must never determine emission order.
	first := pkg.Curriculum.Plan.Detect["iec104-dpi"]
	second := PlanDetect{Rules: []PlanRule{{ID: "second-observer", Action: "DENY", ICS: &PlanICS{Protocol: "iec104", Mode: "monitor"}}}}
	pkg.Curriculum.Plan.Detect["improve-logging"] = second
	compiled := mustCompilePlan(t, pkg, []string{"improve-logging", "iec104-dpi"})
	tail := compiled.Rules[len(empty.Rules):]
	if tail[0].Rule.ID != first.Rules[0].ID || tail[3].Rule.ID != "second-observer" {
		t.Fatalf("observations not in catalog order: %+v", tail)
	}
	for _, action := range []string{"ALLOW", "DENY"} {
		rules := []PlanRule{{ID: "observe", Action: action, ICS: &PlanICS{Mode: "monitor"}}}
		for _, fallback := range []string{"ALLOW", "DENY"} {
			if got := Evaluate(rules, fallback, PlanTuple{}); got != planVerdict(fallback) {
				t.Fatalf("monitor %s changed default %s to %s", action, fallback, got)
			}
		}
	}
}

func TestPlanObservePackageModes(t *testing.T) {
	for _, mode := range []string{ICSRulesAllowed, ICSRulesMonitor, ICSRulesForbidden} {
		t.Run(mode, func(t *testing.T) {
			pkg := planPackage(t, "eu-iec104-substation")
			pkg.ICSRules = mode
			_, err := CompilePlan(pkg, pkg.Curriculum, []string{"iec104-dpi"})
			if mode == ICSRulesForbidden {
				if err == nil || !strings.Contains(err.Error(), "forbidden") {
					t.Fatalf("error=%v, want forbidden", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
