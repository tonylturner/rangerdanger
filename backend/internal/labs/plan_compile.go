package labs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// CompilePlan builds a policy from authored slots, never legacy display or
// policy fields. Slot order controls L4 rule emission; observations follow in
// catalog order, which also canonicalizes selections. The result owns its data
// and can be edited without mutating the package or a later compilation.
func CompilePlan(pkg Package, curriculum *Curriculum, actionIDs []string) (CompiledPlan, error) {
	kinds, err := validatePlan(pkg, curriculum)
	if err != nil {
		return CompiledPlan{}, err
	}
	chosen := map[string]bool{}
	for _, id := range actionIDs {
		if _, exists := kinds[id]; !exists {
			return CompiledPlan{}, fmt.Errorf("plan: unknown action %s", id)
		}
		chosen[id] = true
	}
	compiled := CompiledPlan{
		Rules:        []CompiledPlanRule{},
		Actions:      []PlanAction{},
		Expectations: []PlanExpectation{},
		Label:        "weak",
	}
	technical, selected := 0, 0
	for _, action := range curriculum.Catalog {
		kind := kinds[action.ID]
		compiled.Actions = append(compiled.Actions, PlanAction{ID: action.ID, Kind: kind, Chosen: chosen[action.ID]})
		if kind != PlanActionOnly {
			technical++
			if chosen[action.ID] {
				selected++
			}
		}
	}
	if selected > 0 {
		compiled.Label = "partial"
		if selected == technical {
			compiled.Label = "hardened"
		}
	}
	for _, slot := range curriculum.Plan.Rules {
		if slot.Required != nil {
			compiled.Rules = append(compiled.Rules, CompiledPlanRule{Rule: *slot.Required, Origin: PlanOrigin{Kind: "required"}})
			continue
		}
		variant, kind := slot.Deferred, "deferred"
		if chosen[slot.Action] {
			variant, kind = slot.Selected, "selected"
		}
		for _, rule := range variant {
			compiled.Rules = append(compiled.Rules, CompiledPlanRule{Rule: rule, Origin: PlanOrigin{Kind: kind, Action: slot.Action}})
		}
	}
	// Apply overlays in catalog order too. Validation forbids competing ICS
	// writers; log overlays are idempotent.
	for _, action := range compiled.Actions {
		if action.Kind != PlanActionDetect || !action.Chosen {
			continue
		}
		detect := curriculum.Plan.Detect[action.ID]
		for _, rule := range detect.Rules {
			compiled.Rules = append(compiled.Rules, CompiledPlanRule{Rule: rule, Origin: PlanOrigin{Kind: "observe", Action: action.ID}})
		}
		for i := range compiled.Rules {
			rule := &compiled.Rules[i].Rule
			if slices.Contains(detect.Log, rule.ID) {
				rule.Log = true
			}
			if predicate, exists := detect.ICS[rule.ID]; exists {
				rule.ICS = &predicate
			}
		}
	}
	// Deep-copy nested rule selectors and predicates as a group, not by
	// maintaining a second field-by-field copy of the appliance schema.
	ruleData, err := json.Marshal(compiled.Rules)
	if err != nil {
		return CompiledPlan{}, err
	}
	var ownedRules []CompiledPlanRule
	if err := json.Unmarshal(ruleData, &ownedRules); err != nil {
		return CompiledPlan{}, err
	}
	compiled.Rules = ownedRules
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(pkg.planEnvelope, &envelope); err != nil {
		return CompiledPlan{}, fmt.Errorf("plan: missing or invalid improved policy envelope: %w", err)
	}
	var firewall map[string]json.RawMessage
	if err := json.Unmarshal(envelope["firewall"], &firewall); err != nil || firewall == nil {
		return CompiledPlan{}, fmt.Errorf("plan: improved policy needs a firewall object")
	}
	var defaultAction string
	if err := json.Unmarshal(firewall["defaultAction"], &defaultAction); err != nil ||
		(defaultAction != "ALLOW" && defaultAction != "DENY") {
		return CompiledPlan{}, fmt.Errorf("plan: invalid improved defaultAction")
	}
	hardened, err := decodePlanRules(firewall["rules"])
	if err != nil {
		return CompiledPlan{}, fmt.Errorf("plan: improved rules: %w", err)
	}
	emitted := make([]PlanRule, 0, len(compiled.Rules))
	for _, entry := range compiled.Rules {
		emitted = append(emitted, entry.Rule)
	}
	if firewall["rules"], err = json.Marshal(emitted); err != nil {
		return CompiledPlan{}, err
	}
	if envelope["firewall"], err = json.Marshal(firewall); err != nil {
		return CompiledPlan{}, err
	}
	if compiled.Config, err = json.Marshal(envelope); err != nil {
		return CompiledPlan{}, err
	}
	if err := LintPolicy("compiled plan", compiled.Config, pkg.ICSRules); err != nil {
		return CompiledPlan{}, err
	}
	for _, proof := range curriculum.Plan.Proof {
		route, err := ResolvePlanTuple(pkg, proof)
		if err != nil {
			return CompiledPlan{}, err
		}
		compiled.Expectations = append(compiled.Expectations, PlanExpectation{
			ID: proof.ID, From: proof.From, To: proof.To, Network: proof.Network,
			Port: proof.Port, Category: proof.Category, Owners: slices.Clone(proof.Owners),
			Labs: slices.Clone(proof.Labs), Note: proof.Note,
			PlanVerdict:     evaluatePlanRoute(emitted, defaultAction, route),
			HardenedVerdict: evaluatePlanRoute(hardened, defaultAction, route),
			Bridged:         route.Bridged,
		})
	}
	return compiled, nil
}

func decodePlanRules(data []byte) ([]PlanRule, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var rules []PlanRule
	if err := decoder.Decode(&rules); err != nil {
		return nil, err
	}
	if rules == nil {
		return nil, fmt.Errorf("rules must be an array")
	}
	return rules, nil
}
