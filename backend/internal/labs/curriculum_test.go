package labs

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if c.ICSRules != ICSRulesForbidden || len(c.Catalog) != 14 || len(c.Requirements) != 5 || len(c.Readiness) != 4 || c.BudgetHours != 500 {
		t.Fatalf("incomplete EU model: %#v", c)
	}
	if len(c.BaselineRules) != 6 {
		t.Fatalf("required conduits = %d", len(c.BaselineRules))
	}
	if c.Policy[0].Rule.Sources[0] != "10.60.60.20/32" || c.Policy[2].Rule.Sources[0] != "10.30.30.30/32" {
		t.Fatal("CC / PLC source pins not derived from topology")
	}
	// Changing the topology changes the policy and prose; no network fact is
	// copied into a second curriculum authority.
	pkg.Template.Nodes[0].IP = "192.0.2.1"
	for i := range pkg.Template.Nodes {
		if pkg.Template.Nodes[i].ID == "cc-1" {
			pkg.Template.Nodes[i].IP = "192.0.2.20"
		}
	}
	binding := c.CurriculumBinding
	next, err := loadCurriculum(filepath.Join("testdata", "curriculum-eu"), &binding, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if next.Policy[0].Rule.Sources[0] != "192.0.2.20/32" {
		t.Fatal("stale CC source")
	}
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
		{"unknown topology node", strings.ReplaceAll(string(raw), "${ip:cc-1}", "${ip:missing}"), "unknown topology reference"},
		{"unknown topology network", strings.ReplaceAll(string(raw), "${zone:field_net}", "${zone:missing}"), "unknown topology reference"},
		{"unsupported schema", strings.Replace(string(raw), "schema: 1", "schema: 2", 1), "schema must be 1"},
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
		{"decision", func(c *Curriculum) { c.Requirements["foreign"] = CurriculumRequirement{} }, "unknown requirement decision"},
		{"readiness", func(c *Curriculum) { c.Readiness[0].ID = "foreign" }, "unknown readiness decision"},
		{"catalog", func(c *Curriculum) { c.Catalog[0].ID = "foreign" }, "unknown planning action"},
		{"coverage action", func(c *Curriculum) { r := c.Requirements["enterprise-to-field"]; r.Impl["BLOCK"] = []string{"foreign"} }, "unknown action"},
		{"impact scenario", func(c *Curriculum) { c.Impact["modbus-dpi"] = CurriculumImpact{Defends: []string{"foreign"}} }, "unknown scenario"},
		{"policy action", func(c *Curriculum) { c.Policy[0].When = "foreign" }, "unknown action"},
		{"effect", func(c *Curriculum) { g := c.Groups["modbus-dpi"]; g.Effect = "magic"; c.Groups["modbus-dpi"] = g }, "unknown effect"},
		{"deferred closure", func(c *Curriculum) {
			for i := range c.Policy {
				if c.Policy[i].Unless == "block-enterprise-to-field" {
					c.Policy[i].Unless = ""
				}
			}
		}, "scoped weak allows"},
		{"closure allow", func(c *Curriculum) {
			for i := range c.Policy {
				if c.Policy[i].When == "block-enterprise-to-field" {
					c.Policy[i].Rule.Action = "ALLOW"
				}
			}
		}, "cannot emit a selected allow"},
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
	if !c.LegacyEffects || !c.LegacyDisplayMutation || len(c.Catalog) != 14 || len(c.Requirements) != 5 || len(c.Policy) != 13 {
		t.Fatal("US extraction changed")
	}
	wire, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"planning":"remediation-planning"`, `"icsRules":"allowed"`, `"source":"10.30.30.20"`, `"phase-3-create-minimal-required-rules":"rules"`, `"access":{"mgmt":true,"http":true,"https":true,"ssh":true}`} {
		if !strings.Contains(string(wire), want) {
			t.Fatalf("wire DTO missing %s", want)
		}
	}
	if strings.Contains(string(wire), "${") {
		t.Fatal("unresolved topology facts in DTO")
	}
}
