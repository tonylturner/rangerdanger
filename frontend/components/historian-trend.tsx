"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getHistorianHistory, getHistorianPoints, historyRuns, numericSample, type History } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, qualityFlags } from "../lib/operator-format";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";

export function HistoryPlot({ history }: { history: History }) {
  const samples = history.segments.flatMap((s) => s.samples);
  const times = samples.map((s) => Date.parse(s.ingested_at)).filter(Number.isFinite);
  const runs = historyRuns(history);
  const values = runs.flat().map((s) => numericSample(s)!);
  const xmin = Math.min(...times), xmax = Math.max(...times);
  const ymin = Math.min(...values), ymax = Math.max(...values);
  const x = (t: string) => 60 + (Date.parse(t) - xmin) / (xmax - xmin || 1) * 840;
  const y = (n: number) => 160 - (n - ymin) / (ymax - ymin || 1) * 120;
  return <div className="space-y-3">
    {values.length ? <svg viewBox="0 0 940 205" role="img" aria-label={`${pointLabel(history.point)} trend by ingest time UTC; breaks mark missing or invalid observations`} className="w-full">
      <path d="M60 25 V175 H900" fill="none" stroke="#64748b" />
      {history.gaps.map((gap, i) => <rect key={i} x={Math.max(60, x(gap.from))} y="25" width={Math.max(0, Math.min(900, x(gap.to)) - Math.max(60, x(gap.from)))} height="150" fill="#fbbf24" fillOpacity="0.15"><title>Ingest gap {gap.from} to {gap.to}</title></rect>)}
      {runs.map((run, i) => <g key={i} data-history-run="true">
        <polyline points={run.map((s) => `${x(s.ingested_at)},${y(numericSample(s)!)}`).join(" ")} fill="none" stroke="#38bdf8" strokeWidth="2" />
        {run.map((s) => <circle key={s.snapshot_seq} cx={x(s.ingested_at)} cy={y(numericSample(s)!)} r="2" fill="#38bdf8"><title>{s.ingested_at}: {formatPointValue(history.point, s.value)}</title></circle>)}
      </g>)}
      <text x="55" y="40" textAnchor="end" fill="#cbd5e1" fontSize="11">{ymax.toFixed(2)}</text>
      <text x="55" y="164" textAnchor="end" fill="#cbd5e1" fontSize="11">{ymin.toFixed(2)}</text>
      <text x="60" y="195" fill="#cbd5e1" fontSize="11">{formatUtcTime(new Date(xmin).toISOString())} UTC</text>
      <text x="900" y="195" textAnchor="end" fill="#cbd5e1" fontSize="11">{formatUtcTime(new Date(xmax).toISOString())} UTC</text>
    </svg> : <p className="text-sm text-amber-300">No good numeric observations in this window. Unknown and stale samples are not plotted as zero. Select a numeric or single-point signal for a trend.</p>}
    <ul className="text-xs text-amber-300">{history.gaps.map((gap, i) => <li key={i}>Ingest gap: {gap.from} → {gap.to}</li>)}</ul>
    <p className="text-xs text-slate-400">{history.segments.length} cache-epoch segment(s). Lines never cross an epoch or a gap. Invalid, substituted, blocked, overflow and transient samples break the line.</p>
    <div className="max-h-64 overflow-auto">
      <table className="w-full text-left text-xs text-slate-300">
        <caption className="pb-2 text-left text-slate-400">Latest 30 snapshots, newest first · full timestamps in UTC</caption>
        <thead><tr>{["CC epoch / seq", "Value", "Quality / reason", "Source time", "CC received", "Ingested"].map((h) => <th className="px-2 py-1" key={h}>{h}</th>)}</tr></thead>
        <tbody>{history.segments.flatMap((segment, i) => segment.samples.map((sample) => ({ sample, epoch: segment.cache_epoch, segment: i }))).slice(-30).reverse().map(({ sample: s, epoch, segment }) => <tr key={`${segment}-${s.snapshot_seq}`} className="border-t border-slate-800">
          <td className="px-2 py-1">{epoch} / {s.snapshot_seq}</td>
          <td className="px-2 py-1">{formatPointValue(history.point, s.value)}{s.transient ? " · TRANSIENT" : ""}</td>
          <td className="px-2 py-1">{qualityFlags(s.quality).join(" ") || "no flags"} {s.stale_reason ?? ""}</td>
          <td className="px-2 py-1">{s.source_time ?? "untimed"}{s.source_time_invalid ? " · time IV" : ""}</td>
          <td className="px-2 py-1">{s.cc_received_at ?? "unknown"}</td>
          <td className="px-2 py-1">{s.ingested_at}</td>
        </tr>)}</tbody>
      </table>
    </div>
  </div>;
}

export function HistorianTrend() {
  const [point, setPoint] = useState("oltc-1-bus-kv");
  const points = useQuery({ queryKey: ["historian", "points"], queryFn: getHistorianPoints, refetchInterval: 10000 });
  const history = useQuery({
    queryKey: ["historian", "history", point],
    queryFn: () => {
      const until = Date.now();
      return getHistorianHistory(point, new Date(until - 15 * 60_000).toISOString(), new Date(until).toISOString());
    },
    refetchInterval: 5000,
  });
  const error = points.error ?? history.error;
  return <section aria-label="Historian trend" className="space-y-3 rounded-xl border border-slate-800 bg-slate-950 p-4">
    <h2 className="text-sm font-bold text-slate-200">Historian · CC-observed history</h2>
    <p className="max-w-3xl text-xs text-slate-400">Last 15 minutes in the Operations DMZ. Read-only 1 s snapshots, not a complete sequence-of-events archive. The horizontal axis uses ingest time, not the station clock. Reset Lab does not erase history.</p>
    <label className="flex flex-wrap items-center gap-2 text-xs text-slate-300">Signal
      <select className="max-w-full rounded border border-slate-600 bg-slate-900 p-2 text-slate-100" value={point} onChange={(e) => setPoint(e.target.value)}>
        {!points.data?.some((p) => p.id === point) && <option value={point}>{pointLabel(point)}</option>}
        {points.data?.map((p) => <option key={p.id} value={p.id}>{pointLabel(p.id)} · IOA {p.ioa}</option>)}
      </select>
    </label>
    {error ? <p role="alert" className="text-xs text-amber-300">{isRangeNotReady(error) ? "History shows once the range is ready." : `Historian unavailable: ${errorMessage(error)}. ${history.data ? "Showing the last answer, not a live feed." : "No history received."}`}</p> : null}
    {history.data ? <HistoryPlot history={history.data} /> : !error ? <p className="text-sm text-slate-400">Waiting for historian snapshots…</p> : null}
  </section>;
}
