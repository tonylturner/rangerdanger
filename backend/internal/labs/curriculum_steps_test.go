package labs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCurriculumStepReferences(t *testing.T) {
	for _, tc := range []struct {
		name, template, body, role, binding string
		valid                               bool
	}{
		{"valid", "{{step:implement}}\n\n{{ruleTable}}", "### Do this\n- Review.", "implementation", "rules", true},
		{"unknown step", "{{step:missing}}", "Body", "implementation", "rules", false},
		{"wrong role", "{{step:implement}}", "Body", "other", "rules", false},
		{"wrong phase", "{{step:implement}}", "Body", "implementation", "positive", false},
		{"empty body", "{{step:implement}}", "", "implementation", "rules", false},
		{"recursive", "{{step:implement}}", "{{step:implement}}", "implementation", "rules", false},
		{"malformed", "{{step:Bad}}", "Body", "implementation", "rules", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Curriculum{
				CurriculumBinding: CurriculumBinding{
					Roles:        map[string]string{"implementation": tc.role},
					DynamicSteps: map[string]string{"implement": tc.binding},
				},
				Phases: map[string]string{"rules": tc.template},
			}
			scenarios := []ScenarioYAML{{ID: "implementation", Steps: []ScenarioStep{{ID: "implement", Description: tc.body}}}}
			err := expandCurriculumSteps(c, scenarios)
			if (err == nil) != tc.valid {
				t.Fatalf("error = %v, valid = %v", err, tc.valid)
			}
			if tc.valid && c.Phases["rules"] != tc.body+"\n\n{{ruleTable}}" {
				t.Fatalf("expanded text = %q", c.Phases["rules"])
			}
		})
	}
}

func TestCurriculumEUPhasesUseScenarioInstructions(t *testing.T) {
	root := filepath.Join("..", "..", "..", "lab-definitions")
	dir := filepath.Join(root, "packages", "eu-iec104-substation")
	pkg, err := ReadDefinition(root, filepath.Join(dir, "package.yml"))
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := resolveScenarioSources(root, dir, []ScenarioSource{{Dir: "scenarios"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios {
		if scenario.ID != pkg.Curriculum.Roles["implementation"] {
			continue
		}
		for _, step := range scenario.Steps {
			if phase := pkg.Curriculum.DynamicSteps[step.ID]; phase != "" {
				text := pkg.Curriculum.Phases[phase]
				if !strings.HasPrefix(text, strings.TrimSpace(step.Description)+"\n\n### ") || strings.Contains(text, "{{step:") {
					t.Fatalf("%s does not reuse its scenario instructions", phase)
				}
			}
		}
	}
}
