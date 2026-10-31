// Exercise progress persistence. Completed steps are recorded by
// authored step ID, never by array position, so reordering or
// inserting steps cannot shift a student's progress onto the wrong
// step. IDs the scenario no longer has are dropped on load.

import type { Scenario } from "./api";
import {
  exerciseName,
  readCurriculum,
  removeCurriculum,
  writeCurriculum,
  type CurriculumScope,
} from "./curriculum-storage";

export type ExerciseProgress = {
  completedStepIds: string[];
  notes: string;
  cmdLog: string[];
};

type ProgressScenario = Pick<Scenario, "id" | "steps">;

function emptyProgress(): ExerciseProgress {
  return { completedStepIds: [], notes: "", cmdLog: [] };
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
  return {
    completedStepIds: [...new Set(stringArray(record.completedStepIds))].filter((id) => stepIds.has(id)),
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

export function completionPercent(scope: CurriculumScope, scenario: ProgressScenario): number {
  if (scenario.steps.length === 0) return 0;
  const done = loadProgress(scope, scenario).completedStepIds.length;
  return Math.round((done / scenario.steps.length) * 100);
}
