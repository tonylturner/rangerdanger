package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func courseParityProjection(t *testing.T, id string) any {
	t.Helper()
	root := filepath.Join(repoRoot(), "lab-definitions")
	pkg, err := labs.NewLoader(root, ValidatorRequirements()).LoadPackage(filepath.Join(root, "packages", id, "package.yml"))
	if err != nil {
		t.Fatal(err)
	}
	c, feature := pkg.SegmentationPlan(), pkg.Course.Features.SegmentationPlan
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
	planBytes, err := json.Marshal(c.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	for _, p := range plan["proof"].([]any) {
		delete(p.(map[string]any), "activities")
	}
	content, err := json.Marshal(map[string]any{"model": model, "plan": plan})
	if err != nil {
		t.Fatal(err)
	}
	expectations, err := labs.CompilePlan(pkg, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	activities := map[string]any{}
	for position, a := range pkg.Course.Activities() {
		binding := ""
		for name, sid := range map[string]string{"requirements": feature.Activities.Requirements, "planning": feature.Activities.Planning, "implementation": feature.Activities.Implementation} {
			if sid == a.Scenario {
				binding = name
			}
		}
		probes, err := resolveProof(gen, expectations.Expectations, a.Scenario, true)
		if err != nil {
			t.Fatal(err)
		}
		validator := []string{}
		for _, p := range probes {
			validator = append(validator, p.ID)
		}
		dynamic := map[string]string{}
		if binding == "implementation" {
			for step, phase := range feature.DynamicSteps {
				dynamic[step] = c.Phases[phase]
			}
		}
		flags := map[string]bool{}
		for name, value := range map[string]*bool{"policyActions": a.PolicyActions, "hideBanner": a.HideBanner, "validate": a.Validate} {
			if value != nil {
				flags[name] = *value
			}
		}
		activities[a.Scenario] = map[string]any{"label": a.Label, "position": position, "flags": flags, "binding": binding, "dynamic": dynamic, "validator": validator}
	}
	return map[string]any{"contentHash": fmt.Sprintf("%x", sha256.Sum256(content)), "activities": activities, "nodeMap": pkg.Course.NodeMap, "nodeInference": pkg.Course.NodeInference}
}
