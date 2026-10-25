package labs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func curriculumExample(t *testing.T) (Package, *CurriculumBinding) {
	t.Helper()
	dir := filepath.Join("testdata", "curriculum-eu")
	var manifest struct {
		Curriculum *CurriculumBinding `yaml:"curriculum"`
		Topology   string             `yaml:"topology"`
		ICSRules   string             `yaml:"ics_rules"`
	}
	if err := decodeFile(filepath.Join(dir, "package.yml"), &manifest); err != nil {
		t.Fatal(err)
	}
	pkg := Package{ID: "eu-example", ICSRules: manifest.ICSRules}
	if err := decodeFile(filepath.Join(dir, manifest.Topology), &pkg.Template); err != nil {
		t.Fatal(err)
	}
	if err := decodeFile(filepath.Join(dir, "scenarios.json"), &pkg.Scenarios); err != nil {
		t.Fatal(err)
	}
	var err error
	pkg.Curriculum, err = loadCurriculum(dir, manifest.Curriculum, pkg)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, manifest.Curriculum
}

func TestCurriculumEUExample(t *testing.T) {
	pkg, _ := curriculumExample(t)
	if err := validateCurriculum(pkg); err != nil {
		t.Fatal(err)
	}
	c := pkg.Curriculum
	if c.ICSRules != ICSRulesForbidden || len(c.Catalog) != 15 || len(c.Requirements) != 5 || len(c.Readiness) != 4 || c.BudgetHours != 500 {
		t.Fatalf("incomplete EU model: %#v", c)
	}
	if c.Plan.Rules[0].Required.Sources[0] != "10.60.60.20/32" {
		t.Fatal("CC source pin not derived from topology")
	}
	// Changing the topology changes the policy and prose; no network fact is
	// copied into a second curriculum authority.
	pkg.Template.Nodes[0].IP = "192.0.2.1"
	for i := range pkg.Template.Nodes {
		if pkg.Template.Nodes[i].ID == "cc-1" {
			pkg.Template.Nodes[i].IP = "192.0.2.20"
		}
	}
	for i := range pkg.Template.Networks {
		if pkg.Template.Networks[i].ID == "control_centre_net" {
			pkg.Template.Networks[i].Subnet = "192.0.2.0/24"
		}
	}
	binding := c.CurriculumBinding
	next, err := loadCurriculum(filepath.Join("testdata", "curriculum-eu"), &binding, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if next.Plan.Rules[0].Required.Sources[0] != "192.0.2.20/32" {
		t.Fatal("stale CC source")
	}
}

func TestCurriculumEUFieldDestinationInventory(t *testing.T) {
	pkg, _ := curriculumExample(t)
	var want []string
	for _, node := range pkg.Template.Nodes {
		if len(node.Networks) == 1 && node.Networks[0] == "field_net" {
			want = append(want, node.IP+"/32")
		}
	}
	if len(want) == 0 {
		t.Fatal("no field devices in topology")
	}
	slices.Sort(want)
	for _, slot := range pkg.Curriculum.Plan.Rules {
		if slot.Required != nil && slot.Required.ID == "rtu-to-feeder-modbus" {
			got := slices.Clone(slot.Required.Destinations)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("field destinations %v want %v", got, want)
			}
			return
		}
	}
	t.Fatal("missing required RTU field rule")
}

func TestCurriculumStrictDecode(t *testing.T) {
	pkg, binding := curriculumExample(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "curriculum-eu", "curriculum.yml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, data, want string }{
		{"unknown field", string(raw) + "\nsurprise: true\n", "field surprise"},
		{"extra document", string(raw) + "\n---\nschema: 1\n", "exactly one"},
		{"unknown nested field", strings.Replace(string(raw), "capacity_hours: 120", "capacity_hours: 120\n    surprise: true", 1), "field surprise"},
		{"duplicate field", string(raw) + "\nschema: 1\n", "already defined"},
		{"duplicate nested field", strings.Replace(string(raw), "capacity_hours: 120", "capacity_hours: 120\n    capacity_hours: 120", 1), "already defined"},
		{"unknown topology node", strings.ReplaceAll(string(raw), "${ip:cc-1}", "${ip:missing}"), "unknown topology reference"},
		{"unknown topology network", strings.ReplaceAll(string(raw), "${zone:field_net}", "${zone:missing}"), "unknown topology reference"},
		{"malformed topology reference", strings.ReplaceAll(string(raw), "${ip:cc-1}", "${ips:cc-1}"), "invalid topology reference"},
		{"reserved interpolation in prose", strings.Replace(string(raw), "noPlan: |+", "noPlan: |+\n  Literal ${ prose", 1), "invalid topology reference"},
		{"unsupported schema", strings.Replace(string(raw), "schema: 1", "schema: 2", 1), "schema must be 1"},
	}
	for _, field := range []string{"policy", "baselineRules", "enterpriseToVendorRule", "flags", "loggingPrefixes", "legacyDisplayMutation", "legacyEffects"} {
		cases = append(cases, struct{ name, data, want string }{"retired " + field, string(raw) + "\n" + field + ": []\n", "field " + field})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeDefinition(t, filepath.Join(dir, binding.File), tc.data)
			_, err := loadCurriculum(dir, binding, pkg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
	bad := *binding
	bad.File = "../curriculum.yml"
	if _, err := loadCurriculum("testdata/curriculum-eu", &bad, pkg); err == nil {
		t.Fatal("accepted non-sibling file")
	}
}

func TestCurriculumReferences(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Curriculum)
		want   string
	}{
		{"role", func(c *Curriculum) { c.Roles["planning"] = "remediation-planning" }, "role planning"},
		{"activity", func(c *Curriculum) { c.Activities["foreign"] = CurriculumActivity{} }, "activity"},
		{"step", func(c *Curriculum) { c.DynamicSteps["translated-title"] = "rules" }, "unknown implementation step"},
		{"decision", func(c *Curriculum) { c.Requirements["foreign"] = CurriculumRequirement{Impl: map[string][]string{}} }, "unknown requirement decision"},
		{"readiness", func(c *Curriculum) { c.Readiness[0].ID = "foreign" }, "unknown readiness decision"},
		{"catalog", func(c *Curriculum) { c.Catalog[0].ID = "foreign" }, "unknown action"},
		{"coverage action", func(c *Curriculum) { r := c.Requirements["enterprise-to-field"]; r.Impl["BLOCK"] = []string{"foreign"} }, "unknown action"},
		{"impact scenario", func(c *Curriculum) { c.Impact["modbus-dpi"] = CurriculumImpact{Defends: []string{"foreign"}} }, "unknown scenario"},
		{"plan action", func(c *Curriculum) { c.Plan.Rules[6].Action = "foreign" }, "unknown action"},
		{"effect", func(c *Curriculum) { g := c.Groups["modbus-dpi"]; g.Effect = "magic"; c.Groups["modbus-dpi"] = g }, "unknown effect"},
		{"node", func(c *Curriculum) { c.NodeInference[0].Node = "rtac-1" }, "unknown inference node"},
		{"ordering", func(c *Curriculum) { c.GroupOrder = c.GroupOrder[:1] }, "groupOrder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, _ := curriculumExample(t)
			tc.change(pkg.Curriculum)
			err := validateCurriculum(pkg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCurriculumAbsent(t *testing.T) {
	c, err := loadCurriculum("does-not-exist", nil, Package{})
	if err != nil || c != nil {
		t.Fatalf("no binding = %v, %v", c, err)
	}
	if err := validateCurriculum(Package{}); err != nil {
		t.Fatal(err)
	}
}

func TestCurriculumUSExtraction(t *testing.T) {
	definitions := filepath.Join("..", "..", "..", "lab-definitions")
	var manifest PackageYAML
	path := filepath.Join(definitions, "packages", "us-dnp3-substation", "package.yml")
	if err := decodeFile(path, &manifest); err != nil {
		t.Fatal(err)
	}
	pkg, err := ReadDefinition(definitions, path)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Scenarios, err = loadScenarioFiles(filepath.Join(filepath.Dir(path), manifest.Scenarios))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCurriculum(pkg); err != nil {
		t.Fatal(err)
	}
	c := pkg.Curriculum
	if len(c.Catalog) != 15 || len(c.Requirements) != 5 || len(c.Plan.Proof) != 21 {
		t.Fatal("US extraction changed")
	}
	wire, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"planning":"remediation-planning"`, `"icsRules":"allowed"`, `"phase-3-create-minimal-required-rules":"rules"`} {
		if !strings.Contains(string(wire), want) {
			t.Fatalf("wire DTO missing %s", want)
		}
	}
	for _, retired := range []string{"policy", "baselineRules", "enterpriseToVendorRule", "flags", "loggingPrefixes", "legacyEffects", "legacyDisplayMutation", "interfaces", "zones", "plan"} {
		if strings.Contains(string(wire), `"`+retired+`":`) {
			t.Fatalf("retired field %s served", retired)
		}
	}
	if strings.Contains(string(wire), "${") {
		t.Fatal("unresolved topology facts in DTO")
	}
}

func TestCurriculumRejectsRetiredInlineCatalog(t *testing.T) {
	for _, field := range []string{"budget_hours: 500", "roles: []", "actions: []"} {
		t.Run(field, func(t *testing.T) {
			var scenario ScenarioYAML
			err := decodeStrict([]byte("steps:\n  - action:\n      type: decision\n      "+field+"\n"), &scenario)
			if err == nil || !strings.Contains(err.Error(), "not found in type labs.StepAction") {
				t.Fatalf("retired inline field accepted: %v", err)
			}
		})
	}
}
