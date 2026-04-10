"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ClipboardList } from "lucide-react";
import { loadRemediationPlan, ACTION_IMPACT, type RemediationPlan } from "../lib/remediation-plan";

type RemediationPlanBannerProps = {
  currentExerciseId: string;
};

// RemediationPlanBanner renders at the top of later exercises to show which
// remediation actions the student chose in Exercise 1 — and specifically
// which of those actions affect the current exercise.
//
// Hidden entirely if:
//   - no plan has been saved yet
//   - the current exercise is the planning exercise itself
//   - the current exercise is the baseline (no plan yet)
export function RemediationPlanBanner({ currentExerciseId }: RemediationPlanBannerProps) {
  const [plan, setPlan] = useState<RemediationPlan | null>(null);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    setPlan(loadRemediationPlan());
  }, [currentExerciseId]);

  // Hide on baseline and on the planning exercise itself
  if (currentExerciseId === "baseline-assessment") return null;
  if (currentExerciseId === "remediation-planning") return null;

  if (!plan || plan.selectedActionIds.length === 0) {
    return (
      <div className="rounded-lg border border-amber-900/50 bg-amber-950/20 px-4 py-3">
        <div className="flex items-center gap-2">
          <ClipboardList className="h-4 w-4 text-amber-500" />
          <div className="text-[11px] text-amber-400">
            No remediation plan on file. Complete the{" "}
            <Link href="/exercises" className="underline hover:text-amber-200">
              Remediation Planning
            </Link>{" "}
            exercise first — later exercises reference your plan.
          </div>
        </div>
      </div>
    );
  }

  // Build the relevance list — actions in the plan that defend against
  // the current exercise (per ACTION_IMPACT mapping).
  const relevantSelected: { id: string; note: string }[] = [];
  const relevantMissed: { id: string; note: string }[] = [];
  for (const [actionId, impact] of Object.entries(ACTION_IMPACT)) {
    if (!impact.defends.includes(currentExerciseId)) continue;
    if (plan.selectedActionIds.includes(actionId)) {
      relevantSelected.push({ id: actionId, note: impact.note });
    } else {
      relevantMissed.push({ id: actionId, note: impact.note });
    }
  }

  const hasRelevance = relevantSelected.length > 0 || relevantMissed.length > 0;

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 px-4 py-3">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-2"
      >
        <div className="flex items-center gap-2">
          <ClipboardList className="h-4 w-4 text-sky-400" />
          <div className="text-[11px] font-bold uppercase tracking-wider text-slate-400">
            Your Remediation Plan
          </div>
          <div className="text-[11px] text-slate-500">
            {plan.selectedActionIds.length} action{plan.selectedActionIds.length === 1 ? "" : "s"} selected
          </div>
          {hasRelevance && (
            <div
              className={`rounded px-1.5 py-0.5 text-[9px] font-bold ${
                relevantSelected.length > 0
                  ? "bg-green-950 text-green-400 border border-green-800"
                  : "bg-red-950 text-red-400 border border-red-800"
              }`}
            >
              {relevantSelected.length > 0
                ? `${relevantSelected.length} defense${relevantSelected.length === 1 ? "" : "s"} active`
                : "no defenses for this attack"}
            </div>
          )}
        </div>
        <div className="text-[10px] text-slate-600">{expanded ? "hide" : "show"}</div>
      </button>

      {expanded && hasRelevance && (
        <div className="mt-3 space-y-2 border-t border-slate-800 pt-3">
          {relevantSelected.length > 0 && (
            <div>
              <div className="text-[9px] font-bold uppercase tracking-wider text-green-600 mb-1">
                Active defenses for this exercise
              </div>
              <ul className="space-y-1">
                {relevantSelected.map((r) => (
                  <li key={r.id} className="text-[11px] text-slate-400 pl-3 border-l-2 border-green-700">
                    {r.note}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {relevantMissed.length > 0 && (
            <div>
              <div className="text-[9px] font-bold uppercase tracking-wider text-red-600 mb-1">
                Missed — attack paths remain open
              </div>
              <ul className="space-y-1">
                {relevantMissed.map((r) => (
                  <li key={r.id} className="text-[11px] text-slate-400 pl-3 border-l-2 border-red-900">
                    {r.note}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {expanded && !hasRelevance && (
        <div className="mt-3 border-t border-slate-800 pt-3 text-[11px] text-slate-500">
          No remediation actions in your plan directly address this exercise.
        </div>
      )}
    </div>
  );
}
