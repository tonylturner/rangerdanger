// The Lab 1.4 remediation plan. DecisionPanel writes it; later
// exercises read it to display or branch on the student's plan.

import { REMEDIATION_PLAN, readCurriculum, writeCurriculum, type CurriculumScope } from "./curriculum-storage";
import { readBudgetModel } from "./readiness-budget";

export type RemediationPlan = {
  exerciseId: string;
  selectedActionIds: string[];
  savedAt: string; // ISO timestamp
};

export function saveRemediationPlan(scope: CurriculumScope, plan: RemediationPlan): void {
  if (!scope.curriculum) return;
  writeCurriculum(scope, REMEDIATION_PLAN, JSON.stringify(plan));
}

export function loadSavedRemediationPlan(scope: CurriculumScope): RemediationPlan | null {
  if (!scope.curriculum) return null;
  const raw = readCurriculum(scope, REMEDIATION_PLAN);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as RemediationPlan;
  } catch {
    return null;
  }
}

export type ExcludedAction = { id: string; reasons: string[] };

export function readRemediationPlanState(scope: CurriculumScope) {
  const saved = loadSavedRemediationPlan(scope);
  const model = readBudgetModel(scope);
  const excluded: ExcludedAction[] = [];
  const selectedActionIds = (saved?.selectedActionIds ?? []).filter((id) => {
    const reasons = model?.blockedReasons[id];
    if (!reasons) return true;
    excluded.push({ id, reasons });
    return false;
  });
  return { saved, effective: saved ? { ...saved, selectedActionIds } : null, excluded };
}

// Never rewrite saved intent: a readiness reversal restores the draft selection.
export function loadRemediationPlan(scope: CurriculumScope): RemediationPlan | null {
  return readRemediationPlanState(scope).effective;
}
