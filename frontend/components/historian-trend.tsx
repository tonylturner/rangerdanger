"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getHistorianHistory, getHistorianPoints, historyRuns, numericSample, type History } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, qualityFlags } from "../lib/operator-format";
import type { OperatorPoint } from "../lib/operator-api";
import { regulationBand, type VoltageBand } from "../lib/operator-workstation";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";

export function HistoryPlot({ history, band }: { history: History; band?: VoltageBand | null }) {
  const samples = history.segments.flatMap((s) => s.samples);
  const times = samples.map((s) => Date.parse(s.ingested_at)).filter(Number.isFinite);
  const runs = historyRuns(history);
  const values = runs.flat().map((s) => numericSample(s)!);
  const xmin = Math.min(...times), xmax = Math.max(...times);
  const ymin = Math.min(...values, ...(band ? [band.low] : [])), ymax = Math.max(...values, ...(band ? [band.high] : []));
  const x = (t: string) => (Date.parse(t) - xmin) / (xmax - xmin || 1) * 840;
  const y = (n: number) => 135 - (n - ymin) / (ymax - ymin || 1) * 120;
  return <div className="min-w-0 space-y-3 [overflow-wrap:anywhere]">
    {values.length ? <div className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] gap-x-1 gap-y-2 pt-6 text-xs text-slate-300 sm:pr-10">
      {/* HTML axes keep their type size while the plot scales horizontally.
          Its desktop proportions stay the same; phones get a 150px floor. */}
      <div className="relative">
        <span className="absolute right-0 top-[10%] -translate-y-1/2">{ymax.toFixed(2)}</span>
        <span className="absolute right-0 top-[90%] -translate-y-1/2">{ymin.toFixed(2)}</span>
      </div>
      <svg viewBox="0 0 840 150" preserveAspectRatio="none" role="img" aria-label={`${pointLabel(history.point)} trend by ingest time UTC; breaks mark missing or invalid observations`} className="aspect-[840/150] h-auto min-h-[150px] w-full overflow-visible">
        {band && <rect x="0" y={y(band.high)} width="840" height={Math.max(1, y(band.low) - y(band.high))} fill="#64748b" fillOpacity="0.3"><title>Current PLC band, not historical setpoints</title></rect>}
        <path d="M0 0 V150 H840" fill="none" stroke="#64748b" vectorEffect="non-scaling-stroke" />
        {history.gaps.map((gap, i) => <rect key={i} x={Math.max(0, x(gap.from))} y="0" width={Math.max(0, Math.min(840, x(gap.to)) - Math.max(0, x(gap.from)))} height="150" fill="#fbbf24" fillOpacity="0.15"><title>{`Ingest gap ${gap.from} to ${gap.to}`}</title></rect>)}
        {runs.map((run, i) => <g key={i} data-history-run="true">
          <polyline points={run.map((s) => `${x(s.ingested_at)},${y(numericSample(s)!)}`).join(" ")} fill="none" stroke="#38bdf8" strokeWidth="2" vectorEffect="non-scaling-stroke" />
          {run.map((s) => <circle key={s.snapshot_seq} cx={x(s.ingested_at)} cy={y(numericSample(s)!)} r="1" fill="#38bdf8" stroke="#38bdf8" strokeWidth="2" vectorEffect="non-scaling-stroke"><title>{`${s.ingested_at}: ${formatPointValue(history.point, s.value, s.quality)}`}</title></circle>)}
        </g>)}
      </svg>
      <div className="col-start-2 flex min-w-0 flex-wrap justify-between gap-x-4 gap-y-1">
        <span className="whitespace-nowrap">{formatUtcTime(new Date(xmin).toISOString())} UTC</span>
        <span className="ml-auto whitespace-nowrap">{formatUtcTime(new Date(xmax).toISOString())} UTC</span>
      </div>
    </div> : <p className="text-sm text-amber-300">No good numeric observations in this window. Unknown and stale samples are not plotted as zero. Select a numeric or single-point signal for a trend.</p>}
    {band && <p className="text-xs text-slate-300">Current PLC band {band.low.toFixed(2)}–{band.high.toFixed(2)} kV, not historical setpoints.</p>}
    <ul className="text-xs text-amber-300">{history.gaps.map((gap, i) => <li key={i}>Ingest gap: {gap.from} → {gap.to}</li>)}</ul>
    <p className="text-xs text-slate-400">{history.segments.length} cache-epoch segment(s). Lines never cross an epoch or a gap. Invalid, substituted, blocked, overflow and transient samples break the line.</p>
    <div role="region" aria-label="Latest snapshots" tabIndex={0} className="max-w-full overflow-x-auto">
      <table className="w-full min-w-[48rem] text-left text-xs text-slate-300">
        <caption className="pb-2 text-left text-slate-400">Latest 30 snapshots, newest first · full timestamps in UTC</caption>
        <thead><tr>{["CC epoch / seq", "Value", "Quality / reason", "Source time", "CC received", "Ingested"].map((h) => <th className="px-2 py-1" key={h}>{h}</th>)}</tr></thead>
        <tbody>{history.segments.flatMap((segment, i) => segment.samples.map((sample) => ({ sample, epoch: segment.cache_epoch, segment: i }))).slice(-30).reverse().map(({ sample: s, epoch, segment }) => <tr key={`${segment}-${s.snapshot_seq}`} className="border-t border-slate-800">
          <td className="px-2 py-1">{epoch} / {s.snapshot_seq}</td>
          <td className="px-2 py-1">{formatPointValue(history.point, s.value, s.quality)}</td>
          <td className="px-2 py-1">{qualityFlags(s.quality).join(" ") || "no flags"} {s.stale_reason ?? ""}</td>
          <td className="px-2 py-1">{s.source_time ?? "untimed"}{s.source_time_invalid ? " · time IV" : ""}</td>
          <td className="px-2 py-1">{s.cc_received_at ?? "unknown"}</td>
          <td className="px-2 py-1">{s.ingested_at}</td>
        </tr>)}</tbody>
      </table>
    </div>
  </div>;
}

export function HistorianTrend({ points = [], voltageOnly = false }: { points?: OperatorPoint[]; voltageOnly?: boolean }) {
  const [point, setPoint] = useState("oltc-1-bus-kv");
  const catalogue = useQuery({ queryKey: ["historian", "points"], queryFn: getHistorianPoints, refetchInterval: 10000 });
  const history = useQuery({
    queryKey: ["historian", "history", point],
    queryFn: () => {
      const until = Date.now();
      return getHistorianHistory(point, new Date(until - 15 * 60_000).toISOString(), new Date(until).toISOString());
    },
    refetchInterval: 5000,
  });
  const error = catalogue.error ?? history.error;
  const signals = catalogue.data?.filter((p) => !voltageOnly || ["oltc-1-bus-kv", "feeder-voltage", "rmu-1-section-kv", "rmu-2-section-kv", "rmu-1-source-kv", "rmu-2-source-kv"].includes(p.id));
  return <section aria-label="Historian trend" className="min-w-0 space-y-3 rounded-xl border border-slate-800 bg-slate-950 p-4">
    <h2 className="text-sm font-bold text-slate-200">{voltageOnly ? "Voltage trend · bus and sections" : "Historian · CC-observed history"}</h2>
    <p className="max-w-3xl text-xs text-slate-400">Last 15 minutes in the Operations DMZ. Read-only 1 s snapshots, not a complete sequence-of-events archive. The horizontal axis uses ingest time, not the station clock. Reset Lab does not erase history.</p>
    <label className="flex flex-wrap items-center gap-2 text-xs text-slate-300">Signal
      <select className="max-w-full rounded border border-slate-600 bg-slate-900 p-2 text-slate-100" value={point} onChange={(e) => setPoint(e.target.value)}>
        {!signals?.some((p) => p.id === point) && <option value={point}>{pointLabel(point)}</option>}
        {signals?.map((p) => <option key={p.id} value={p.id}>{pointLabel(p.id)} · IOA {p.ioa}</option>)}
      </select>
    </label>
    {error ? <p role="alert" className="text-xs text-amber-300">{isRangeNotReady(error) ? "History shows once the range is ready." : `Historian unavailable: ${errorMessage(error)}. ${history.data ? "Showing the last answer, not a live feed." : "No history received."}`}</p> : null}
    {history.data ? <HistoryPlot history={history.data} band={point === "oltc-1-bus-kv" ? regulationBand(points) : null} /> : !error ? <p className="text-sm text-slate-400">Waiting for historian snapshots…</p> : null}
  </section>;
}
