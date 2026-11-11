import { renderRuleTable, renderPlanTests } from "./plan-presentation";
import type { CompiledPlan, CurriculumBinding, Scenario } from "./api";

export function showsValidateButton(scenario: Pick<Scenario, "id" | "package_id" | "validator">, curriculum?: CurriculumBinding): boolean {
  return scenario.validator !== "" && (curriculum?.activities[scenario.id]?.validate ?? true);
}

// Step IDs survive edits to prose, translations and workshop labels.
export function injectDynamicContent(desc: string, stepId: string, plan: CompiledPlan | null, c?: CurriculumBinding, role = "implementation"): string {
  if (!plan || !c) return desc;
  const phase = c.dynamicSteps[stepId];
  if (!phase || !c.phases[phase]) return desc;
  return c.phases[phase]
    .replaceAll("{{noPlan}}", plan.actions.some((a) => a.chosen) ? "" : c.noPlan.replaceAll("{{planRole}}", c.roles.planning))
    .replaceAll("{{ruleTable}}", renderRuleTable(plan, c))
    .replaceAll("{{tests}}", phase === "rules" ? "" : renderPlanTests(plan, c, role, phase));
}
export function actionLabel(type: string): string {
  switch (type) {
    case "check": return "verify";
    case "firewall": return "config";
    case "sequence": return "auto";
    default: return type;
  }
}
