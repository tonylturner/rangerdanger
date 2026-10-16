// The Lab 1.4 remediation plan. DecisionPanel writes it; later
// exercises read it to display or branch on the student's plan.

import { REMEDIATION_PLAN, readCurriculum, writeCurriculum, type CurriculumScope } from "./curriculum-storage";

export type RemediationPlan = {
  exerciseId: string;
  selectedActionIds: string[];
  savedAt: string; // ISO timestamp
};

export function saveRemediationPlan(scope: CurriculumScope, plan: RemediationPlan): void {
  if (!scope.curriculum) return;
  writeCurriculum(scope, REMEDIATION_PLAN, JSON.stringify(plan));
}

export function loadRemediationPlan(scope: CurriculumScope): RemediationPlan | null {
  if (!scope.curriculum) return null;
  const raw = readCurriculum(scope, REMEDIATION_PLAN);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as RemediationPlan;
  } catch {
    return null;
  }
}


// Qualify planning claims without changing historical US banner prose.
export function actionEffectLabel(binding: import("./api").CurriculumBinding, id: string): string {
  if (binding.legacyEffects) return "";
  const effect = binding.groups[id]?.effect;
  if (effect === "semantic" && binding.icsRules === "forbidden") return "planned, not enforceable in this range yet";
  if (effect === "conceptual" || binding.planningOnly?.includes(id)) return "planning-only";
  return "";
}
