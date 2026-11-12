package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"gopkg.in/yaml.v3"
)

func courseParityProjection(t *testing.T, id string) any {
	t.Helper()
	root := filepath.Join(repoRoot(), "lab-definitions")
	manifestPath := filepath.Join(root, "packages", id, "package.yml")
	pkg, err := labs.NewLoader(root, ValidatorRequirements()).LoadPackage(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	c := pkg.Curriculum
	gen := usGeneration(nil)
	if id == euPackageID {
		gen = euGeneration(t, nil, nil)
	}
	gen.Package = pkg
	modelBytes, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var model map[string]any
	if err := json.Unmarshal(modelBytes, &model); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"roles", "activities", "dynamicSteps", "nodeMap", "nodeInference"} {
		delete(model, key)
	}
	planBytes, err := json.Marshal(c.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	for _, p := range plan["proof"].([]any) {
		delete(p.(map[string]any), "labs")
	}
	content, err := json.Marshal(map[string]any{"model": model, "plan": plan})
	if err != nil {
		t.Fatal(err)
	}
	// Read authored flags separately: bool DTO fields intentionally cannot tell
	// omission from false, but the course declaration must retain that distinction.
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Curriculum struct {
			Activities map[string]map[string]bool `yaml:"activities"`
		} `yaml:"curriculum"`
	}
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	scenarios := append([]labs.ScenarioYAML(nil), pkg.Scenarios...)
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Order < scenarios[j].Order })
	expectations, err := labs.CompilePlan(pkg, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	activities := map[string]any{}
	for position, sc := range scenarios {
		role := ""
		for r, sid := range c.Roles {
			if sid == sc.ID {
				role = r
			}
		}
		binding := ""
		if slices.Contains([]string{"requirements", "planning", "implementation"}, role) {
			binding = role
		}
		probes, err := resolveProof(gen, expectations.Expectations, sc.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		validator := []string{}
		for _, p := range probes {
			validator = append(validator, p.ID)
		}
		dynamic := map[string]string{}
		if binding == "implementation" {
			for step, phase := range c.DynamicSteps {
				dynamic[step] = c.Phases[phase]
			}
		}
		activities[sc.ID] = map[string]any{"label": sc.Order, "position": position, "flags": manifest.Curriculum.Activities[sc.ID], "binding": binding, "dynamic": dynamic, "validator": validator}
	}
	return map[string]any{"contentHash": fmt.Sprintf("%x", sha256.Sum256(content)), "activities": activities, "nodeMap": c.NodeMap, "nodeInference": c.NodeInference}
}
