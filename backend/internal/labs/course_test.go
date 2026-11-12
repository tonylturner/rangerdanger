package labs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func basicCourse(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/course-basic")); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "packages", "basic", "package.yml")
}

func TestCourseWithoutPlanner(t *testing.T) {
	root, path := basicCourse(t)
	pkg, err := NewLoader(root, nil).LoadPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.SegmentationPlan() != nil || len(pkg.Course.Activities()) != 2 || pkg.Course.Activities()[0].Label != "B" || pkg.Course.Activities()[1].Label != "A" {
		t.Fatalf("unexpected course: %+v", pkg.Course)
	}
	if pkg.Course.NodeInference[0].IP != "192.0.2.2" {
		t.Fatal("nodes topology resolution lost")
	}
}

func TestCourseStrictSchema(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"legacy curriculum", "curriculum: {}", "field curriculum"},
		{"unknown feature", "course: {features: {other: {}}}", "field other"},
		{"unknown binding", "course: {features: {segmentationPlan: {activities: {hardening: other}}}}", "field hardening"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var manifest PackageYAML
			if err := decodeStrict([]byte(tc.text), &manifest); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %s", err, tc.want)
			}
		})
	}
	for _, text := range []string{"scenario: first\nforeign: true\n", "scenario: first\npolicyActions: null\n", "scenario: first\nhideBanner: null\n", "scenario: first\nvalidate: null\n"} {
		var activity CurriculumActivity
		if err := decodeStrict([]byte(text), &activity); err == nil {
			t.Fatalf("accepted activity %s", text)
		}
	}
	var scenario ScenarioYAML
	if err := decodeStrict([]byte("id: example\norder: B\n"), &scenario); err == nil {
		t.Fatal("accepted scenario order")
	}
	var proof PlanProof
	if err := decodeStrict([]byte("labs: [implementation]\n"), &proof); err == nil {
		t.Fatal("accepted proof labs")
	}
}

func TestCourseInvalidStructure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Course)
		want   string
	}{
		{"missing course", func(c *Course) {}, "course must not be omitted"},
		{"empty course", func(c *Course) { *c = Course{} }, "course.modules"},
		{"empty modules", func(c *Course) { c.Modules = nil }, "course.modules"},
		{"empty activities", func(c *Course) { c.Modules[0].Activities = nil }, "needs title and activities"},
		{"duplicate module", func(c *Course) { c.Modules[1].ID = c.Modules[0].ID }, "duplicate course module"},
		{"blank title", func(c *Course) { c.Modules[0].Title = " " }, "needs title"},
		{"blank label", func(c *Course) { c.Modules[0].Activities[0].Label = " " }, "blank or duplicate"},
		{"duplicate label", func(c *Course) { c.Modules[1].Activities[0].Label = "B" }, "blank or duplicate"},
		{"unknown scenario", func(c *Course) { c.Modules[0].Activities[0].Scenario = "foreign" }, "unknown scenario"},
		{"scenario omitted", func(c *Course) { c.Modules = c.Modules[:1] }, "has no course activity"},
		{"scenario twice", func(c *Course) { c.Modules[1].Activities[0].Scenario = "first" }, "two activities"},
		{"binding not activity", func(c *Course) {
			c.Features.SegmentationPlan = &CurriculumBinding{Activities: CurriculumActivities{Requirements: "foreign", Planning: "first", Implementation: "second"}, DynamicSteps: map[string]string{}}
		}, "distinct course activities"},
		{"binding repeated", func(c *Course) {
			c.Features.SegmentationPlan = &CurriculumBinding{Activities: CurriculumActivities{Requirements: "first", Planning: "first", Implementation: "second"}, DynamicSteps: map[string]string{}}
		}, "distinct course activities"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, path := basicCourse(t)
			var manifest PackageYAML
			if err := decodeFile(path, &manifest); err != nil {
				t.Fatal(err)
			}
			tc.mutate(manifest.Course)
			if tc.name == "missing course" {
				manifest.Course = nil
			}
			data, err := yaml.Marshal(map[string]*Course{"course": manifest.Course})
			if err != nil {
				t.Fatal(err)
			}
			original, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			data = append(original[:strings.Index(string(original), "course:\n")], data...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLoader(root, nil).LoadPackage(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %s", err, tc.want)
			}
		})
	}
}

func TestCoursePlannerRequiredSurfaces(t *testing.T) {
	for _, kind := range []string{"decision", "policyActions", "hideBanner", "improved", "coverage", "scenario coverage", "validator"} {
		t.Run(kind, func(t *testing.T) {
			root, path := basicCourse(t)
			var manifest PackageYAML
			if err := decodeFile(path, &manifest); err != nil {
				t.Fatal(err)
			}
			scPath := filepath.Join(filepath.Dir(path), "scenarios", "z-first.yml")
			var sc ScenarioYAML
			if err := decodeFile(scPath, &sc); err != nil {
				t.Fatal(err)
			}
			no := false
			switch kind {
			case "decision":
				sc.Steps[0].Action = &StepAction{Type: "decision"}
			case "policyActions":
				manifest.Course.Modules[0].Activities[0].PolicyActions = &no
			case "hideBanner":
				manifest.Course.Modules[0].Activities[0].HideBanner = &no
			case "improved":
				sc.Steps[0].Action = &StepAction{Type: "check", Expect: map[string]any{"firewall_config": "improved"}}
			case "coverage":
				sc.Steps[0].Description = ":::plan-coverage\n:::"
			case "scenario coverage":
				sc.Description = ":::plan-coverage\n:::"
			case "validator":
				sc.Validator = "planner"
			}
			data, err := yaml.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(scPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			data, err = yaml.Marshal(map[string]*Course{"course": manifest.Course})
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(original[:strings.Index(string(original), "course:\n")], data...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			registry := ValidatorRequirements{"planner": {SegmentationPlan: true}}
			if _, err := NewLoader(root, registry).LoadPackage(path); err == nil || (!strings.Contains(err.Error(), "segmentationPlan") && !strings.Contains(err.Error(), "decision must belong")) {
				t.Fatalf("surface %s: %v", kind, err)
			}
		})
	}
}

func TestCourseRoutingCollections(t *testing.T) {
	pkg, _ := curriculumExample(t)
	pkg.Course.NodeMap["eu-remediation-planning"] = CurriculumNodes{Primary: "cc-1"}
	if err := validateCourseNodes(pkg); err == nil || !strings.Contains(err.Error(), "others must be an explicit") {
		t.Fatalf("missing others: %v", err)
	}
}

func TestCourseNodesStrict(t *testing.T) {
	for _, text := range []string{
		"nodeMap: {}\n", "nodeInference: []\n",
		"nodeMap: {}\nnodeInference: []\nforeign: true\n",
		"nodeMap: {foreign: {primary: fw-1, others: []}}\nnodeInference: []\n",
		"nodeMap: {}\nnodeInference: [{node: foreign, ip: 192.0.2.2, pattern: firewall}]\n",
		"nodeMap: {}\nnodeInference: [{node: fw-1, ip: 192.0.2.2, pattern: '['}]\n",
	} {
		t.Run(text, func(t *testing.T) {
			root, path := basicCourse(t)
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "nodes.yml"), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLoader(root, nil).LoadPackage(path); err == nil {
				t.Fatal("accepted invalid nodes")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		root, path := basicCourse(t)
		nodes := filepath.Join(filepath.Dir(path), "nodes.yml")
		target := filepath.Join(t.TempDir(), "nodes.yml")
		data, err := os.ReadFile(nodes)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(nodes); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, nodes); err != nil {
			t.Fatal(err)
		}
		if _, err := NewLoader(root, nil).LoadPackage(path); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("symlink: %v", err)
		}
	})
}
