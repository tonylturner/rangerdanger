package labs

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCurriculumCanonicalValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Curriculum)
		want   string
	}{
		{"zero budget", func(c *Curriculum) { c.BudgetHours = 0 }, "budget, teams and catalog"},
		{"negative budget", func(c *Curriculum) { c.BudgetHours = -1 }, "budget, teams and catalog"},
		{"no teams", func(c *Curriculum) { c.Teams = []DecisionRole{} }, "budget, teams and catalog"},
		{"zero capacity", func(c *Curriculum) { c.Teams[0].CapacityHours = 0 }, "capacity_hours must be greater than zero"},
		{"negative capacity", func(c *Curriculum) { c.Teams[0].CapacityHours = -1 }, "capacity_hours must be greater than zero"},
		{"no catalog", func(c *Curriculum) { c.Catalog = []DecisionAction{} }, "budget, teams and catalog"},
		{"empty action id", func(c *Curriculum) { c.Catalog[0].ID = "" }, "action id must not be empty"},
		{"duplicate action id", func(c *Curriculum) { c.Catalog[1].ID = c.Catalog[0].ID }, "duplicate catalog action"},
		{"zero effort", func(c *Curriculum) { c.Catalog[0].EffortHours = 0 }, "effort_hours must be greater than zero"},
		{"negative effort", func(c *Curriculum) { c.Catalog[0].EffortHours = -1 }, "effort_hours must be greater than zero"},
		{"unknown role", func(c *Curriculum) { c.Catalog[0].Roles[0] = "foreign" }, "references unknown team"},
		{"undeclared default impl verdict", func(c *Curriculum) {
			c.Requirements["enterprise-to-field"].Impl["LOG"] = []string{"improve-logging"}
		}, "requirement enterprise-to-field impl verdict \"LOG\" is not a decision option"},
		{"undeclared explicit impl verdict", func(c *Curriculum) {
			c.Requirements["unauth-control"].Impl["DENY"] = []string{"improve-logging"}
		}, "requirement unauth-control impl verdict \"DENY\" is not a decision option"},
		{"empty impl verdict", func(c *Curriculum) {
			c.Requirements["enterprise-to-field"].Impl[""] = []string{"improve-logging"}
		}, "requirement enterprise-to-field impl verdict \"\" is not a decision option"},
		{"unknown readiness good", func(c *Curriculum) { c.Readiness[0].Good = "foreign" }, "good verdict \"foreign\" is not a decision option"},
		{"empty readiness good", func(c *Curriculum) { c.Readiness[0].Good = "" }, "good verdict \"\" is not a decision option"},
		{"unknown flag verdict", func(c *Curriculum) { c.ReadinessFlags[0].Verdict = "foreign" }, "verdict \"foreign\" is not a decision option"},
		{"empty flag verdict", func(c *Curriculum) { c.ReadinessFlags[0].Verdict = "" }, "verdict \"\" is not a decision option"},
		{"extra phase", func(c *Curriculum) { c.Phases["foreign"] = "unused template" }, "unknown phase template foreign"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg, binding := curriculumExample(t)
			if err := validateCurriculum(pkg); err != nil {
				t.Fatalf("valid fixture: %v", err)
			}
			// Mutate only the canonical sibling, serialize and load one independent defect.
			tc.change(pkg.Curriculum)
			data, err := yaml.Marshal(pkg.Curriculum)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeDefinition(t, filepath.Join(dir, binding.File), string(data))
			pkg.Curriculum, err = loadCurriculum(dir, binding, pkg)
			if err == nil {
				err = validateCurriculum(pkg)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCurriculumDeclaredOptionsOverrideDefaults(t *testing.T) {
	pkg, binding := curriculumExample(t)
	var options map[string]bool
	for i := range pkg.Scenarios {
		if pkg.Scenarios[i].ID == binding.Roles["requirements"] {
			step := &pkg.Scenarios[i].Steps[0]
			step.Description = strings.Replace(step.Description,
				":::decision id=enterprise-to-field\n",
				":::decision id=enterprise-to-field options=\"BLOCK,BLOCK and LOG,ALLOW\"\n", 1)
			options = curriculumDecisionOptions(pkg.Scenarios[i])["enterprise-to-field"]
		}
	}
	if len(options) != 3 || options["RESTRICT"] {
		t.Fatalf("fixture must explicitly exclude default RESTRICT: %v", options)
	}
	if err := validateCurriculum(pkg); err != nil {
		t.Fatalf("valid explicit-options fixture: %v", err)
	}
	// Only this mapping is invalid: the action exists, and RESTRICT is a
	// default option but not an option declared by this particular decision.
	pkg.Curriculum.Requirements["enterprise-to-field"].Impl["RESTRICT"] = []string{"block-enterprise-to-field"}
	data, err := yaml.Marshal(pkg.Curriculum)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeDefinition(t, filepath.Join(dir, binding.File), string(data))
	pkg.Curriculum, err = loadCurriculum(dir, binding, pkg)
	if err == nil {
		err = validateCurriculum(pkg)
	}
	want := `requirement enterprise-to-field impl verdict "RESTRICT" is not a decision option`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func TestCurriculumDecisionOptions(t *testing.T) {
	scenario := ScenarioYAML{Steps: []ScenarioStep{{Description: `
:::decision id=default
:::decision options="FIRST,With space,LAST" id="quoted"
:::decision id=explicit options=YES,NO
:::decision id=trimmed options=" , YES, NO, "
:::decision id=empty options=
`}}}
	options := curriculumDecisionOptions(scenario)
	for id, want := range map[string][]string{
		"default":  {"BLOCK", "BLOCK and LOG", "RESTRICT", "ALLOW"},
		"quoted":   {"FIRST", "With space", "LAST"},
		"explicit": {"YES", "NO"},
		"trimmed":  {"YES", "NO"},
		"empty":    {"BLOCK", "BLOCK and LOG", "RESTRICT", "ALLOW"},
	} {
		if len(options[id]) != len(want) {
			t.Fatalf("%s options = %v, want %v", id, options[id], want)
		}
		for _, value := range want {
			if !options[id][value] {
				t.Fatalf("%s missing %q", id, value)
			}
		}
	}
}
