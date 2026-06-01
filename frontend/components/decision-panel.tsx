"use client";

import { useEffect, useState, useMemo } from "react";
import { Check, AlertCircle, CircleCheck, CircleDashed, ChevronUp, ChevronDown } from "lucide-react";
import type { StepAction, DecisionAction, DecisionRole } from "../lib/api";
import { saveRemediationPlan, loadRemediationPlan } from "../lib/remediation-plan";
import {
  readRequirements,
  actionImplements,
  computeCoverage,
  summariseCoverage,
  readReadiness,
  readinessFlagsForAction,
  type Requirement,
  type ReadinessAnswer,
} from "../lib/requirement-coverage";

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

  // Lab 1.3 verdicts (the design requirements) + resourcing
  // readiness answers. Both read once on mount - students aren't
  // expected to flip back to 1.3 mid-1.4-session.
  const [requirements, setRequirements] = useState<Requirement[]>([]);
  const [readiness, setReadiness] = useState<ReadinessAnswer[]>([]);
  useEffect(() => {
    setRequirements(readRequirements());
    setReadiness(readReadiness());
  }, []);
  const hasAnyReadiness = readiness.some((r) => r.verdict !== "");

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

  // Per-requirement coverage (computed against current selection)
  const coverage = useMemo(() => computeCoverage(requirements, selected), [requirements, selected]);
  const coverageSummary = useMemo(() => summariseCoverage(coverage), [coverage]);
  const hasAnyVerdict = requirements.some((r) => r.verdict !== "");

  // Coverage panel sticks to the bottom of the viewport so the
  // student sees coverage feedback while scrolling the long action
  // catalog. Compact-by-default; click to expand the per-requirement
  // detail rows.
  const [coverageOpen, setCoverageOpen] = useState(false);

  return (
    <div className="mt-3 space-y-4">
      {/* Lab 1.3 design requirements - context for the action picker */}
      {hasAnyVerdict && (
        <div className="rounded-lg border border-cyan-900/40 bg-cyan-950/10 p-3">
          <div className="text-[10px] font-bold uppercase tracking-wider text-cyan-400 mb-2">
            Your design requirements (from Lab 1.3)
          </div>
          <div className="grid gap-1.5">
            {requirements.map((req) => (
              <div
                key={req.id}
                className="flex items-baseline justify-between gap-3 text-[11px]"
              >
                <span className="text-slate-300 truncate">{req.label}</span>
                <span
                  className={`font-mono shrink-0 ${
                    req.verdict ? "text-cyan-300" : "text-slate-600 italic"
                  }`}
                >
                  {req.verdict || "not set"}
                </span>
              </div>
            ))}
          </div>
          <div className="mt-2 text-[10px] text-slate-500 italic">
            These are read-only - change them in the <a
              href="/exercises/segmentation-requirements"
              className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
            >Segmentation Requirements</a> exercise.
          </div>
        </div>
      )}

      {/* Resourcing readiness - observational overlays */}
      {hasAnyReadiness && (
        <div className="rounded-lg border border-purple-900/40 bg-purple-950/10 p-3">
          <div className="text-[10px] font-bold uppercase tracking-wider text-purple-400 mb-2">
            Your team readiness (from Lab 1.3)
          </div>
          <div className="grid gap-1.5">
            {readiness.map((r) => (
              <div
                key={r.key}
                className="flex items-baseline justify-between gap-3 text-[11px]"
              >
                <span className="text-slate-300 truncate">{r.label}</span>
                <span
                  className={`font-mono shrink-0 ${
                    !r.verdict ? "text-slate-600 italic"
                      : r.good ? "text-emerald-400"
                      : "text-amber-400"
                  }`}
                >
                  {r.verdict || "not set"}
                </span>
              </div>
            ))}
          </div>
          <div className="mt-2 text-[10px] text-slate-500 italic">
            Affected actions below show the dependencies these answers create. Selections aren&apos;t blocked - this is reality-check context.
          </div>
        </div>
      )}

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
                or pick one that doesn&apos;t require these roles.
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
          const implements_ = actionImplements(a.id, requirements);
          const readinessFlags = readinessFlagsForAction(a.id, readiness);
          const hasBlocked = readinessFlags.some((f) => f.severity === "blocked");
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
                    {implements_.length > 0 && implements_.map((req) => (
                      <span
                        key={`req-${req.id}`}
                        className="rounded border border-cyan-800/60 bg-cyan-950/40 px-1.5 py-0.5 text-[9px] text-cyan-300"
                        title={`This action implements your Lab 1.3 verdict: ${req.label} = ${req.verdict}`}
                      >
                        ↳ {req.label.split(" on ")[0].split(" → ").join("→")}: {req.verdict}
                      </span>
                    ))}
                    {implements_.length === 0 && hasAnyVerdict && (
                      <span
                        className="rounded border border-slate-700/60 bg-slate-800/40 px-1.5 py-0.5 text-[9px] text-slate-500 italic"
                        title="Operational hygiene / process - not tied to a specific 1.3 design verdict"
                      >
                        operational hygiene
                      </span>
                    )}
                  </div>
                  {readinessFlags.length > 0 && (
                    <div className={`mt-2 rounded border px-2 py-1.5 text-[10px] space-y-1 ${
                      hasBlocked
                        ? "border-red-800/50 bg-red-950/20 text-red-300"
                        : "border-amber-800/50 bg-amber-950/20 text-amber-300"
                    }`}>
                      {readinessFlags.map((f, idx) => (
                        <div key={idx} className="flex items-start gap-1.5">
                          <span className={`mt-0.5 font-bold uppercase tracking-wider text-[8px] shrink-0 ${
                            f.severity === "blocked" ? "text-red-400" : "text-amber-400"
                          }`}>
                            {f.severity === "blocked" ? "blocked" : "warn"}
                          </span>
                          <span className="leading-tight">{f.message}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </button>
          );
        })}
      </div>

      <div className="text-[10px] text-slate-600 italic pb-12">
        Your selections are saved automatically and will be shown in later exercises
        as your remediation plan of record.
      </div>

      {/* Sticky-bottom coverage summary - visible while the student
          scrolls the action catalog. Compact bar by default; click
          to expand per-requirement details. The pb-12 above gives
          the bar room so it doesn't overlap the footer text. */}
      {hasAnyVerdict && (
        <div className="sticky bottom-2 z-10 -mx-3">
          <div className="mx-3 rounded-lg border border-slate-700 bg-slate-950/95 backdrop-blur shadow-lg shadow-slate-950/50">
            <button
              onClick={() => setCoverageOpen(!coverageOpen)}
              className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-slate-900/60 transition-colors rounded-lg"
            >
              {coverageOpen ? (
                <ChevronDown className="h-4 w-4 text-slate-500 shrink-0" />
              ) : (
                <ChevronUp className="h-4 w-4 text-slate-500 shrink-0" />
              )}
              <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
                Plan Coverage
              </span>
              <span className="ml-auto flex items-baseline gap-2 text-[11px] font-mono">
                <span className="text-slate-300">
                  <span className={
                    coverageSummary.covered === (coverageSummary.total - coverageSummary.na) && coverageSummary.gap === 0
                      ? "text-emerald-400 font-bold"
                      : "text-slate-200 font-bold"
                  }>
                    {coverageSummary.covered}
                  </span>
                  <span className="text-slate-500">
                    {" / "}{coverageSummary.total - coverageSummary.na} covered
                  </span>
                </span>
                {coverageSummary.partial > 0 && (
                  <span className="text-amber-400">· {coverageSummary.partial} partial</span>
                )}
                {coverageSummary.gap > 0 && (
                  <span className="text-red-400">· {coverageSummary.gap} gap</span>
                )}
                {coverageSummary.na > 0 && (
                  <span className="text-slate-600">· {coverageSummary.na} n/a</span>
                )}
              </span>
            </button>
            {coverageOpen && (
              <div className="border-t border-slate-800 px-4 py-3 space-y-1.5 max-h-72 overflow-y-auto">
                {coverage.map((c) => {
                  if (c.status === "n/a") {
                    return (
                      <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                        <CircleDashed className="h-3.5 w-3.5 mt-0.5 text-slate-600 shrink-0" />
                        <span className="text-slate-500">
                          <span className="font-bold text-slate-400">{c.req.label}</span>
                          <span className="text-slate-600"> - {c.reason}</span>
                        </span>
                      </div>
                    );
                  }
                  if (c.status === "covered") {
                    return (
                      <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                        <CircleCheck className="h-3.5 w-3.5 mt-0.5 text-emerald-400 shrink-0" />
                        <span className="text-slate-300">
                          <span className="font-bold">{c.req.label}</span>
                          <span className="text-slate-500"> - {c.req.verdict}, fully addressed</span>
                        </span>
                      </div>
                    );
                  }
                  if (c.status === "partial") {
                    return (
                      <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                        <AlertCircle className="h-3.5 w-3.5 mt-0.5 text-amber-400 shrink-0" />
                        <span className="text-slate-300">
                          <span className="font-bold">{c.req.label}</span>
                          <span className="text-slate-500"> - {c.req.verdict}, partial: missing </span>
                          <span className="font-mono text-amber-400">{c.missingActions.join(", ")}</span>
                        </span>
                      </div>
                    );
                  }
                  // gap
                  return (
                    <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                      <AlertCircle className="h-3.5 w-3.5 mt-0.5 text-red-400 shrink-0" />
                      <span className="text-slate-300">
                        <span className="font-bold">{c.req.label}</span>
                        <span className="text-slate-500"> - {c.req.verdict}, no implementing action selected. Pick </span>
                        <span className="font-mono text-red-400">{c.expectedActions.join(" or ")}</span>
                        <span className="text-slate-500"> to close.</span>
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
