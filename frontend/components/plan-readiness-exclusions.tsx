"use client";
import { useCurriculumScope } from "../lib/curriculum-scope";
import { planActivityOf } from "../lib/course";
import type { ExcludedAction } from "../lib/remediation-plan";

export function PlanReadinessExclusions({ excluded, applied = false }: { excluded: ExcludedAction[]; applied?: boolean }) {
  const { course } = useCurriculumScope();
  if (!excluded.length) return null;
  return (
    <div role="status" className="mt-2 rounded border border-amber-800/60 bg-amber-950/20 p-2 text-[11px] text-amber-300">
      <p>
        Saved actions excluded from draft totals, coverage and Apply Your Plan by Lab {planActivityOf(course, "requirements")?.label} readiness:
        {applied && " Applied-plan provenance and proof expectations are unchanged."}
      </p>
      <ul className="mt-1 space-y-1">
        {excluded.map(({ id, reasons }) => <li key={id}><strong>{id}</strong>: {reasons.join("; ")}</li>)}
      </ul>
      <p className="mt-1">Saved choices are retained. Change readiness or use the default budget in Lab {planActivityOf(course, "planning")?.label} to restore them.</p>
    </div>
  );
}
