// Storage-key contract for the lab decision flow.
//
// Lab 1.2 / 1.3 / 1.4 step bodies use `:::decision id=<X>` fences
// that DecisionBlock persists to localStorage. Downstream labs
// (1.3 / 1.4 / 2.4) read those answers via `:::findings-panel
// from=<scenarioId>` and `default-from=<scenarioId>:<decisionId>`
// fences.
//
// This module is the single source of truth for the storage key
// shape. If the format ever changes, every reader and writer must
// move together — that's why the helper is extracted out of the
// component file. The contract test in
// scenario-decision-graph.test.ts walks every scenario YAML and
// asserts every fence reference resolves to a real definition;
// renaming a decision id without updating the downstream
// references would break that test.

export function decisionStorageKey(scenarioId: string, decisionId: string): string {
  return `decision:${scenarioId}:${decisionId}`;
}

// Storage key for the Lab 1.4 remediation plan. PlanCoveragePanel
// (Lab 2.4) reads via this key. Co-located here so the storage
// surface is documented in one place.
export const REMEDIATION_PLAN_STORAGE_KEY = "rd-remediation-plan";
