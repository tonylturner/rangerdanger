"use client";

import { useState } from "react";
import { ApiError, applyPlan, type CompiledPlan } from "../lib/api";
import { planLabel } from "../lib/policy-wording";

export function ApplyPlanButton({ plan, onApplied }: { plan: CompiledPlan | null; onApplied: () => void }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const apply = async () => {
    if (!plan) return;
    setBusy(true);
    setMessage("");
    setWarnings([]);
    try {
      const result = await applyPlan({ packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id) });
      setWarnings(result.warnings);
      setMessage(result.applied ? `Applied your plan — ${planLabel(result.plan.label)}` : "Plan was not applied");
      if (result.applied) onApplied();
    } catch (e) {
      setMessage(e instanceof Error ? e.message : "Could not apply plan");
      if (e instanceof ApiError) setWarnings(e.warnings);
    } finally {
      setBusy(false);
    }
  };
  return <div className="text-xs">
    <button disabled={!plan || busy} aria-busy={busy} title={!plan ? "Waiting for the backend to compile your plan" : undefined} onClick={apply}
      className="rounded-sm border border-sky-700 bg-sky-950/40 px-2 py-1 text-[10px] font-medium text-sky-300 hover:bg-sky-900/50 disabled:opacity-50">
      {busy ? "Applying…" : "Apply Your Plan"}{plan ? ` — ${planLabel(plan.label)}` : ""}
    </button>
    <div role="status" className="mt-1 text-slate-300">{message}</div>
    {warnings.length > 0 && <ul aria-label="Plan warnings" className="mt-1 text-amber-300">{warnings.map((w, i) => <li key={i}>{w}</li>)}</ul>}
  </div>;
}
