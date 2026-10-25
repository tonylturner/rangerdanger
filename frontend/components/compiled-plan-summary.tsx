"use client";

import type { CompiledPlan, CurriculumBinding, PlanProvenance } from "../lib/api";
import { actionEffectLabel, actionName } from "../lib/plan-presentation";
import { planLabel } from "../lib/policy-wording";

export function CompiledPlanSummary({ plan, binding, appliedPlan }: { plan: CompiledPlan; binding: CurriculumBinding; appliedPlan?: PlanProvenance }) {
  return (
    <div className="space-y-3 rounded-lg border border-sky-800/50 bg-sky-950/20 p-4 text-xs text-slate-300">
      <div className="font-semibold text-sky-300">Your plan — {planLabel(plan.label)}</div>
      {plan.label === "weak" && <p>Weak exposures remain open. Applying this plan restores the Weak exposure verdicts.</p>}
      <ul className="space-y-2">
        {plan.actions.map((a) => <li key={a.id}>
          <span className="font-semibold">{actionName(a.id, binding)}</span>
          {" — "}{a.chosen ? appliedPlan?.actionIds.includes(a.id) ? "applied in your plan" : "planned" : "deferred"}
          {" — "}{actionEffectLabel(a.kind)}
          {!a.chosen && a.kind === "enforce" && "; Weak exposure open"}
        </li>)}
      </ul>
      <p>The L4 source pin enforces; DPI detects and logs. Detection changes no verdict.</p>
      <a className="inline-block text-sky-300 underline" download={`${plan.packageId}-plan.json`}
        href={`data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(plan.config, null, 2))}`}>Export compiled config</a>
    </div>
  );
}
