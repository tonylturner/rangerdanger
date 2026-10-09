// Browser storage for curriculum state: lab decisions, exercise
// progress, the remediation plan and the firewall track.
//
// Every key is `rd:<package>:<revision>:<name>`. Scoping by package
// keeps two curricula from reading each other's answers; scoping by
// curriculum revision means a revision bump (step IDs or validator
// meaning changed) starts students fresh instead of misreading old
// records. Keys written before scoping existed are never read.
//
// This module is the only place that builds a key or touches
// localStorage for curriculum state; every reader and writer goes
// through it. Decision names are part of the lab authoring contract:
// scenario-decision-graph.test.ts walks every scenario YAML and asserts
// each `:::findings-panel from=` and `default-from=` reference resolves
// to a real `:::decision id=`, so renaming a decision id without
// moving its readers fails that test.

export type CurriculumScope = { packageId: string; revision: number };

export function curriculumKey(scope: CurriculumScope, name: string): string {
  return `rd:${scope.packageId}:${scope.revision}:${name}`;
}

// A `:::decision id=<decisionId>` answer recorded in <scenarioId>. The
// step title is deliberately not part of the name so renaming a step
// does not orphan the answer.
export function decisionName(scenarioId: string, decisionId: string): string {
  return `decision:${scenarioId}:${decisionId}`;
}

// Completed step IDs, notes and command log for one exercise.
export function exerciseName(scenarioId: string): string {
  return `exercise:${scenarioId}`;
}

// The Lab 1.4 remediation plan, read again by the later firewall labs.
export const REMEDIATION_PLAN = "remediation-plan";

// The Guided / Advanced firewall track chosen in Lab 2.2.
export const FIREWALL_TRACK = "firewall-track";

function browserStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    // Access throws when the browser blocks storage; state stays in memory.
    return null;
  }
}

export function readCurriculum(scope: CurriculumScope, name: string): string | null {
  try {
    return browserStorage()?.getItem(curriculumKey(scope, name)) ?? null;
  } catch {
    return null;
  }
}

export function writeCurriculum(scope: CurriculumScope, name: string, value: string): void {
  try {
    browserStorage()?.setItem(curriculumKey(scope, name), value);
  } catch {
    // Quota or privacy mode: the UI keeps working without persistence.
  }
}

export function removeCurriculum(scope: CurriculumScope, name: string): void {
  try {
    browserStorage()?.removeItem(curriculumKey(scope, name));
  } catch {
    // Same as writeCurriculum.
  }
}
