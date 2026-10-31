package labs

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPlanSemanticValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Package, *Curriculum)
		want   string
	}{
		{"missing plan", func(_ *Package, c *Curriculum) { c.Plan = nil }, "explicit"},
		{"missing rules", func(_ *Package, c *Curriculum) { c.Plan.Rules = nil }, "explicit"},
		{"missing detect", func(_ *Package, c *Curriculum) { c.Plan.Detect = nil }, "explicit"},
		{"missing proof", func(_ *Package, c *Curriculum) { c.Plan.Proof = nil }, "explicit"},
		{"unknown action", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Action = "foreign" }, "unknown action"},
		{"duplicate action", func(_ *Package, c *Curriculum) { c.Plan.Rules = append(c.Plan.Rules, c.Plan.Rules[0]) }, "more than one"},
		{"unknown detect", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["foreign"] = PlanDetect{Log: []string{"rtac-to-field-modbus"}}
		}, "unknown action"},
		{"duplicate kind", func(_ *Package, c *Curriculum) {
			c.Plan.Detect[c.Plan.Rules[0].Action] = PlanDetect{Log: []string{"rtac-to-field-modbus"}}
		}, "more than one"},
		{"empty detect", func(_ *Package, c *Curriculum) { c.Plan.Detect["modbus-dpi"] = PlanDetect{} }, "must declare metadata"},
		{"required with action", func(_ *Package, c *Curriculum) { c.Plan.Rules[5].Action = "modbus-dpi" }, "required slot"},
		{"required with selected", func(_ *Package, c *Curriculum) { c.Plan.Rules[5].Selected = []PlanRule{} }, "required slot"},
		{"required with deferred", func(_ *Package, c *Curriculum) { c.Plan.Rules[5].Deferred = []PlanRule{} }, "required slot"},
		{"missing selected", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected = nil }, "explicit selected"},
		{"missing deferred", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Deferred = nil }, "nonempty deferred"},
		{"empty deferred", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Deferred = []PlanRule{} }, "nonempty deferred"},
		{"missing slot action", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Action = "" }, "action slot"},
		{"invalid rule action", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Action = "ACCEPT" }, "invalid action"},
		{"empty rule id", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].ID = "" }, "id must not be empty"},
		{"duplicate rule id", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Deferred[0].ID = c.Plan.Rules[0].Selected[0].ID }, "duplicate rule"},
		{"unknown zone", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].SourceZones = []string{"foreign"} }, "unknown zone"},
		{"invalid IP", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Sources = []string{"999.0.0.1"} }, "invalid IP or CIDR"},
		{"invalid CIDR", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Destinations = []string{"192.0.2.0/33"} }, "invalid IP or CIDR"},
		{"invalid protocol", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Name = "any" }, "invalid protocol"},
		{"invalid port", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Port = "22,80" }, "invalid port"},
		{"reversed port range", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Port = "100-90" }, "invalid port"},
		{"port zero", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Port = "0" }, "invalid port"},
		{"port overflow", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Port = "65536" }, "invalid port"},
		{"port whitespace", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Port = " 22" }, "invalid port"},
		{"port with ICMP", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].Protocols[0].Name = "icmp" }, "cannot have a port"},
		{"unknown log target", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["improve-logging"] = PlanDetect{Log: []string{"foreign"}}
		}, "unknown rule"},
		{"duplicate log target", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["improve-logging"] = PlanDetect{Log: []string{"rtac-to-field-modbus", "rtac-to-field-modbus"}}
		}, "repeats log target"},
		{"unknown ICS target", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["modbus-dpi"] = PlanDetect{ICS: map[string]PlanICS{"foreign": {}}}
		}, "unknown rule"},
		{"competing ICS", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["dnp3-dpi"] = PlanDetect{ICS: map[string]PlanICS{"rtac-to-field-modbus": {}}}
		}, "conflicting ics"},
		{"base ICS conflict", func(_ *Package, c *Curriculum) { c.Plan.Rules[5].Required.ICS = &PlanICS{} }, "conflicting ics"},
		{"bad ICS protocol", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["modbus-dpi"] = PlanDetect{ICS: map[string]PlanICS{"rtac-to-field-modbus": {Protocol: "foreign"}}}
		}, "invalid ics protocol"},
		{"bad ICS code", func(_ *Package, c *Curriculum) {
			c.Plan.Detect["modbus-dpi"] = PlanDetect{ICS: map[string]PlanICS{"rtac-to-field-modbus": {FunctionCode: []int{256}}}}
		}, "functionCode"},
		{"bad ICS direction", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].ICS = &PlanICS{Direction: "foreign"} }, "invalid ics direction"},
		{"bad ICS mode", func(_ *Package, c *Curriculum) { c.Plan.Rules[0].Selected[0].ICS = &PlanICS{Mode: "foreign"} }, "invalid ics mode"},
		{"contradictory ICS", func(_ *Package, c *Curriculum) {
			c.Plan.Rules[0].Selected[0].ICS = &PlanICS{ReadOnly: true, WriteOnly: boolPointer(true)}
		}, "both readOnly"},
		{"duplicate proof id", func(_ *Package, c *Curriculum) { c.Plan.Proof[1].ID = c.Plan.Proof[0].ID }, "duplicate proof"},
		{"invalid category", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Category = "foreign" }, "invalid category"},
		{"invalid expectation", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Expect = "skip" }, "invalid expect"},
		{"invalid proof port", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Port = 0 }, "valid port"},
		{"implicit owners", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Owners = nil }, "explicit owners"},
		{"unknown owner", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Owners = []string{"foreign"} }, "unknown, non-enforce"},
		{"detect owner", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Owners = []string{"modbus-dpi"} }, "unknown, non-enforce"},
		{"duplicate owner", func(_ *Package, c *Curriculum) {
			c.Plan.Proof[0].Owners = []string{"pin-rtac-to-field", "pin-rtac-to-field"}
		}, "duplicate owner"},
		{"bridged owner", func(_ *Package, c *Curriculum) { c.Plan.Proof[3].Owners = []string{"pin-rtac-to-field"} }, "bridged proof"},
		{"unknown lab", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Labs = []string{"foreign"} }, "unknown lab role"},
		{"unknown source", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].From = "foreign" }, "unknown source or destination"},
		{"unknown destination", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].To = "foreign" }, "unknown source or destination"},
		{"unknown network", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Network = "foreign" }, "destination network"},
		{"wrong destination network", func(_ *Package, c *Curriculum) { c.Plan.Proof[0].Network = "vendor_net" }, "destination network"},
		{"source matches no network", func(p *Package, _ *Curriculum) {
			for i := range p.Template.Nodes {
				if p.Template.Nodes[i].ID == "rtac-1" {
					p.Template.Nodes[i].IP = "192.0.2.20"
				}
			}
		}, "exactly one topology network, got 0"},
		{"source matches overlapping networks", func(p *Package, _ *Curriculum) { p.Template.Networks[1].Subnet = "10.30.30.0/24" }, "exactly one topology network, got 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := planPackage(t, "us-dnp3-substation")
			tc.change(&pkg, pkg.Curriculum)
			if _, err := validatePlan(pkg, pkg.Curriculum); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPlanICSVocabulary(t *testing.T) {
	unit := uint8(1)
	for _, tc := range []struct {
		name      string
		predicate PlanICS
		want      string
	}{
		{"empty", PlanICS{}, ""},
		{"modbus inherit", PlanICS{Protocol: "modbus"}, ""},
		{"modbus monitor", PlanICS{Protocol: "modbus", Mode: "monitor"}, ""},
		{"modbus enforce", PlanICS{Protocol: "modbus", Mode: "enforce"}, ""},
		{"modbus selectors", PlanICS{Protocol: "modbus", UnitID: &unit, Addresses: []string{"0-10"}}, ""},
		{"dnp3 inherit", PlanICS{Protocol: "dnp3", FunctionCode: []int{1}}, ""},
		{"dnp3 monitor", PlanICS{Protocol: "dnp3", Mode: "monitor"}, ""},
		{"dnp3 enforce", PlanICS{Protocol: "dnp3", Mode: "enforce"}, ""},
		{"learn", PlanICS{Protocol: "modbus", Mode: "learn"}, "invalid ics mode"},
		{"literal inherit", PlanICS{Protocol: "modbus", Mode: "inherit"}, "invalid ics mode"},
		{"protocol-less monitor", PlanICS{Mode: "monitor"}, "mode requires a protocol"},
		{"protocol-less enforce", PlanICS{Mode: "enforce"}, "mode requires a protocol"},
		{"protocol-less unit", PlanICS{UnitID: &unit}, "unitId is Modbus-only"},
		{"dnp3 unit", PlanICS{Protocol: "dnp3", UnitID: &unit}, "unitId is Modbus-only"},
		{"dnp3 addresses", PlanICS{Protocol: "dnp3", Addresses: []string{"1"}}, "unsupported groups"},
		{"protocol-less addresses", PlanICS{Addresses: []string{"1"}}, "addresses require modbus"},
		{"object classes", PlanICS{Protocol: "modbus", ObjectClasses: []uint16{1}}, "unsupported cip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePlanICS(tc.predicate)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	for _, protocol := range []string{"s7comm", "mms", "bacnet", "opcua", "cip"} {
		t.Run(protocol, func(t *testing.T) {
			if err := validatePlanICS(PlanICS{Protocol: protocol}); err == nil || !strings.Contains(err.Error(), "invalid ics protocol") {
				t.Fatalf("error = %v, want unsupported protocol", err)
			}
		})
	}
}

func TestPlanStrictLoader(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		want   string
	}{
		{"unknown plan field", func(p map[string]any) { p["foreign"] = true }, "field foreign"},
		{"unknown slot field", func(p map[string]any) { firstPlanSlot(p)["foreign"] = true }, "field foreign"},
		{"state", func(p map[string]any) { firstPlanRule(p)["state"] = []any{"ESTABLISHED"} }, "field state"},
		{"identities", func(p map[string]any) { firstPlanRule(p)["identities"] = []any{"student"} }, "field identities"},
		{"schedule", func(p map[string]any) { firstPlanRule(p)["schedule"] = map[string]any{} }, "field schedule"},
		{"display selector", func(p map[string]any) { firstPlanRule(p)["source"] = "any" }, "field source"},
		{"unknown protocol field", func(p map[string]any) {
			firstPlanRule(p)["protocols"].([]any)[0].(map[string]any)["foreign"] = true
		}, "field foreign"},
		{"EU empty ICS", func(p map[string]any) { firstPlanRule(p)["ics"] = map[string]any{} }, "ics key is forbidden"},
		{"EU null ICS", func(p map[string]any) { firstPlanRule(p)["ics"] = nil }, "ics key is forbidden"},
		{"EU overlay ICS", func(p map[string]any) {
			p["detect"] = map[string]any{"modbus-dpi": map[string]any{"ics": map[string]any{}}}
		}, "ics key is forbidden"},
		{"absent selected", func(p map[string]any) { delete(p["rules"].([]any)[6].(map[string]any), "selected") }, "explicit selected"},
		{"null selected", func(p map[string]any) { p["rules"].([]any)[6].(map[string]any)["selected"] = nil }, "explicit selected"},
		{"null rule", func(p map[string]any) { firstPlanSlot(p)["required"] = nil }, "action slot"},
		{"unknown proof field", func(p map[string]any) { p["proof"].([]any)[0].(map[string]any)["foreign"] = true }, "field foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, binding := curriculumExample(t)
			raw := curriculumExampleDocument(t, binding)
			var document map[string]any
			if err := yaml.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			tc.change(document["plan"].(map[string]any))
			data, err := yaml.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeDefinition(t, filepath.Join(dir, binding.Dir, "catalog.yml"), string(data))
			if _, err := loadCurriculum(dir, binding, pkg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func firstPlanSlot(plan map[string]any) map[string]any {
	return plan["rules"].([]any)[0].(map[string]any)
}

func firstPlanRule(plan map[string]any) map[string]any {
	return firstPlanSlot(plan)["required"].(map[string]any)
}

func TestPlanRuleStrictJSONAndYAML(t *testing.T) {
	for _, field := range []string{"state", "identities", "schedule", "purpose"} {
		t.Run(field, func(t *testing.T) {
			rule := map[string]any{"id": "example", "action": "ALLOW", field: []any{}}
			data, err := json.Marshal([]any{rule})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodePlanRules(data); err == nil {
				t.Fatal("strict JSON decoder accepted unsupported field")
			}
			yamlData, err := yaml.Marshal(rule)
			if err != nil {
				t.Fatal(err)
			}
			if err := decodeStrict(yamlData, &PlanRule{}); err == nil {
				t.Fatal("strict YAML decoder accepted unsupported field")
			}
		})
	}
	for _, field := range []string{"functionCodes", "note", "state"} {
		t.Run("predicate-"+field, func(t *testing.T) {
			data := []byte(`[{ "id":"example", "action":"ALLOW", "ics":{"` + field + `":[]} }]`)
			if _, err := decodePlanRules(data); err == nil {
				t.Fatal("strict JSON predicate decoder accepted unsupported field")
			}
			if err := decodeStrict([]byte("id: example\naction: ALLOW\nics:\n  "+field+": []\n"), &PlanRule{}); err == nil {
				t.Fatal("strict YAML predicate decoder accepted unsupported field")
			}
		})
	}
}
