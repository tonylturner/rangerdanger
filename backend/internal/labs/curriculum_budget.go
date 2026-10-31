package labs

import (
	"fmt"
	"math"
)

func validateBudgetAdjustments(c *Curriculum, decisions map[string]map[string]bool) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("curriculum: budgetAdjustments: "+format, args...)
	}
	sources, teams, actions := map[string]bool{}, map[string]int{}, map[string]bool{}
	for _, r := range c.Readiness {
		sources[r.ID] = true
	}
	for _, team := range c.Teams {
		teams[team.Name] = team.CapacityHours
	}
	for _, action := range c.Catalog {
		actions[action.ID] = true
	}
	seen := map[string]bool{}
	for _, a := range c.BudgetAdjustments {
		if !sources[a.Source] || !decisions[a.Source][a.Verdict] {
			return fail("unknown source/verdict %s = %q", a.Source, a.Verdict)
		}
		key := a.Source + ":" + a.Verdict
		if seen[key] {
			return fail("duplicate source/verdict %s", key)
		}
		seen[key] = true
		if a.TeamCaps == nil || a.Effort == nil {
			return fail("teamCaps and effort must be explicit collections")
		}
		if len(a.TeamCaps)+len(a.Effort) == 0 {
			return fail("adjustment must have an effect")
		}
		for team, cap := range a.TeamCaps {
			base, ok := teams[team]
			if !ok || cap < 0 || cap > base {
				return fail("invalid team cap %s = %d", team, cap)
			}
		}
		for _, e := range a.Effort {
			if len(e.Actions) == 0 || math.IsNaN(e.Multiplier) || math.IsInf(e.Multiplier, 0) || e.Multiplier < 1 || e.ExtraHours < 0 || (e.Multiplier == 1 && e.ExtraHours == 0) {
				return fail("effort needs actions and a finite multiplier >= 1 or nonnegative extraHours with an effect")
			}
			ids := map[string]bool{}
			for _, id := range e.Actions {
				if !actions[id] || ids[id] {
					return fail("unknown or duplicate effort action %s", id)
				}
				ids[id] = true
			}
		}
	}
	return nil
}
