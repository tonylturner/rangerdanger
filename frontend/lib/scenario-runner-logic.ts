import { renderRuleTable, renderPlanTests } from "./plan-presentation";
import type { CompiledPlan, Course, Scenario } from "./api";
import { activityOf } from "./course";

export function showsValidateButton(scenario: Pick<Scenario, "id" | "package_id" | "validator">, course?: Course): boolean {
  return scenario.validator !== "" && (activityOf(course, scenario.id)?.validate ?? true);
}

// Dynamic templates belong exclusively to the bound implementation activity.
export function injectDynamicContent(desc: string, stepId: string, plan: CompiledPlan | null, course: Course | undefined, scenarioId: string): string {
  const feature = course?.features.segmentationPlan;
  if (!plan || !feature || scenarioId !== feature.activities.implementation) return desc;
  const c = feature.model;
  const phase = feature.dynamicSteps[stepId];
  if (!phase || !c.phases[phase]) return desc;
  return c.phases[phase]
    .replaceAll("{{noPlan}}", plan.actions.some((a) => a.chosen) ? "" : c.noPlan.replaceAll("{{planRole}}", feature.activities.planning))
    .replaceAll("{{ruleTable}}", renderRuleTable(plan, c))
    .replaceAll("{{tests}}", phase === "rules" ? "" : renderPlanTests(plan, c, scenarioId, phase));
}
export function actionLabel(type: string): string {
  switch (type) {
    case "check": return "verify";
    case "firewall": return "config";
    case "sequence": return "auto";
    default: return type;
  }
}
