// Persist progress independently of the last historical validation result.
// Completed/skipped steps use authored IDs, never array positions, so edits
// cannot shift a student's progress onto another step. Removed IDs are dropped.

import type { Scenario, ValidationCheck, ValidationResult } from "./api";
import {
  exerciseName,
  readCurriculum,
  removeCurriculum,
  writeCurriculum,
  type CurriculumScope,
} from "./curriculum-storage";

export type ExerciseProgress = {
  completedStepIds: string[];
  skippedStepIds: string[];
  notes: string;
  cmdLog: string[];
};

type ProgressScenario = Pick<Scenario, "id" | "steps">;

function emptyProgress(): ExerciseProgress {
  return { completedStepIds: [], skippedStepIds: [], notes: "", cmdLog: [] };
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === "string") : [];
}

export function loadProgress(scope: CurriculumScope, scenario: ProgressScenario): ExerciseProgress {
  const raw = readCurriculum(scope, exerciseName(scenario.id));
  if (!raw) return emptyProgress();
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return emptyProgress();
  }
  if (typeof parsed !== "object" || parsed === null) return emptyProgress();
  const record = parsed as Record<string, unknown>;
  const stepIds = new Set(scenario.steps.map((s) => s.id));
  const completedStepIds = [...new Set(stringArray(record.completedStepIds))].filter((id) => stepIds.has(id));
  return {
    completedStepIds,
    skippedStepIds: [...new Set(stringArray(record.skippedStepIds))]
      .filter((id) => stepIds.has(id) && !completedStepIds.includes(id)),
    notes: typeof record.notes === "string" ? record.notes : "",
    cmdLog: stringArray(record.cmdLog),
  };
}

export function saveProgress(scope: CurriculumScope, scenarioId: string, progress: ExerciseProgress): void {
  writeCurriculum(scope, exerciseName(scenarioId), JSON.stringify(progress));
}

export function clearProgress(scope: CurriculumScope, scenarioId: string): void {
  removeCurriculum(scope, exerciseName(scenarioId));
}

// Outcome is a historical validator observation, not student completion. Keep a
// separate record so an explicit progress clear cannot erase or manufacture it.
function validationName(scenarioId: string): string {
  return `validation:${scenarioId}`;
}

function isCheck(value: unknown): value is ValidationCheck {
  if (typeof value !== "object" || value === null) return false;
  const check = value as Record<string, unknown>;
  return typeof check.name === "string" && typeof check.detail === "string"
    && (check.status === "pass" || check.status === "fail" || check.status === "warn");
}

export function loadValidation(scope: CurriculumScope, scenarioId: string): ValidationResult | null {
  const raw = readCurriculum(scope, validationName(scenarioId));
  if (!raw) return null;
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return null; }
  if (typeof parsed !== "object" || parsed === null) return null;
  const result = parsed as Record<string, unknown>;
  if (result.scenario_id !== scenarioId
    || (result.outcome !== "PASS" && result.outcome !== "FAIL" && result.outcome !== "PARTIAL")
    || typeof result.timestamp !== "string" || !Number.isFinite(Date.parse(result.timestamp))
    || !Array.isArray(result.checks) || !result.checks.every(isCheck)) return null;
  return {
    scenario_id: scenarioId, outcome: result.outcome,
    timestamp: result.timestamp, checks: result.checks,
  };
}

export function saveValidation(scope: CurriculumScope, result: ValidationResult): void {
  writeCurriculum(scope, validationName(result.scenario_id), JSON.stringify(result));
}

export function completionPercent(scope: CurriculumScope, scenario: ProgressScenario): number {
  if (scenario.steps.length === 0) return 0;
  const done = loadProgress(scope, scenario).completedStepIds.length;
  return Math.round((done / scenario.steps.length) * 100);
}
