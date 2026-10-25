// Package-owned requirements connect saved design verdicts to planning actions.
// The binding supplies labels, verdict-to-action mappings and effect semantics;
// coverage reports full, partial or missing planned selections, not prevention.

import type { CurriculumBinding } from "./api";
import { decisionName, readCurriculum, type CurriculumScope } from "./curriculum-storage";

export type Requirement = {
  id: string;            // matches DecisionBlock decisionId
  label: string;         // human-readable for UI (e.g. "Enterprise → Field")
  verdict: string;       // BLOCK / BLOCK and LOG / RESTRICT / ALLOW / "" if unset
};

// Read decisions from the requirements-role scenario in the active package.
function readVerdict(scope: CurriculumScope, decisionId: string): string {
  const scenario = scope.curriculum?.roles.requirements;
  return scenario ? readCurriculum(scope, decisionName(scenario, decisionId)) ?? "" : "";
}

export function readRequirements(scope: CurriculumScope): Requirement[] {
  if (typeof window === "undefined") return [];
  return (scope.curriculum?.requirementOrder ?? []).map((id) => ({
    id,
    label: scope.curriculum!.requirements[id].label,
    verdict: readVerdict(scope, id),
  }));
}

// Find requirements mapped to this action for the student's saved verdicts.
export function actionImplements(actionId: string, requirements: Requirement[], curriculum?: CurriculumBinding): Requirement[] {
  const out: Requirement[] = [];
  for (const req of requirements) {
    const m = curriculum?.requirements[req.id];
    if (!m) continue;
    const actions = m.impl[req.verdict] ?? [];
    if (actions.includes(actionId)) {
      out.push(req);
    }
  }
  return out;
}

export type CoverageItem =
  | { req: Requirement; status: "covered"; selectedActions: string[] }
  | { req: Requirement; status: "partial"; selectedActions: string[]; missingActions: string[] }
  | { req: Requirement; status: "gap"; expectedActions: string[] }
  | { req: Requirement; status: "n/a"; reason: string };

export function computeCoverage(requirements: Requirement[], selected: Set<string>, curriculum?: CurriculumBinding): CoverageItem[] {
  return requirements.map((req): CoverageItem => {
    if (!req.verdict) {
      return { req, status: "n/a", reason: "no verdict committed" };
    }
    if (req.verdict === "ALLOW") {
      // Design said permit — nothing to address. Not a gap.
      return { req, status: "n/a", reason: "design says ALLOW (no remediation required)" };
    }
    const m = curriculum?.requirements[req.id];
    const expected = m?.impl[req.verdict] ?? [];
    if (expected.length === 0) {
      return { req, status: "n/a", reason: `no remediation actions map to verdict "${req.verdict}"` };
    }
    const got = expected.filter((a) => selected.has(a));
    if (got.length === expected.length) {
      return { req, status: "covered", selectedActions: got };
    }
    if (got.length === 0) {
      return { req, status: "gap", expectedActions: expected };
    }
    return {
      req,
      status: "partial",
      selectedActions: got,
      missingActions: expected.filter((a) => !selected.has(a)),
    };
  });
}

export function summariseCoverage(items: CoverageItem[]): {
  total: number;
  covered: number;
  partial: number;
  gap: number;
  na: number;
} {
  const out = { total: items.length, covered: 0, partial: 0, gap: 0, na: 0 };
  for (const i of items) {
    if (i.status === "covered") out.covered++;
    else if (i.status === "partial") out.partial++;
    else if (i.status === "gap") out.gap++;
    else out.na++;
  }
  return out;
}

// Readiness answers and action overlays are defined by the active binding.
// Flags are observational: they do not change the budget or prevent selection.

export type ReadinessKey = string;

export type ReadinessAnswer = {
  key: ReadinessKey;
  label: string;             // human-readable for UI
  verdict: string;           // one of the dropdown options, or "" if unset
  good: boolean;             // true if verdict is the "happy path" option
};

export function readReadiness(scope: CurriculumScope): ReadinessAnswer[] {
  if (typeof window === "undefined") return [];
  return (scope.curriculum?.readiness ?? []).map((r) => {
    const verdict = readVerdict(scope, r.id);
    return { key: r.id, label: r.label, verdict, good: verdict !== "" && verdict === r.good };
  });
}

export type ReadinessFlag = {
  severity: "warn" | "blocked";
  source: ReadinessKey;
  sourceLabel: string;
  verdict: string;
  message: string;
};

// Match package-authored overlays against the student's readiness answers.
export function readinessFlagsForAction(
  actionId: string,
  readiness: ReadinessAnswer[],
  curriculum?: CurriculumBinding,
): ReadinessFlag[] {
  return (curriculum?.readinessFlags ?? []).filter((flag) =>
    flag.actions.includes(actionId) && readiness.some((r) => r.key === flag.source && !r.good && r.verdict === flag.verdict),
  ).map((flag) => ({ severity: flag.severity, source: flag.source, sourceLabel: flag.sourceLabel, verdict: flag.verdict, message: flag.message }));
}
