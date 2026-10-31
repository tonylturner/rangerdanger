import type { CurriculumBinding } from "./api";
import { readCurriculum, type CurriculumScope } from "./curriculum-storage";
import { readReadiness, readinessFlagsForAction, type ReadinessAnswer } from "./requirement-coverage";

export const DEFAULT_BUDGET = "use-default-budget";

// A single eligibility authority for both the picker and all draft-plan readers.
// Caps take the minimum (including the base cap). Effort is base × product of
// multipliers + sum of overhead, rounded to the nearest whole hour only once.
export function computeBudgetAdjustments(
  readiness: ReadinessAnswer[],
  binding: CurriculumBinding,
  useDefaults = false,
) {
  const active = useDefaults ? [] : binding.budgetAdjustments.filter((a) =>
    readiness.some((r) => r.key === a.source && r.verdict === a.verdict),
  );
  const reason = (source: string, verdict: string) =>
    `${binding.readiness.find((r) => r.id === source)!.label} = ${verdict}`;
  const teamReasons: Record<string, string[]> = {};
  const actionReasons: Record<string, string[]> = {};
  const blockedReasons: Record<string, string[]> = {};
  const teams = binding.teams.map((team) => {
    const changes = active.filter((a) => a.teamCaps[team.name] !== undefined);
    teamReasons[team.name] = changes.map((a) => reason(a.source, a.verdict));
    return { ...team, capacity_hours: Math.min(team.capacity_hours, ...changes.map((a) => a.teamCaps[team.name])) };
  });
  const catalog = binding.catalog.map((action) => {
    let multiplier = 1, extraHours = 0;
    actionReasons[action.id] = [];
    for (const a of active) {
      for (const e of a.effort.filter((e) => e.actions.includes(action.id))) {
        multiplier *= e.multiplier;
        extraHours += e.extraHours;
        actionReasons[action.id].push(`${reason(a.source, a.verdict)}: ×${e.multiplier}, +${e.extraHours}h`);
      }
    }
    const blocked = useDefaults ? [] : readinessFlagsForAction(action.id, readiness, binding).filter((f) => f.severity === "blocked");
    if (blocked.length) blockedReasons[action.id] = blocked.map((f) => `${reason(f.source, f.verdict)} — ${f.message}`);
    return { ...action, effort_hours: Math.round(action.effort_hours * multiplier + extraHours) };
  });
  return { teams, catalog, teamReasons, actionReasons, blockedReasons };
}

export function usesDefaultBudget(scope: CurriculumScope): boolean {
  return readCurriculum(scope, DEFAULT_BUDGET) === "true";
}

export function readBudgetModel(scope: CurriculumScope) {
  return scope.curriculum
    ? computeBudgetAdjustments(readReadiness(scope), scope.curriculum, usesDefaultBudget(scope))
    : null;
}
