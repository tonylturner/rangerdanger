import {
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
  curriculumForPlan,
  type DynamicExercisePlan,
} from "./remediation-to-rules";
import type { CurriculumBinding, Scenario } from "./api";

export function showsValidateButton(scenario: Pick<Scenario, "id" | "package_id" | "validator">, curriculum?: CurriculumBinding): boolean {
  return scenario.validator !== "" && (curriculum?.activities[scenario.id]?.validate ?? true);
}

// Step IDs survive edits to prose, translations and workshop labels.
export function injectDynamicContent(desc: string, stepId: string, plan: DynamicExercisePlan | null): string {
  if (!plan) return desc;
  const c = curriculumForPlan(plan);
  const phase = c.dynamicSteps[stepId];
  if (!phase || !c.phases[phase]) return desc;
  const tests = phase === "positive" ? positiveValidationTests(plan) : negativeValidationTests(plan);
  return c.phases[phase]
    .replace("{{noPlan}}", plan.hasRemediationPlan ? "" : c.noPlan.replace("{{planRole}}", c.roles.planning))
    .replace("{{ruleTable}}", renderRuleTable(plan))
    .replace("{{tests}}", tests.join("\n\n"));
}
export function actionLabel(type: string): string {
  switch (type) {
    case "command": return "auto";
    case "check": return "verify";
    case "firewall": return "config";
    case "sequence": return "auto";
    default: return type;
  }
}

export function deviceLabel(device: string): string {
  switch (device) {
    case "relay": return "Feeder Breaker (10.40.40.20)";
    case "recloser": return "Recloser (10.40.40.21)";
    case "regulator": return "Regulator (10.40.40.22)";
    default: return device;
  }
}

export function isSegmentationStep(title?: string): boolean {
  if (!title) return false;
  const t = title.toLowerCase();
  // Match the "apply hardened policy" steps in Lab 2.3 and 2.3-bonus
  // alongside the older "improve segmentation" wording used elsewhere.
  // Without `hardened`/`harden`, Lab 2.3 step 6 ("Apply the hardened
  // policy") and Lab 2.3-bonus step 5 ("Apply hardened policy") would
  // hide the Apply Hardened button even though the step's own action
  // is { type: firewall, config: improved }.
  return t.includes("improve") || t.includes("segmentation") || t.includes("hardened") || t.includes("harden");
}

export function isBaselineStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("baseline") || title.toLowerCase().includes("observe");
}
