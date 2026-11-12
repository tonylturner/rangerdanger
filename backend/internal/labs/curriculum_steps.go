package labs

import (
	"fmt"
	"regexp"
	"strings"
)

var curriculumStepReference = regexp.MustCompile(`\{\{step:([a-z0-9-]+)\}\}`)

// Phase templates reuse the implementation scenario's instructions. Resolving
// before publishing the curriculum keeps static and compiled-plan surfaces in
// agreement without requiring the client to load another scenario.
func expandCurriculumSteps(c *Curriculum, binding *CurriculumBinding, scenarios []ScenarioYAML) error {
	steps := map[string]string{}
	for _, scenario := range scenarios {
		if scenario.ID == binding.Activities.Implementation {
			for _, step := range scenario.Steps {
				steps[step.ID] = strings.TrimSpace(step.Description)
			}
		}
	}
	for phase, text := range c.Phases {
		var invalid string
		text = curriculumStepReference.ReplaceAllStringFunc(text, func(ref string) string {
			id := curriculumStepReference.FindStringSubmatch(ref)[1]
			body := steps[id]
			if binding.DynamicSteps[id] != phase || body == "" || strings.Contains(body, "{{step:") {
				invalid = ref
				return ref
			}
			return body
		})
		if invalid != "" || strings.Contains(text, "{{step:") {
			return fmt.Errorf("curriculum phase %s has invalid implementation step reference", phase)
		}
		c.Phases[phase] = text
	}
	return nil
}
