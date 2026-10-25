package labs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var curriculumReference = regexp.MustCompile(`\$\{(ip|subnet|zone):([a-z0-9_-]+)\}`)
var curriculumDecision = regexp.MustCompile(`(?m)^[\t ]*:::decision[ \t]+([^\r\n]+)$`)
var curriculumDecisionAttribute = regexp.MustCompile(`\b(id|options)=("([^"]+)"|(\S+))`)

func loadCurriculum(dir string, binding *CurriculumBinding, pkg Package) (*Curriculum, error) {
	if binding == nil {
		return nil, nil
	}
	if binding.File == "" || filepath.Base(binding.File) != binding.File {
		return nil, fmt.Errorf("curriculum.file must name a sibling file")
	}
	data, err := os.ReadFile(filepath.Join(dir, binding.File))
	if err != nil {
		return nil, err
	}
	// Resolve only scalar values. This avoids corrupting YAML quoting in prose.
	// Parse exactly one document here; the resolved model is strictly typed below.
	var document yaml.Node
	if err := decodeStrict(data, &document); err != nil {
		return nil, err
	}
	if pkg.ICSRules == ICSRulesForbidden {
		if err := rejectForbiddenPlanICS(&document); err != nil {
			return nil, err
		}
	}
	facts := map[string]string{}
	for _, n := range pkg.Template.Nodes {
		facts["ip:"+n.ID] = n.IP
	}
	c := &Curriculum{}
	for _, n := range pkg.Template.Networks {
		subnet := n.Subnet
		if subnet == "" {
			subnet = n.CIDR
		}
		facts["subnet:"+n.ID] = subnet
		facts["zone:"+n.ID] = n.Zone
	}
	var resolve func(*yaml.Node) error
	resolve = func(n *yaml.Node) error {
		if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
			var missing string
			n.Value = curriculumReference.ReplaceAllStringFunc(n.Value, func(ref string) string {
				key := ref[2 : len(ref)-1]
				value := facts[key]
				if value == "" {
					missing = ref
				}
				return value
			})
			if missing != "" {
				return fmt.Errorf("unknown topology reference %s", missing)
			}
			if strings.Contains(n.Value, "${") {
				// "${" is reserved for topology interpolation throughout authored
				// strings, including prose. Reject typos rather than publish them.
				return fmt.Errorf("invalid topology reference in %q", n.Value)
			}
		}
		for _, child := range n.Content {
			if err := resolve(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := resolve(&document); err != nil {
		return nil, err
	}
	resolved, err := yaml.Marshal(&document)
	if err != nil {
		return nil, err
	}
	if err := decodeStrict(resolved, c); err != nil {
		return nil, err
	}
	c.CurriculumBinding = *binding
	c.ICSRules = pkg.ICSRules
	if c.Schema != 1 {
		return nil, fmt.Errorf("curriculum.schema must be 1")
	}
	if err := validateCurriculumCollections(c); err != nil {
		return nil, err
	}
	if _, err := validatePlan(pkg, c); err != nil {
		return nil, err
	}
	return c, nil
}

func validateCurriculum(pkg Package) error {
	c := pkg.Curriculum
	if err := validateDecision(pkg); err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	fail := func(format string, args ...any) error { return fmt.Errorf("curriculum: "+format, args...) }
	// Explicit empty collections keep the wire DTO free of null maps/arrays.
	if c.Requirements == nil || c.Readiness == nil || c.ReadinessFlags == nil || c.Groups == nil || c.PlanningOnly == nil || c.Phases == nil || c.Tests == nil || c.NodeMap == nil || c.NodeInference == nil || c.RequirementOrder == nil || c.GroupOrder == nil || c.ImpactOrder == nil || c.Impact == nil || c.Activities == nil || c.DynamicSteps == nil {
		return fail("collections must be explicit mappings or lists, not null or omitted")
	}
	if c.BudgetHours <= 0 || len(c.Teams) == 0 || len(c.Catalog) == 0 {
		return fail("budget, teams and catalog must not be empty")
	}
	if err := validateCurriculumCollections(c); err != nil {
		return err
	}
	teams := map[string]bool{}
	for _, team := range c.Teams {
		if team.CapacityHours <= 0 {
			return fail("team %q capacity_hours must be greater than zero", team.Name)
		}
		teams[team.Name] = true
	}
	scenarios := map[string]ScenarioYAML{}
	for _, s := range pkg.Scenarios {
		scenarios[s.ID] = s
	}
	for _, role := range []string{"baseline", "requirements", "planning", "implementation", "hardening", "vendor", "validation"} {
		if _, ok := scenarios[c.Roles[role]]; !ok {
			return fail("role %s must reference a scenario in this package", role)
		}
	}
	for role, id := range c.Roles {
		if _, ok := scenarios[id]; !ok {
			return fail("role %s references unknown scenario %s", role, id)
		}
	}
	for id := range c.Activities {
		if _, ok := scenarios[id]; !ok {
			return fail("activity references unknown scenario %s", id)
		}
	}
	for id := range c.NodeMap {
		if _, ok := scenarios[id]; !ok {
			return fail("nodeMap references unknown scenario %s", id)
		}
	}
	steps := map[string]bool{}
	for _, step := range scenarios[c.Roles["implementation"]].Steps {
		steps[step.ID] = true
	}
	for phase := range c.Phases {
		if phase != "rules" && phase != "positive" && phase != "negative" {
			return fail("unknown phase template %s", phase)
		}
	}
	for id, phase := range c.DynamicSteps {
		if !steps[id] {
			return fail("dynamicSteps references unknown implementation step %s", id)
		}
		if phase != "rules" && phase != "positive" && phase != "negative" {
			return fail("unknown dynamic phase %s", phase)
		}
		if c.Phases[phase] == "" {
			return fail("missing phase template %s", phase)
		}
	}
	decisions := curriculumDecisionOptions(scenarios[c.Roles["requirements"]])
	for id, requirement := range c.Requirements {
		options, ok := decisions[id]
		if !ok {
			return fail("unknown requirement decision %s", id)
		}
		for verdict := range requirement.Impl {
			if !options[verdict] {
				return fail("requirement %s impl verdict %q is not a decision option", id, verdict)
			}
		}
	}
	readiness := map[string]bool{}
	for _, r := range c.Readiness {
		options, ok := decisions[r.ID]
		if !ok {
			return fail("unknown readiness decision %s", r.ID)
		}
		if !options[r.Good] {
			return fail("readiness %s good verdict %q is not a decision option", r.ID, r.Good)
		}
		readiness[r.ID] = true
	}
	actions := map[string]bool{}
	for _, a := range c.Catalog {
		if a.ID == "" {
			return fail("catalog action id must not be empty")
		}
		if actions[a.ID] {
			return fail("duplicate catalog action %s", a.ID)
		}
		if a.EffortHours <= 0 {
			return fail("catalog action %s effort_hours must be greater than zero", a.ID)
		}
		for _, role := range a.Roles {
			if !teams[role] {
				return fail("catalog action %s references unknown team %q", a.ID, role)
			}
		}
		actions[a.ID] = true
	}
	check := func(ids ...string) error {
		for _, id := range ids {
			if id != "" && !actions[id] {
				return fail("unknown action %s", id)
			}
		}
		return nil
	}
	for _, req := range c.Requirements {
		for _, ids := range req.Impl {
			if err := check(ids...); err != nil {
				return err
			}
		}
	}
	for _, f := range c.ReadinessFlags {
		if !readiness[f.Source] {
			return fail("unknown readiness source %s", f.Source)
		}
		if !decisions[f.Source][f.Verdict] {
			return fail("readiness flag %s verdict %q is not a decision option", f.Source, f.Verdict)
		}
		if f.Severity != "warn" && f.Severity != "blocked" {
			return fail("unknown readiness severity %s", f.Severity)
		}
		if err := check(f.Actions...); err != nil {
			return err
		}
	}
	for id, impact := range c.Impact {
		if err := check(id); err != nil {
			return err
		}
		for _, s := range impact.Defends {
			if _, ok := scenarios[s]; !ok {
				return fail("impact references unknown scenario %s", s)
			}
		}
	}
	if err := check(c.PlanningOnly...); err != nil {
		return err
	}
	for id, g := range c.Groups {
		if err := check(id); err != nil {
			return err
		}
		switch g.Effect {
		case "closes", "restricts", "semantic", "conceptual", "logging":
		default:
			return fail("unknown effect %s", g.Effect)
		}
	}
	for _, order := range []struct {
		name  string
		ids   []string
		count int
		has   func(string) bool
	}{
		{"requirementOrder", c.RequirementOrder, len(c.Requirements), func(id string) bool { _, ok := c.Requirements[id]; return ok }},
		{"groupOrder", c.GroupOrder, len(c.Groups), func(id string) bool { _, ok := c.Groups[id]; return ok }},
		{"impactOrder", c.ImpactOrder, len(c.Impact), func(id string) bool { _, ok := c.Impact[id]; return ok }},
	} {
		seen := map[string]bool{}
		for _, id := range order.ids {
			if seen[id] || !order.has(id) {
				return fail("invalid %s entry %s", order.name, id)
			}
			seen[id] = true
		}
		if len(seen) != order.count {
			return fail("%s must list every entry exactly once", order.name)
		}
	}
	for _, t := range c.Tests {
		if t.Kind != "positive" && t.Kind != "negative" {
			return fail("unknown test kind %s", t.Kind)
		}
	}
	nodes := map[string]bool{}
	for _, n := range pkg.Template.Nodes {
		nodes[n.ID] = true
	}
	for _, n := range c.NodeInference {
		if !nodes[n.Node] {
			return fail("unknown inference node %s", n.Node)
		}
		if _, err := regexp.Compile(n.Pattern); err != nil {
			return fail("invalid inference pattern: %v", err)
		}
	}
	for _, n := range c.NodeMap {
		for _, id := range append([]string{n.Primary}, n.Others...) {
			if id != "" && !nodes[id] {
				return fail("unknown exercise node %s", id)
			}
		}
	}
	_, err := validatePlan(pkg, c)
	return err
}

// Match the renderer's decision attributes, including quoted values and its
// default verdict vocabulary when options is absent. Attribute order is free.
func curriculumDecisionOptions(scenario ScenarioYAML) map[string]map[string]bool {
	decisions := map[string]map[string]bool{}
	for _, step := range scenario.Steps {
		for _, match := range curriculumDecision.FindAllStringSubmatch(step.Description, -1) {
			attrs := map[string]string{}
			for _, attr := range curriculumDecisionAttribute.FindAllStringSubmatch(match[1], -1) {
				if _, exists := attrs[attr[1]]; !exists {
					value := attr[3]
					if value == "" {
						value = attr[4]
					}
					attrs[attr[1]] = strings.TrimSpace(value)
				}
			}
			raw := attrs["options"]
			if raw == "" {
				raw = "BLOCK,BLOCK and LOG,RESTRICT,ALLOW"
			}
			options := map[string]bool{}
			for _, value := range strings.Split(raw, ",") {
				if value = strings.TrimSpace(value); value != "" {
					options[value] = true
				}
			}
			decisions[attrs["id"]] = options
		}
	}
	return decisions
}

// Required nested collections follow the same wire contract as the top-level
// collections. Empty maps/lists are valid; omitted or null values are not.
// Optional catalog tags retain their omission semantics.
func validateCurriculumCollections(c *Curriculum) error {
	missing := func(path string) error {
		return fmt.Errorf("curriculum: %s must be an explicit mapping or list, not null or omitted", path)
	}
	for id, req := range c.Requirements {
		if req.Impl == nil {
			return missing("requirements." + id + ".impl")
		}
		for verdict, actions := range req.Impl {
			if actions == nil {
				return missing("requirements." + id + ".impl." + verdict)
			}
		}
	}
	for i, flag := range c.ReadinessFlags {
		if flag.Actions == nil {
			return missing(fmt.Sprintf("readinessFlags[%d].actions", i))
		}
	}
	for id, impact := range c.Impact {
		if impact.Defends == nil {
			return missing("impact." + id + ".defends")
		}
	}
	for id, nodes := range c.NodeMap {
		if nodes.Others == nil {
			return missing("nodeMap." + id + ".others")
		}
	}
	for i, action := range c.Catalog {
		if action.Roles == nil {
			return missing(fmt.Sprintf("catalog[%d].roles", i))
		}
	}
	return nil
}
