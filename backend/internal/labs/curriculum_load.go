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
var curriculumDecision = regexp.MustCompile(`:::decision\s+id=([a-z0-9-]+)`)

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
	var strict Curriculum
	if err := decodeStrict(data, &strict); err != nil {
		return nil, err
	}
	// Resolve only scalar values. This avoids corrupting YAML quoting in prose.
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
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
		if n.Zone != "" {
			c.Interfaces = append(c.Interfaces, CurriculumInterface{Name: n.Zone, Zone: n.Zone, Access: CurriculumAccess{}})
			c.Zones = append(c.Zones, CurriculumZone{Name: n.Zone, Description: n.Name})
		}
	}
	// Management is the platform's out-of-band control plane, not a teaching
	// network. Neither package topology declares it.
	c.Interfaces = append(c.Interfaces, CurriculumInterface{Name: "lan3", Zone: "lan3", Access: CurriculumAccess{Mgmt: true, HTTP: true, HTTPS: true, SSH: true}})
	c.Zones = append(c.Zones, CurriculumZone{Name: "lan3", Description: "RangerDanger management control plane"})
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
	return c, nil
}

func validateCurriculum(pkg Package) error {
	c := pkg.Curriculum
	if c == nil {
		return nil
	}
	fail := func(format string, args ...any) error { return fmt.Errorf("curriculum: "+format, args...) }
	// Explicit empty collections keep the wire DTO free of null maps/arrays.
	if c.Requirements == nil || c.Readiness == nil || c.ReadinessFlags == nil || c.Groups == nil || c.PlanningOnly == nil || c.Policy == nil || c.Phases == nil || c.Tests == nil || c.NodeMap == nil || c.NodeInference == nil || c.RequirementOrder == nil || c.GroupOrder == nil || c.ImpactOrder == nil || c.Impact == nil || c.LoggingPrefixes == nil || c.Flags.DPI == nil || c.BaselineRules == nil || c.Activities == nil || c.DynamicSteps == nil {
		return fail("collections must be explicit mappings or lists, not null or omitted")
	}
	if c.BudgetHours <= 0 || len(c.Teams) == 0 || len(c.Catalog) == 0 {
		return fail("budget, teams and catalog must not be empty")
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
	decisions := map[string]bool{}
	for _, step := range scenarios[c.Roles["requirements"]].Steps {
		for _, m := range curriculumDecision.FindAllStringSubmatch(step.Description, -1) {
			decisions[m[1]] = true
		}
	}
	for id := range c.Requirements {
		if !decisions[id] {
			return fail("unknown requirement decision %s", id)
		}
	}
	readiness := map[string]bool{}
	for _, r := range c.Readiness {
		if !decisions[r.ID] {
			return fail("unknown readiness decision %s", r.ID)
		}
		readiness[r.ID] = true
	}
	authored := map[string]bool{}
	for _, step := range scenarios[c.Roles["planning"]].Steps {
		if step.Action != nil && step.Action.Type == "decision" {
			for _, a := range step.Action.Actions {
				authored[a.ID] = true
			}
		}
	}
	actions := map[string]bool{}
	for _, a := range c.Catalog {
		if !authored[a.ID] {
			return fail("catalog references unknown planning action %s", a.ID)
		}
		if actions[a.ID] {
			return fail("duplicate catalog action %s", a.ID)
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
	if err := check(append([]string{c.Flags.Logging, c.Flags.Objects, c.Flags.Validation}, c.Flags.DPI...)...); err != nil {
		return err
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
		if (g.Effect == "conceptual" || g.Effect == "logging") && len(g.Rules) > 0 {
			return fail("%s action %s cannot emit rules", g.Effect, id)
		}
		if !c.LegacyEffects && (g.Effect == "closes" || g.Effect == "restricts") {
			omitted := false
			for _, entry := range c.Policy {
				if entry.Unless == id && entry.Rule.Action == "ALLOW" && len(entry.Rule.SourceZones) > 0 && len(entry.Rule.DestZones) > 0 && len(entry.Rule.Protocols) > 0 {
					omitted = true
				}
			}
			if !omitted || len(g.OmittedRules) == 0 {
				return fail("%s must declare scoped weak allows when omitted", id)
			}
		}
	}
	for _, e := range c.Policy {
		if err := check(e.When, e.Unless, e.Semantic); err != nil {
			return err
		}
		if !c.LegacyEffects && e.When != "" && c.Groups[e.When].Effect == "closes" && e.Rule.Action != "DENY" {
			return fail("closes action %s cannot emit a selected allow", e.When)
		}
		if e.Rule.Action != "ALLOW" && e.Rule.Action != "DENY" {
			return fail("invalid policy action %s", e.Rule.Action)
		}
		if e.When != "" && e.Unless != "" {
			return fail("policy entry cannot have both when and unless")
		}
		if e.Semantic != "" && c.Groups[e.Semantic].Effect != "semantic" {
			return fail("predicate action %s must be semantic", e.Semantic)
		}
		for _, id := range []string{e.When, e.Unless} {
			if id != "" {
				g, ok := c.Groups[id]
				if !ok || g.Effect == "conceptual" || g.Effect == "logging" {
					return fail("policy entry references non-policy action %s", id)
				}
			}
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
	for id := range actions {
		if _, ok := c.Groups[id]; !ok {
			found := false
			for _, other := range c.PlanningOnly {
				if other == id {
					found = true
				}
			}
			if !found {
				return fail("action %s has no effect", id)
			}
		}
	}
	for _, t := range c.Tests {
		if err := check(t.When); err != nil {
			return err
		}
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
	return nil
}
