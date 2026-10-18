import type { ComparisonRow } from "../lib/physical-comparison";
import { agreementText, comparisonCounts } from "../lib/physical-comparison";
import { ageMs, formatAge, formatPointValue } from "../lib/operator-format";
import { PANEL } from "./physical-system-parts";

const BADGE = {
  MATCH: { symbol: "=", tone: "text-slate-300 border-slate-700" },
  MISMATCH: { symbol: "≠", tone: "text-red-200 border-red-600 bg-red-950" },
  STALE: { symbol: "◷", tone: "text-amber-200 border-amber-700" },
  INVALID: { symbol: "!", tone: "text-amber-200 border-amber-700" },
  UNKNOWN: { symbol: "?", tone: "text-amber-200 border-amber-700" },
};
function ComparisonItem({ row, now }: { row: ComparisonRow; now: number }) {
  const badge = BADGE[row.status], p = row.cc;
  return <div data-comparison={row.id} data-status={row.status} className="grid min-w-0 gap-2 border-t border-slate-800 py-2 text-xs sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.2fr)]">
    <div className="min-w-0 text-slate-200 [overflow-wrap:anywhere]">{row.label}</div>
    <div className="min-w-0 font-mono text-slate-200 [overflow-wrap:anywhere]"><span className="text-slate-400 sm:hidden">Physical: </span>{row.physical.shortText}</div>
    <div className="min-w-0 font-mono text-slate-200 [overflow-wrap:anywhere]"><span className="text-slate-400 sm:hidden">CC: </span>{p ? formatPointValue(p.id, p.value, p.quality) : "unknown"}
      <p className="mt-1 text-slate-400">IEC {row.flags.length ? row.flags.join(" ") : p ? "no flags" : "unknown"} · received {p?.received_at ? `${formatAge(ageMs(p.received_at, now))} ago` : "unknown"}</p>
      {p?.source_time && <p className="text-slate-400">Source UTC {p.source_time}{p.source_time_invalid ? " · time IV" : ""}</p>}
    </div>
    <div className="min-w-0 [overflow-wrap:anywhere]"><span className={`inline-block rounded border px-1.5 py-0.5 font-mono ${badge.tone}`}>{badge.symbol} {row.status}</span><p className="mt-1 text-slate-400">{row.detail}</p></div>
  </div>;
}
export function PhysicalComparison({ rows, now, allOpen, onAllToggle }: { rows: ComparisonRow[]; now: number; allOpen: boolean; onAllToggle: () => void }) {
  const counts = comparisonCounts(rows);
  const matched = counts.MATCH === rows.length;
  const visible = rows.filter((r) => r.primary || r.status !== "MATCH");
  const secondary = rows.filter((r) => !r.primary && r.status === "MATCH");
  return <section id="physical-comparison" aria-label="Physical system vs. Control Centre" className={`${PANEL} scroll-mt-4 ${matched ? "border-slate-700" : counts.MISMATCH ? "border-red-700" : "border-amber-700"}`}>
    <h2 className="text-sm font-semibold text-slate-200">Physical system vs. Control Centre</h2>
    <p className="my-2 font-mono text-xs text-slate-300">Physical agreement: {agreementText(counts)}</p>
    <div className="hidden gap-2 pb-1 text-xs text-slate-400 sm:grid sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.2fr)]"><span>Quantity</span><span>Physical · sim</span><span>Control Centre · IEC 104</span><span>Agreement / evidence</span></div>
    {visible.map((r) => <ComparisonItem key={r.id} row={r} now={now} />)}
    <button type="button" className="mt-2 rounded p-1 text-xs text-slate-300 hover:bg-slate-800 focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400" aria-expanded={allOpen} aria-controls="physical-all-points" onClick={onAllToggle}>{allOpen ? "−" : "+"} All comparable points ({rows.length})</button>
    {allOpen && <div id="physical-all-points">{secondary.length ? secondary.map((r) => <ComparisonItem key={r.id} row={r} now={now} />) : <p className="py-2 text-xs text-slate-400">All comparable points are already visible above.</p>}</div>}
  </section>;
}
