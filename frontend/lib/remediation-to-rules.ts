// Package-owned advisory planning. No binding means no planner.
import { actionEffectLabel, loadRemediationPlan, type RemediationPlan } from "./remediation-plan";
import type { CurriculumScope } from "./curriculum-storage";
import type { CurriculumBinding, FirewallRule } from "./api";

export type RuleGroup = {
  remediationId: string;
  title: string;
  description: string;
  rules: FirewallRule[];
};

export type ActionSummary = {
  id: string;
  title: string;
  description: string;
  hasRules: boolean;
};

export type DynamicExercisePlan = {
  hasRemediationPlan: boolean;
  selectedActions: string[];
  baselineRules: FirewallRule[];
  remediationGroups: RuleGroup[];
  enterpriseToVendorRule: FirewallRule | null;
  allRules: FirewallRule[];
  enableLogging: boolean;
  tightenObjects: boolean;
  includeValidation: boolean;
  includeDpi: boolean;
  selectedSummary: ActionSummary[];
  unselectedSummary: ActionSummary[];
};

// Keep binding provenance out of the serializable historical plan DTO.
const bindings = new WeakMap<DynamicExercisePlan, CurriculumBinding>();
export function curriculumForPlan(plan: DynamicExercisePlan): CurriculumBinding {
  const binding = bindings.get(plan);
  if (!binding) throw new Error("Dynamic plan has no curriculum binding");
  return binding;
}

export function buildDynamicPlan(plan: RemediationPlan | null, curriculum: CurriculumBinding): DynamicExercisePlan {
  const selected = plan?.selectedActionIds ?? [];
  const hasRemediationPlan = plan !== null && selected.length > 0;
  const allowed = (id: string) => curriculum.icsRules !== "forbidden" || curriculum.groups[id]?.effect !== "semantic";
  const groups: RuleGroup[] = [];
  for (const actionId of selected) {
    const mapping = curriculum.groups[actionId];
    if (mapping && mapping.rules.length > 0 && allowed(actionId)) {
      // Do not leak effect metadata into the historical plan DTO.
      groups.push({ remediationId: actionId, title: mapping.title, description: mapping.description, rules: mapping.rules });
    }
  }
  if (!curriculum.legacyEffects) {
    for (const id of curriculum.groupOrder) {
      const group = curriculum.groups[id];
      if (!selected.includes(id) && group.omittedRules?.length) {
        groups.push({ remediationId: id, title: group.title, description: group.description, rules: group.omittedRules });
      }
    }
  }
  const enableLogging = selected.includes(curriculum.flags.logging);
  const baseline = curriculum.legacyDisplayMutation ? curriculum.baselineRules : structuredClone(curriculum.baselineRules);
  let allRules = [...baseline, ...(curriculum.enterpriseToVendorRule ? [curriculum.enterpriseToVendorRule] : []), ...groups.flatMap((g) => g.rules)];
  if (!curriculum.legacyDisplayMutation) allRules = structuredClone(allRules);
  if (curriculum.icsRules === "forbidden") for (const rule of allRules) delete rule.ics;
  if (enableLogging) for (const rule of allRules) {
    if (rule.action === "DENY" || (curriculum.legacyDisplayMutation ? curriculum.loggingPrefixes.some((prefix) => rule.id.startsWith(prefix)) : rule.action === "ALLOW")) rule.log = true;
  }
  const summarise = (id: string): ActionSummary => {
    const m = curriculum.groups[id];
    const label = actionEffectLabel(curriculum, id);
    return {
      id, title: m?.title ?? id,
      description: (m?.description ?? "") + (label ? ` — ${label}` : ""),
      hasRules: allowed(id) && (m?.rules.length ?? 0) > 0,
    };
  };
  const result: DynamicExercisePlan = {
    hasRemediationPlan, selectedActions: selected, baselineRules: baseline, remediationGroups: groups,
    enterpriseToVendorRule: curriculum.enterpriseToVendorRule, allRules, enableLogging,
    tightenObjects: selected.includes(curriculum.flags.objects), includeValidation: selected.includes(curriculum.flags.validation),
    includeDpi: curriculum.flags.dpi.some((id) => selected.includes(id)),
    selectedSummary: selected.map(summarise),
    unselectedSummary: curriculum.groupOrder.filter((id) => !selected.includes(id)).map(summarise),
  };
  bindings.set(result, curriculum);
  return result;
}

export function loadDynamicPlan(scope: CurriculumScope): DynamicExercisePlan | null {
  return scope.curriculum ? buildDynamicPlan(loadRemediationPlan(scope), scope.curriculum) : null;
}

// Generate the markdown rule table for Exercise 3 Phase 3.
export function renderRuleTable(plan: DynamicExercisePlan): string {
  const rows = plan.allRules.map((r, i) => {
    const logCol = plan.enableLogging ? (r.log ? " Yes" : " —") : "";
    const icsCol = r.ics ? ` ${r.ics.note || r.ics.protocol}` : "";
    return `| ${i + 1} | \`${r.source}\` | \`${r.destination}\` | ${r.proto} | ${r.port} | ${r.action} | ${r.purpose} |${logCol}${icsCol}`;
  });

  const logHeader = plan.enableLogging ? " Log |" : "";
  const logSep = plan.enableLogging ? " --- |" : "";

  return [
    `| # | Source | Destination | Proto | Port(s) | Action | Purpose |${logHeader}`,
    `|---|--------|-------------|-------|---------|--------|---------|${logSep}`,
    ...rows,
  ].join("\n");
}

function validationTests(plan: DynamicExercisePlan, kind: "positive" | "negative"): string[] {
  const c = curriculumForPlan(plan);
  const tests = c.tests.filter((t) => t.kind === kind && (!t.when || plan.selectedActions.includes(t.when))).map((t) => t.text);
  if (!tests.length && kind === "negative") tests.push(c.negativeEmpty);
  return tests;
}
export function positiveValidationTests(plan: DynamicExercisePlan): string[] { return validationTests(plan, "positive"); }
export function negativeValidationTests(plan: DynamicExercisePlan): string[] { return validationTests(plan, "negative"); }
