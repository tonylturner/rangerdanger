package labs

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// rejectForbiddenPlanICS inspects key presence, including null and {}. Legacy
// curriculum fields are deliberately outside the new plan's schema boundary.
func rejectForbiddenPlanICS(document *yaml.Node) error {
	var walk func(*yaml.Node) error
	walk = func(node *yaml.Node) error {
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == "ics" {
					return fmt.Errorf("plan: ics key is forbidden")
				}
			}
		}
		for _, child := range node.Content {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	for i := 0; root.Kind == yaml.MappingNode && i < len(root.Content); i += 2 {
		if root.Content[i].Value == "plan" {
			return walk(root.Content[i+1])
		}
	}
	return nil
}

func validatePlan(pkg Package, c *Curriculum) (map[string]PlanActionKind, error) {
	fail := func(format string, args ...any) (map[string]PlanActionKind, error) {
		return nil, fmt.Errorf("plan: "+format, args...)
	}
	if c == nil || c.Plan == nil || len(c.Plan.Rules) == 0 || c.Plan.Detect == nil || len(c.Plan.Proof) == 0 {
		return fail("rules, detect and proof must be explicit, with nonempty rules and proof")
	}
	if c.BudgetHours <= 0 || len(c.Teams) == 0 || len(c.Catalog) == 0 {
		return fail("budget, teams and catalog must not be empty")
	}
	kinds := map[string]PlanActionKind{}
	teams := map[string]bool{}
	for _, team := range c.Teams {
		if team.CapacityHours <= 0 {
			return fail("team %q capacity_hours must be greater than zero", team.Name)
		}
		teams[team.Name] = true
	}
	for _, action := range c.Catalog {
		if action.ID == "" {
			return fail("catalog action id must not be empty")
		}
		if _, exists := kinds[action.ID]; exists {
			return fail("duplicate catalog action %s", action.ID)
		}
		if action.EffortHours <= 0 {
			return fail("catalog action %s effort_hours must be greater than zero", action.ID)
		}
		if len(action.Roles) == 0 {
			return fail("catalog action %s needs a team", action.ID)
		}
		for _, role := range action.Roles {
			if !teams[role] {
				return fail("catalog action %s references unknown team %q", action.ID, role)
			}
		}
		kinds[action.ID] = PlanActionOnly
	}
	claim := func(id string, kind PlanActionKind) error {
		previous, exists := kinds[id]
		if !exists {
			return fmt.Errorf("plan: unknown action %s", id)
		}
		if previous != PlanActionOnly {
			return fmt.Errorf("plan: action %s occurs in more than one slot or kind", id)
		}
		kinds[id] = kind
		return nil
	}
	zones := map[string]bool{"lan3": true}
	for _, network := range pkg.Template.Networks {
		zones[network.Zone] = network.Zone != ""
	}
	rules := map[string]PlanRule{}
	addRule := func(rule PlanRule) error {
		if _, exists := rules[rule.ID]; exists {
			return fmt.Errorf("plan: duplicate rule id %s", rule.ID)
		}
		if err := validatePlanRule(rule, zones, pkg.ICSRules); err != nil {
			return fmt.Errorf("plan rule %s: %w", rule.ID, err)
		}
		rules[rule.ID] = rule
		return nil
	}
	for _, slot := range c.Plan.Rules {
		if slot.Required != nil {
			if slot.Action != "" || slot.Selected != nil || slot.Deferred != nil {
				return fail("required slot cannot declare action, selected or deferred")
			}
			if err := addRule(*slot.Required); err != nil {
				return nil, err
			}
			continue
		}
		if slot.Action == "" || slot.Selected == nil || len(slot.Deferred) == 0 {
			return fail("action slot needs action, explicit selected and nonempty deferred")
		}
		if err := claim(slot.Action, PlanActionEnforce); err != nil {
			return nil, err
		}
		for _, variant := range [][]PlanRule{slot.Selected, slot.Deferred} {
			for _, rule := range variant {
				if err := addRule(rule); err != nil {
					return nil, err
				}
			}
		}
	}
	icsOwners := map[string]string{}
	for id, detection := range c.Plan.Detect {
		if err := claim(id, PlanActionDetect); err != nil {
			return nil, err
		}
		if len(detection.Log) == 0 && len(detection.ICS) == 0 {
			return fail("detect action %s must declare metadata", id)
		}
		seen := map[string]bool{}
		for _, target := range detection.Log {
			if _, exists := rules[target]; !exists {
				return fail("detect action %s targets unknown rule %s", id, target)
			}
			if seen[target] {
				return fail("detect action %s repeats log target %s", id, target)
			}
			seen[target] = true
		}
		if pkg.ICSRules == ICSRulesForbidden && detection.ICS != nil {
			return fail("detect action %s: ics key is forbidden", id)
		}
		for target, predicate := range detection.ICS {
			rule, exists := rules[target]
			if !exists {
				return fail("detect action %s targets unknown rule %s", id, target)
			}
			if previous := icsOwners[target]; previous != "" || rule.ICS != nil {
				return fail("conflicting ics overlays for rule %s", target)
			}
			if err := validatePlanICS(predicate); err != nil {
				return fail("detect action %s: %v", id, err)
			}
			icsOwners[target] = id
		}
	}
	proofIDs := map[string]bool{}
	for _, proof := range c.Plan.Proof {
		if strings.TrimSpace(proof.ID) == "" || proofIDs[proof.ID] {
			return fail("empty or duplicate proof id %q", proof.ID)
		}
		proofIDs[proof.ID] = true
		if proof.Category != "authorized" && proof.Category != "unauthorized" {
			return fail("proof %s has invalid category %q", proof.ID, proof.Category)
		}
		if proof.Expect != VerdictAllow && proof.Expect != VerdictDeny {
			return fail("proof %s has invalid expect %q", proof.ID, proof.Expect)
		}
		if proof.Port < 1 || proof.Port > 65535 || proof.Owners == nil {
			return fail("proof %s needs a valid port and explicit owners", proof.ID)
		}
		route, err := ResolvePlanTuple(pkg, proof)
		if err != nil {
			return nil, err
		}
		if route.Bridged && len(proof.Owners) != 0 {
			return fail("bridged proof %s cannot have owners", proof.ID)
		}
		seen := map[string]bool{}
		for _, owner := range proof.Owners {
			if kinds[owner] != PlanActionEnforce || seen[owner] {
				return fail("proof %s has unknown, non-enforce or duplicate owner %s", proof.ID, owner)
			}
			seen[owner] = true
		}
		for _, role := range proof.Labs {
			if c.Roles[role] == "" {
				return fail("proof %s references unknown lab role %s", proof.ID, role)
			}
		}
	}
	return kinds, nil
}

func validatePlanRule(rule PlanRule, zones map[string]bool, icsRules string) error {
	if strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("id must not be empty")
	}
	if rule.Action != "ALLOW" && rule.Action != "DENY" {
		return fmt.Errorf("invalid action %q", rule.Action)
	}
	for _, list := range [][]string{rule.SourceZones, rule.DestZones} {
		for _, zone := range list {
			if !zones[zone] {
				return fmt.Errorf("unknown zone %q", zone)
			}
		}
	}
	for _, list := range [][]string{rule.Sources, rule.Destinations} {
		for _, value := range list {
			if _, err := netip.ParseAddr(value); err == nil {
				continue
			}
			if _, err := netip.ParsePrefix(value); err != nil {
				return fmt.Errorf("invalid IP or CIDR %q", value)
			}
		}
	}
	for _, protocol := range rule.Protocols {
		if !slices.Contains([]string{"tcp", "udp", "icmp", "icmpv6"}, protocol.Name) {
			return fmt.Errorf("invalid protocol %q", protocol.Name)
		}
		if protocol.Port != "" {
			if protocol.Name != "tcp" && protocol.Name != "udp" {
				return fmt.Errorf("protocol %s cannot have a port", protocol.Name)
			}
			if _, _, err := planPortRange(protocol.Port); err != nil {
				return err
			}
		}
	}
	if rule.ICS != nil {
		if icsRules == ICSRulesForbidden {
			return fmt.Errorf("ics key is forbidden")
		}
		return validatePlanICS(*rule.ICS)
	}
	return nil
}

func validatePlanICS(predicate PlanICS) error {
	for _, code := range predicate.FunctionCode {
		if code < 0 || code > 255 {
			return fmt.Errorf("ics functionCode must be in 0..255")
		}
	}
	if !slices.Contains([]string{"", "modbus", "dnp3", "cip", "s7comm", "mms", "bacnet", "opcua"}, predicate.Protocol) {
		return fmt.Errorf("invalid ics protocol %q", predicate.Protocol)
	}
	if !slices.Contains([]string{"", "request", "response"}, predicate.Direction) {
		return fmt.Errorf("invalid ics direction %q", predicate.Direction)
	}
	if !slices.Contains([]string{"", "learn", "enforce"}, predicate.Mode) {
		return fmt.Errorf("invalid ics mode %q", predicate.Mode)
	}
	if predicate.ReadOnly && predicate.WriteOnly {
		return fmt.Errorf("ics cannot be both readOnly and writeOnly")
	}
	return nil
}
