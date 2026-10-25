package labs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Each case starts with a valid authored fixture and changes only one field,
// exercising YAML omission/null decoding rather than constructing Go nils.
func TestCurriculumRequiredNestedCollections(t *testing.T) {
	cases := []struct {
		path []any
		want string
	}{
		{[]any{"requirements", "enterprise-to-field", "impl"}, "requirements.enterprise-to-field.impl"},
		{[]any{"readinessFlags", 0, "actions"}, "readinessFlags[0].actions"},
		{[]any{"impact", "modbus-dpi", "defends"}, "impact.modbus-dpi.defends"},
		{[]any{"nodeMap", "eu-remediation-planning", "others"}, "nodeMap.eu-remediation-planning.others"},
		{[]any{"catalog", 0, "roles"}, "catalog[0].roles"},
		{[]any{"requirements", "enterprise-to-field", "impl", "BLOCK"}, "requirements.enterprise-to-field.impl.BLOCK"},
	}
	for _, tc := range cases {
		for _, omit := range []bool{false, true} {
			// Verdict keys are optional; only a present verdict needs a list.
			if omit && len(tc.path) == 4 {
				continue
			}
			mode := "null"
			if omit {
				mode = "omitted"
			}
			t.Run(tc.want+"/"+mode, func(t *testing.T) {
				pkg, binding := curriculumExample(t)
				if err := validateCurriculum(pkg); err != nil {
					t.Fatalf("valid fixture: %v", err)
				}
				raw, err := os.ReadFile(filepath.Join("testdata", "curriculum-eu", binding.File))
				if err != nil {
					t.Fatal(err)
				}
				var fixture map[string]any
				if err := yaml.Unmarshal(raw, &fixture); err != nil {
					t.Fatal(err)
				}
				var parent any = fixture
				for _, part := range tc.path[:len(tc.path)-1] {
					switch key := part.(type) {
					case string:
						parent = parent.(map[string]any)[key]
					case int:
						parent = parent.([]any)[key]
					}
				}
				fields := parent.(map[string]any)
				key := tc.path[len(tc.path)-1].(string)
				if _, ok := fields[key]; !ok {
					t.Fatalf("fixture field %s does not exist", tc.want)
				}
				if omit {
					delete(fields, key)
				} else {
					fields[key] = nil
				}
				data, err := yaml.Marshal(fixture)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				writeDefinition(t, filepath.Join(dir, binding.File), string(data))
				pkg.Curriculum, err = loadCurriculum(dir, binding, pkg)
				if err == nil {
					err = validateCurriculum(pkg)
				}
				if err == nil || !strings.Contains(err.Error(), tc.want+" must be an explicit") {
					t.Fatalf("error = %v, want missing %s", err, tc.want)
				}
			})
		}
	}
}
