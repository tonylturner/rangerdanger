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
type ImplementationMap = Record<string, string[]>;

const MAPPINGS: Record<string, { label: string; impl: ImplementationMap }> = {
  "enterprise-to-field": {
    label: "Enterprise → Field on Modbus/DNP3",
    impl: {
      "BLOCK":         ["block-enterprise-to-field"],
      "BLOCK and LOG": ["block-enterprise-to-field", "improve-logging"],
    },
  },
  "enterprise-to-ot": {
    label: "Enterprise → OT Operations on ICS ports",
    impl: {
      "BLOCK":         ["block-enterprise-to-ot"],
      "BLOCK and LOG": ["block-enterprise-to-ot", "improve-logging"],
    },
  },
  "vendor-to-ot": {
    label: "Vendor DMZ → OT Operations on Modbus/HMI control",
    impl: {
      "BLOCK":         ["restrict-vendor-to-ot", "block-vendor-to-field"],
      "BLOCK and LOG": ["restrict-vendor-to-ot", "block-vendor-to-field", "improve-logging"],
      "RESTRICT":      ["restrict-vendor-to-ot"],
    },
  },
  "non-rtac-to-field": {
    label: "Non-RTAC OT hosts → Field devices",
    impl: {
      "BLOCK":         ["pin-rtac-to-field"],
      "BLOCK and LOG": ["pin-rtac-to-field", "improve-logging"],
    },
  },
  "unauth-modbus-writes": {
    label: "Unauthorized Modbus writes / DNP3 Direct Operate",
    impl: {
      "BLOCK":         ["modbus-dpi", "dnp3-dpi"],
      "BLOCK and LOG": ["modbus-dpi", "dnp3-dpi", "improve-logging"],
      "LOG":           ["improve-logging"],
    },
  },
};

// Storage key matches DecisionBlock.decisionStorageKey() in the
// scenario-runner. Lab 1.3's scenario id is "segmentation-
// requirements" — that's where the committed verdicts live.
const REQ_SCENARIO = "segmentation-requirements";

export function readRequirements(): Requirement[] {
  if (typeof window === "undefined") return [];
  return Object.keys(MAPPINGS).map((id) => {
    let verdict = "";
    try {
      verdict = window.localStorage.getItem(`decision:${REQ_SCENARIO}:${id}`) ?? "";
    } catch {
      /* localStorage blocked — return empty */
    }
    return { id, label: MAPPINGS[id].label, verdict };
  });
}

// What 1.3 requirement(s) does this 1.4 action satisfy, given the
// student's stated verdicts? Returns an empty list if the action
// is in the "operational hygiene" bucket (no design-driven need).
export function actionImplements(actionId: string, requirements: Requirement[]): Requirement[] {
  const out: Requirement[] = [];
  for (const req of requirements) {
    const m = MAPPINGS[req.id];
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

export function computeCoverage(requirements: Requirement[], selected: Set<string>): CoverageItem[] {
  return requirements.map((req): CoverageItem => {
    if (!req.verdict) {
      return { req, status: "n/a", reason: "no verdict committed" };
    }
    if (req.verdict === "ALLOW") {
      // Design said permit — nothing to address. Not a gap.
      return { req, status: "n/a", reason: "design says ALLOW (no remediation required)" };
    }
    const m = MAPPINGS[req.id];
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

// ─── Resourcing readiness (Option B — observational) ───────────────────
//
// Lab 1.3's resourcing-reality-check step captures four readiness
// answers (OT engineering capacity, firewall admin skills, vendor
// change windows, risk-acceptance authority). They live under the
// same `decision:segmentation-requirements:*` localStorage prefix.
//
// Lab 1.4 reads them and renders per-action overlays: actions that
// depend on a resource the student marked as constrained get a
// "warn" or "blocked" flag with a tooltip explaining the dependency.
// Actions still selectable — this is observational, not enforcing.
// Issue #27 tracks the dynamic-budget version (Option C) where
// readiness answers actually shrink the labor budget.

export type ReadinessKey =
  | "ot-eng-availability"
  | "fw-admin-skills"
  | "vendor-change-window"
  | "risk-acceptance-authority";

export type ReadinessAnswer = {
  key: ReadinessKey;
  label: string;             // human-readable for UI
  verdict: string;           // one of the dropdown options, or "" if unset
  good: boolean;             // true if verdict is the "happy path" option
};

const READINESS_LABELS: Record<ReadinessKey, string> = {
  "ot-eng-availability":     "OT engineering capacity",
  "fw-admin-skills":         "Firewall admin skills",
  "vendor-change-window":    "Vendor change windows",
  "risk-acceptance-authority": "Risk-acceptance authority",
};

const READINESS_GOOD_VERDICT: Record<ReadinessKey, string> = {
  "ot-eng-availability":     "AVAILABLE",
  "fw-admin-skills":         "IN_HOUSE",
  "vendor-change-window":    "SECURED",
  "risk-acceptance-authority": "CLEAR",
};

export function readReadiness(): ReadinessAnswer[] {
  if (typeof window === "undefined") return [];
  const keys: ReadinessKey[] = [
    "ot-eng-availability",
    "fw-admin-skills",
    "vendor-change-window",
    "risk-acceptance-authority",
  ];
  return keys.map((k) => {
    let v = "";
    try {
      v = window.localStorage.getItem(`decision:${REQ_SCENARIO}:${k}`) ?? "";
    } catch { /* localStorage blocked */ }
    return {
      key: k,
      label: READINESS_LABELS[k],
      verdict: v,
      good: v !== "" && v === READINESS_GOOD_VERDICT[k],
    };
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
): ReadinessFlag[] {
  const out: ReadinessFlag[] = [];
  const by = (k: ReadinessKey) => readiness.find((r) => r.key === k);

  // ── OT engineering capacity ───────────────────────────────────
  // Actions that require OT Engineer time on the spec.
  const otEng = by("ot-eng-availability");
  if (otEng && !otEng.good && otEng.verdict !== "") {
    const otTouching = new Set([
      "restrict-vendor-to-ot",
      "block-vendor-to-field",
      "pin-rtac-to-field",
      "modbus-dpi",
      "dnp3-dpi",
      "rtac-architecture-review",
      "positive-validation",
    ]);
    if (otTouching.has(actionId)) {
      const blocked = otEng.verdict === "UNAVAILABLE";
      out.push({
        severity: blocked ? "blocked" : "warn",
        source: "ot-eng-availability",
        sourceLabel: otEng.label,
        verdict: otEng.verdict,
        message: blocked
          ? "OT Engineer marked UNAVAILABLE — this action can't ship without OT review."
          : "OT Engineer marked STRETCHED — expect this to take longer than the budget shows.",
      });
    }
  }

  // ── Firewall admin skills ─────────────────────────────────────
  // DPI actions and policy work need firewall expertise.
  const fwSkill = by("fw-admin-skills");
  if (fwSkill && !fwSkill.good && fwSkill.verdict !== "") {
    const dpiHeavy = new Set(["modbus-dpi", "dnp3-dpi"]);
    const policyHeavy = new Set([
      "block-enterprise-to-field",
      "block-enterprise-to-ot",
      "restrict-vendor-to-ot",
      "block-vendor-to-field",
      "pin-rtac-to-field",
      "tighten-firewall-objects",
    ]);
    if (dpiHeavy.has(actionId)) {
      const gap = fwSkill.verdict === "GAP";
      out.push({
        severity: gap ? "blocked" : "warn",
        source: "fw-admin-skills",
        sourceLabel: fwSkill.label,
        verdict: fwSkill.verdict,
        message: gap
          ? "Firewall admin skills marked GAP — DPI tuning can't ship without hiring or training first."
          : "Firewall admin skills are vendor-led — DPI tuning will need vendor cycles, not just internal time.",
      });
    } else if (policyHeavy.has(actionId) && fwSkill.verdict === "GAP") {
      out.push({
        severity: "warn",
        source: "fw-admin-skills",
        sourceLabel: fwSkill.label,
        verdict: fwSkill.verdict,
        message: "Firewall admin skills marked GAP — basic policy work needs hiring or training to land cleanly.",
      });
    }
  }

  // ── Vendor change windows ─────────────────────────────────────
  // Actions that touch vendor systems need scheduled outage windows.
  const vendor = by("vendor-change-window");
  if (vendor && !vendor.good && vendor.verdict !== "") {
    const vendorTouching = new Set([
      "restrict-vendor-to-ot",
      "block-vendor-to-field",
    ]);
    if (vendorTouching.has(actionId)) {
      const notDiscussed = vendor.verdict === "NOT_DISCUSSED";
      out.push({
        severity: notDiscussed ? "blocked" : "warn",
        source: "vendor-change-window",
        sourceLabel: vendor.label,
        verdict: vendor.verdict,
        message: notDiscussed
          ? "Vendor change window NOT_DISCUSSED — this action can't ship until that conversation happens."
          : "Vendor change window NEGOTIATING — schedule slip is likely.",
      });
    }
  }

  // ── Risk-acceptance authority ─────────────────────────────────
  // High-impact denies need a clear signoff path.
  const risk = by("risk-acceptance-authority");
  if (risk && !risk.good && risk.verdict !== "") {
    const highImpactDeny = new Set([
      "block-enterprise-to-field",
      "block-enterprise-to-ot",
      "block-vendor-to-field",
      "pin-rtac-to-field",
    ]);
    if (highImpactDeny.has(actionId)) {
      const disputed = risk.verdict === "DISPUTED";
      out.push({
        severity: disputed ? "blocked" : "warn",
        source: "risk-acceptance-authority",
        sourceLabel: risk.label,
        verdict: risk.verdict,
        message: disputed
          ? "Risk-acceptance authority DISPUTED — denying production flows without a clear signoff path is unsafe."
          : "Risk-acceptance authority UNCLEAR — get the signoff path nailed down before applying this rule.",
      });
    }
  }

  return out;
}
