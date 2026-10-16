// requirement-coverage.ts — bridge between Lab 1.3 and Lab 1.4.
//
// Lab 1.3 captures five "design requirements" via :::decision blocks
// (BLOCK / BLOCK and LOG / RESTRICT / ALLOW). Lab 1.4 lets the
// student pick a slate of remediation actions under a labor budget.
// This module maps between them so 1.4 can:
//
//   - render the student's stated requirements as context at the top
//     of the decision panel (read-only — they're a constraint, not
//     a new input)
//   - tag each 1.4 action with which 1.3 requirement it implements
//     (so students can see why a specific action is in the catalog)
//   - compute coverage at the bottom: for each non-ALLOW requirement,
//     is at least one implementing action selected? if not, show
//     the gap explicitly
//
// 1.4 actions that don't map to any 1.3 requirement (e.g.
// rtac-architecture-review, tighten-firewall-objects, positive-
// validation, improve-logging-as-standalone) stay in the catalog —
// they're "operational hygiene" beyond what the design spec requires
// and the labor budget still constrains their selection.

import type { CurriculumBinding } from "./api";
import { decisionName, readCurriculum, type CurriculumScope } from "./curriculum-storage";

export type Requirement = {
  id: string;            // matches DecisionBlock decisionId
  label: string;         // human-readable for UI (e.g. "Enterprise → Field")
  verdict: string;       // BLOCK / BLOCK and LOG / RESTRICT / ALLOW / "" if unset
};

// One 1.3 requirement → the 1.4 actions that implement it, keyed
// by verdict. ALLOW deliberately has no implementing actions
// (because the design said "permit" — student may still choose
// hygiene actions, but they don't satisfy a requirement). Verdicts
// not listed (e.g. RESTRICT for enterprise-to-field, which doesn't
// make engineering sense) fall through to "no mapped actions" —
// the requirement is then marked "uncertain" in coverage rather
// than gap, since the verdict itself is unusual.
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

// What 1.3 requirement(s) does this 1.4 action satisfy, given the
// student's stated verdicts? Returns an empty list if the action
// is in the "operational hygiene" bucket (no design-driven need).
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
    if (curriculum?.icsRules === "forbidden" && got.some((id) => curriculum.groups[id]?.effect === "semantic")) {
      return { req, status: "n/a", reason: "planned, not enforceable in this range yet" };
    }
    if (!curriculum?.legacyEffects && got.some((id) => curriculum?.groups[id]?.effect === "conceptual")) {
      return { req, status: "n/a", reason: "planning-only (no policy effect)" };
    }
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

// ─── Resourcing readiness (Option B — observational) ───────────────────
//
// Lab 1.3's resourcing-reality-check step captures four readiness
// answers (OT engineering capacity, firewall admin skills, vendor
// change windows, risk-acceptance authority). They live under the
// same `decision:segmentation-requirements:*` curriculum storage names.
//
// Lab 1.4 reads them and renders per-action overlays: actions that
// depend on a resource the student marked as constrained get a
// "warn" or "blocked" flag with a tooltip explaining the dependency.
// Actions still selectable — this is observational, not enforcing.
// Issue #27 tracks the dynamic-budget version (Option C) where
// readiness answers actually shrink the labor budget.

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

// readinessFlagsForAction inspects the four readiness answers and
// returns the set of overlays that should attach to a given 1.4
// action. "blocked" means the action is dependent on a precondition
// the student says doesn't exist yet (e.g. vendor change window not
// even discussed). "warn" means the action is harder than the labor
// budget alone would suggest. Both are visible — neither stops the
// student from selecting the action.
export function readinessFlagsForAction(
  actionId: string,
  readiness: ReadinessAnswer[],
  curriculum?: CurriculumBinding,
): ReadinessFlag[] {
  return (curriculum?.readinessFlags ?? []).filter((flag) =>
    flag.actions.includes(actionId) && readiness.some((r) => r.key === flag.source && !r.good && r.verdict === flag.verdict),
  ).map((flag) => ({ severity: flag.severity, source: flag.source, sourceLabel: flag.sourceLabel, verdict: flag.verdict, message: flag.message }));
}
