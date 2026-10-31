import type { ExcludedAction } from "../lib/remediation-plan";

export function PlanReadinessExclusions({ excluded, applied = false }: { excluded: ExcludedAction[]; applied?: boolean }) {
  if (!excluded.length) return null;
  return (
    <div role="status" className="mt-2 rounded border border-amber-800/60 bg-amber-950/20 p-2 text-[11px] text-amber-300">
      <p>
        Saved actions excluded from draft totals, coverage and Apply Your Plan by Lab 1.3 readiness:
        {applied && " Applied-plan provenance and proof expectations are unchanged."}
      </p>
      <ul className="mt-1 space-y-1">
        {excluded.map(({ id, reasons }) => <li key={id}><strong>{id}</strong>: {reasons.join("; ")}</li>)}
      </ul>
      <p className="mt-1">Saved choices are retained. Change readiness or use the default budget in Lab 1.4 to restore them.</p>
    </div>
  );
}
