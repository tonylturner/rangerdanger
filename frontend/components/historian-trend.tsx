"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getHistorianEvents, majorHistorianEvents, getHistorianHistory, getHistorianPoints, type HistorianEvent, type History } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, qualityFlags } from "../lib/operator-format";
import type { OperatorPoint } from "../lib/operator-api";
import { regulationBand, type VoltageBand } from "../lib/operator-workstation";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { HistorianAnnotations, HistorianEventList } from "./historian-annotations";
import { historyScale, MinuteAxis, StripAxis, StripPlot } from "./historian-chart";

const SECTION_VIEW = "section-voltage";
const SECTION_SIGNALS = [
  { id: "rmu-1-source-kv", label: "S1", color: "#38bdf8" },
  { id: "rmu-1-section-kv", label: "S2", color: "#a78bfa" },
  { id: "rmu-2-source-kv", label: "S3", color: "#34d399" },
] as const;
const VOLTAGE_SIGNAL_IDS = [
  "oltc-1-bus-kv", "feeder-voltage", "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-section-kv", "rmu-2-source-kv",
];

function HistorySnapshots({ history, expandableSnapshots = false, captionPrefix }: { history: History; expandableSnapshots?: boolean; captionPrefix?: string }) {
  const [snapshotsExpanded, setSnapshotsExpanded] = useState(false);
  const snapshotLimit = expandableSnapshots && !snapshotsExpanded ? 5 : 30;
  const snapshots = history.segments.flatMap((segment, i) => segment.samples.map((sample) => ({ sample, epoch: segment.cache_epoch, segment: i }))).slice(-snapshotLimit).reverse();
  return <div role="region" aria-label="Latest snapshots" tabIndex={0} className="max-w-full overflow-x-auto">
    {expandableSnapshots && <button type="button" aria-expanded={snapshotsExpanded} className="mb-2 text-xs text-slate-300 underline decoration-slate-500 underline-offset-2 hover:text-slate-100 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-sky-400" onClick={() => setSnapshotsExpanded((expanded) => !expanded)}>
      Show last {snapshotsExpanded ? 5 : 30}
    </button>}
    <table className="w-full min-w-3xl text-left text-xs text-slate-300">
      <caption className="pb-2 text-left text-slate-400">{captionPrefix ? `${captionPrefix} · ` : ""}Latest {snapshots.length} snapshots, newest first · full timestamps in UTC</caption>
      <thead><tr>{["CC epoch / seq", "Value", "Quality / reason", "Source time", "CC received", "Ingested"].map((h) => <th className="px-2 py-1" key={h}>{h}</th>)}</tr></thead>
      <tbody>{snapshots.map(({ sample: s, epoch, segment }) => <tr key={`${segment}-${s.snapshot_seq}`} className="border-t border-slate-800">
        <td className="px-2 py-1">{epoch} / {s.snapshot_seq}</td>
        <td className="px-2 py-1">{formatPointValue(history.point, s.value, s.quality)}</td>
        <td className="px-2 py-1">{qualityFlags(s.quality).join(" ") || "no flags"} {s.stale_reason ?? ""}</td>
        <td className="px-2 py-1">{s.source_time ?? "untimed"}{s.source_time_invalid ? " · time IV" : ""}</td>
        <td className="px-2 py-1">{s.cc_received_at ?? "unknown"}</td>
        <td className="px-2 py-1">{s.ingested_at}</td>
      </tr>)}</tbody>
    </table>
  </div>;
}

type TrendWindow = { since: string; until: string };
function historyBounds(histories: History[], window?: TrendWindow) {
  const times = histories.flatMap((history) => history.segments.flatMap((segment) => segment.samples.map((sample) => Date.parse(sample.ingested_at)))).filter(Number.isFinite);
  return { xmin: window ? Date.parse(window.since) : Math.min(...times), xmax: window ? Date.parse(window.until) : Math.max(...times) };
}
const MARKER_KEY = "Dots and full-height rules: archived status changes (hover, focus or tap to pin). Amber bands: ingest silence or invalid observations. Times UTC.";

export function HistoryPlot({ history, band, expandableSnapshots = false, events = [], window }: { history: History; band?: VoltageBand | null; expandableSnapshots?: boolean; events?: HistorianEvent[]; window?: TrendWindow }) {
  const { xmin, xmax } = historyBounds([history], window);
  const scale = historyScale(history, band);
  return <div className="min-w-0 space-y-3 wrap-anywhere">
    {Number.isFinite(xmin) && Number.isFinite(xmax) && <div className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] gap-x-2 pt-6 text-xs text-slate-300">
      <StripAxis scale={scale} />
      <div className="relative min-w-0">
        <StripPlot history={history} scale={scale} xmin={xmin} xmax={xmax} band={band} label={`${pointLabel(history.point)} trend by ingest time UTC; breaks mark missing or invalid observations`} />
        <HistorianAnnotations history={history} events={events} xmin={xmin} xmax={xmax} />
      </div>
      <MinuteAxis xmin={xmin} xmax={xmax} />
    </div>}
    {!scale.values.length && <p className="text-sm text-amber-300">No valid numeric observations in this window. Unknown and stale samples are not plotted as zero. Select a numeric or single-point signal for a trend.</p>}
    <p className="text-xs text-slate-400">{MARKER_KEY}</p>
    {band && <p className="text-xs text-slate-300">Current PLC band {band.low.toFixed(2)}–{band.high.toFixed(2)} kV, not historical setpoints.</p>}
    <ul className="text-xs text-amber-300">{history.gaps.map((gap, i) => <li key={i}>Ingest gap: {gap.from} → {gap.to}</li>)}</ul>
    <p className="text-xs text-slate-400">{history.segments.length} cache-epoch segment(s). Lines never cross an epoch or a gap. Invalid, substituted, blocked, overflow and transient samples break the line.</p>
    <HistorianEventList history={history} events={events} />
    <HistorySnapshots history={history} expandableSnapshots={expandableSnapshots} />
  </div>;
}

export function SectionHistoryPlot({ histories, events = [], window, expandableSnapshots = false }: { histories: History[]; events?: HistorianEvent[]; window?: TrendWindow; expandableSnapshots?: boolean }) {
  const plotted = SECTION_SIGNALS.map((series) => {
    const history = histories.find((item) => item.point === series.id) ?? { point: series.id, segments: [], gaps: [] };
    return { ...series, history, scale: historyScale(history) };
  });
  const { xmin, xmax } = historyBounds(histories, window);
  const snapshotHistory = histories.find((history) => history.point === "rmu-1-section-kv");
  return <div className="min-w-0 space-y-3 wrap-anywhere">
    <p className="text-xs text-slate-300">Section voltages · kV · independent scales: S1 RMU1 source VT; S2 RMU1 section VT; S3 RMU2 source VT.</p>
    {Number.isFinite(xmin) && Number.isFinite(xmax) && <div className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] gap-x-2 pt-6 text-xs text-slate-300" role="group" aria-label="S1, S2 and S3 section voltage trends">
      <div>{plotted.map((series) => <StripAxis key={series.id} compact label={series.label} scale={series.scale} />)}</div>
      <div className="relative min-w-0">
        {plotted.map((series) => <div key={series.id} data-section-strip={series.label} className="relative h-[96px]">
          <StripPlot {...series} compact xmin={xmin} xmax={xmax} label={`${series.label} section voltage by historian ingest time UTC`} />
          <HistorianAnnotations history={series.history} events={events} showEvents={false} xmin={xmin} xmax={xmax} />
          {!series.scale.values.length && <p className="absolute inset-x-1 top-12 text-xs text-amber-300">No valid {series.label} observations</p>}
        </div>)}
        <HistorianAnnotations events={events} xmin={xmin} xmax={xmax} />
      </div>
      <MinuteAxis xmin={xmin} xmax={xmax} />
    </div>}
    <p className="text-xs text-slate-400">{MARKER_KEY}</p>
    <p className="text-xs text-slate-400">One shared time axis; each section independently breaks at cache epochs, ingest gaps, invalid quality or transient samples. S2 uses RMU1 section VT; the RMU2 section VT is the same S2 bus and remains selectable on its own.</p>
    {histories[0] && <HistorianEventList history={snapshotHistory ?? histories[0]} events={events} />}
    {snapshotHistory
      ? <HistorySnapshots history={snapshotHistory} expandableSnapshots={expandableSnapshots} captionPrefix="S2 snapshots · RMU1 section VT (same S2 bus measured at RMU2)" />
      : <p className="text-xs text-slate-400">S2 snapshot table unavailable: no RMU1 section VT history was received.</p>}
  </div>;
}

export function HistorianTrend({ points = [], voltageOnly = false, expandableSnapshots = false }: { points?: OperatorPoint[]; voltageOnly?: boolean; expandableSnapshots?: boolean }) {
  const [point, setPoint] = useState(() => voltageOnly ? SECTION_VIEW : "oltc-1-bus-kv");
  const sectionMode = voltageOnly && point === SECTION_VIEW;
  const catalogue = useQuery({ queryKey: ["historian", "points"], queryFn: getHistorianPoints, refetchInterval: 10000 });
  const history = useQuery({
    queryKey: ["historian", "trend", point],
    queryFn: async () => {
      const until = Date.now();
      const window = { since: new Date(until - 15 * 60_000).toISOString(), until: new Date(until).toISOString() };
      const [histories, events] = await Promise.all([
        Promise.all((sectionMode ? SECTION_SIGNALS.map((series) => series.id) : [point]).map((id) => getHistorianHistory(id, window.since, window.until))),
        getHistorianEvents(window.since, window.until),
      ]);
      return { histories, events: majorHistorianEvents(events.events), window };
    },
    refetchInterval: 5000,
  });
  const sectionHistories = history.data?.histories ?? [];
  const error = catalogue.error ?? history.error;
  const signals = catalogue.data?.filter((p) => !voltageOnly || VOLTAGE_SIGNAL_IDS.includes(p.id));
  const chartHistory = sectionMode ? sectionHistories.length > 0 : Boolean(history.data);
  return <section aria-label="Historian trend" className="min-w-0 space-y-3 rounded-xl border border-slate-800 bg-slate-950 p-4">
    <h2 className="text-sm font-bold text-slate-200">{voltageOnly ? `Voltage trend · ${sectionMode ? "sections S1/S2/S3" : pointLabel(point)}` : "Historian · CC-observed history"}</h2>
    <p className="max-w-3xl text-xs text-slate-400">Last 15 minutes in the Operations DMZ. Read-only 1 s snapshots, not a complete sequence-of-events archive. The horizontal axis uses ingest time, not the station clock. Reset Lab does not erase history.</p>
    <label className="flex flex-wrap items-center gap-2 text-xs text-slate-300">Signal
      <select className="max-w-full rounded-sm border border-slate-600 bg-slate-900 p-2 text-slate-100" value={point} onChange={(e) => setPoint(e.target.value)}>
        {voltageOnly && <option value={SECTION_VIEW}>Sections S1/S2/S3 · default</option>}
        {!signals?.some((p) => p.id === point) && !(voltageOnly && point === SECTION_VIEW) && <option value={point}>{pointLabel(point)}</option>}
        {signals?.map((p) => <option key={p.id} value={p.id}>{pointLabel(p.id)} · IOA {p.ioa}</option>)}
      </select>
    </label>
    {error ? <p role="alert" className="text-xs text-amber-300">{isRangeNotReady(error) ? "History shows once the range is ready." : `Historian unavailable: ${errorMessage(error)}. ${chartHistory ? "Showing received history, not a live feed." : "No history received."}`}</p> : null}
    {sectionMode
      ? sectionHistories.length ? <SectionHistoryPlot histories={sectionHistories} events={history.data?.events} window={history.data?.window} expandableSnapshots={expandableSnapshots} /> : !error ? <p className="text-sm text-slate-400">Waiting for section voltage snapshots…</p> : null
      : history.data ? <HistoryPlot history={history.data.histories[0]} events={history.data.events} window={history.data.window} band={point === "oltc-1-bus-kv" ? regulationBand(points) : null} expandableSnapshots={expandableSnapshots} /> : !error ? <p className="text-sm text-slate-400">Waiting for historian snapshots…</p> : null}
  </section>;
}
