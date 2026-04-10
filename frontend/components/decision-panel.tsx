"use client";

import { useEffect, useState, useMemo } from "react";
import { Check } from "lucide-react";
import type { StepAction, DecisionAction, DecisionRole } from "../lib/api";
import { saveRemediationPlan, loadRemediationPlan } from "../lib/remediation-plan";

type DecisionPanelProps = {
  exerciseId: string;
  action: StepAction;
};

// Effort split model: each required role consumes effort / roles.length hours.
// Rounded to nearest integer.
function hoursPerRole(action: DecisionAction): number {
  if (!action.roles || action.roles.length === 0) return action.effort_hours;
  return Math.round(action.effort_hours / action.roles.length);
}

export function DecisionPanel({ exerciseId, action }: DecisionPanelProps) {
  const budgetHours = action.budget_hours ?? 500;
  const roles: DecisionRole[] = useMemo(() => action.roles ?? [], [action.roles]);
  const catalog: DecisionAction[] = useMemo(() => action.actions ?? [], [action.actions]);

  // Load any previously saved plan for this exercise
  const [selected, setSelected] = useState<Set<string>>(() => {
    if (typeof window === "undefined") return new Set();
    const plan = loadRemediationPlan();
    if (plan && plan.exerciseId === exerciseId) {
      return new Set(plan.selectedActionIds);
    }
    return new Set();
  });

  // Persist on change
  useEffect(() => {
    if (typeof window === "undefined") return;
    saveRemediationPlan({
      exerciseId,
      selectedActionIds: [...selected],
      savedAt: new Date().toISOString(),
    });
  }, [selected, exerciseId]);

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // Computed totals
  const totalHours = [...selected].reduce((sum, id) => {
    const a = catalog.find((c) => c.id === id);
    return sum + (a?.effort_hours ?? 0);
  }, 0);
  const remaining = budgetHours - totalHours;
  const budgetPct = Math.min(100, Math.round((totalHours / budgetHours) * 100));
  const overBudget = totalHours > budgetHours;

  // Per-role usage
  const roleUsage: Record<string, number> = {};
  for (const role of roles) roleUsage[role.name] = 0;
  for (const id of selected) {
    const a = catalog.find((c) => c.id === id);
    if (!a) continue;
    const perRole = hoursPerRole(a);
    for (const r of a.roles) {
      if (roleUsage[r] !== undefined) roleUsage[r] += perRole;
    }
  }

  const overRoles = roles.filter((r) => roleUsage[r.name] > r.capacity_hours);

  return (
    <div className="mt-3 space-y-4">
      {/* Budget and role meters */}
      <div className="rounded-lg border border-slate-800 bg-slate-950 p-3">
        <div className="flex items-baseline justify-between mb-2">
          <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
            Labor Budget
          </div>
          <div className={`text-xs font-mono ${overBudget ? "text-red-400" : "text-slate-300"}`}>
            <span className="font-bold">{totalHours}</span>
            <span className="text-slate-600"> / {budgetHours} h</span>
            <span className="ml-2 text-slate-600">
              ({remaining >= 0 ? `${remaining} remaining` : `${-remaining} over`})
            </span>
          </div>
        </div>
        <div className="h-1.5 rounded-full bg-slate-800 overflow-hidden">
          <div
            className={`h-full rounded-full transition-all ${
              overBudget ? "bg-red-500" : budgetPct > 85 ? "bg-amber-500" : "bg-sky-500"
            }`}
            style={{ width: `${budgetPct}%` }}
          />
        </div>

        <div className="mt-3 grid grid-cols-2 md:grid-cols-4 gap-2">
          {roles.map((role) => {
            const used = roleUsage[role.name];
            const pct = Math.min(100, Math.round((used / role.capacity_hours) * 100));
            const over = used > role.capacity_hours;
            return (
              <div key={role.name} className="rounded border border-slate-800 bg-slate-900/60 p-2">
                <div className="text-[9px] uppercase tracking-wider text-slate-500 truncate" title={role.name}>
                  {role.name}
                </div>
                <div className={`text-xs font-mono ${over ? "text-red-400 font-bold" : "text-slate-300"}`}>
                  {used}
                  <span className="text-slate-600"> / {role.capacity_hours}h</span>
                </div>
                <div className="mt-1 h-1 rounded-full bg-slate-800 overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all ${
                      over ? "bg-red-500" : pct > 85 ? "bg-amber-500" : "bg-green-500"
                    }`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              </div>
            );
          })}
        </div>

        {(overBudget || overRoles.length > 0) && (
          <div className="mt-3 rounded border border-red-800/60 bg-red-950/30 px-3 py-2 text-[11px] text-red-300">
            {overBudget && <div>Over total budget by {totalHours - budgetHours} hours.</div>}
            {overRoles.length > 0 && (
              <div>
                Overloaded teams: {overRoles.map((r) => r.name).join(", ")}. Remove an action
                or pick one that doesn't require these roles.
              </div>
            )}
          </div>
        )}
      </div>

      {/* Action catalog */}
      <div className="space-y-2">
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
          Remediation Actions ({selected.size} selected of {catalog.length})
        </div>
        {catalog.map((a) => {
          const isSelected = selected.has(a.id);
          const perRole = hoursPerRole(a);
          return (
            <button
              key={a.id}
              onClick={() => toggle(a.id)}
              className={`w-full text-left rounded-lg border p-3 transition-all ${
                isSelected
                  ? "border-sky-600 bg-sky-950/40 ring-1 ring-sky-600/40"
                  : "border-slate-800 bg-slate-900/40 hover:border-slate-700 hover:bg-slate-900/70"
              }`}
            >
              <div className="flex items-start gap-3">
                <div
                  className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
                    isSelected ? "border-sky-500 bg-sky-600" : "border-slate-600 bg-slate-950"
                  }`}
                >
                  {isSelected && <Check className="h-3 w-3 text-white" strokeWidth={3} />}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-start justify-between gap-3">
                    <div className="text-xs font-bold text-slate-200">{a.title}</div>
                    <div className="text-[10px] font-mono text-amber-400 shrink-0">
                      {a.effort_hours}h
                      <span className="text-slate-600"> ({perRole}h/role)</span>
                    </div>
                  </div>
                  <div className="mt-1 text-[11px] leading-relaxed text-slate-400 whitespace-pre-line">
                    {a.why}
                  </div>
                  <div className="mt-1.5 flex flex-wrap gap-1">
                    {a.roles.map((role) => (
                      <span
                        key={role}
                        className="rounded bg-slate-800 px-1.5 py-0.5 text-[9px] text-slate-400"
                      >
                        {role}
                      </span>
                    ))}
                  </div>
                </div>
              </div>
            </button>
          );
        })}
      </div>

      <div className="text-[10px] text-slate-600 italic">
        Your selections are saved automatically and will be shown in later exercises
        as your remediation plan of record.
      </div>
    </div>
  );
}
