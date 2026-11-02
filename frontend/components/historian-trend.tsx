"use client";

import { useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { getHistorianHistory, getHistorianPoints, historyRuns, numericSample, voltageTrendJournalEvents, type History, type TrendJournalEvents } from "../lib/historian-api";
import { formatPointValue, formatUtcTime, pointLabel, qualityFlags } from "../lib/operator-format";
import { getOperatorJournal, type OperatorPoint } from "../lib/operator-api";
import { regulationBand, type VoltageBand } from "../lib/operator-workstation";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";

const SECTION_VIEW = "section-voltage";
const SECTION_SIGNALS = [
  { id: "rmu-1-source-kv", label: "S1", color: "#38bdf8", dash: undefined },
  { id: "rmu-1-section-kv", label: "S2", color: "#a78bfa", dash: "8 4" },
  { id: "rmu-2-source-kv", label: "S3", color: "#34d399", dash: "2 3" },
] as const;
const VOLTAGE_SIGNAL_IDS = [
  "oltc-1-bus-kv", "feeder-voltage", "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-section-kv", "rmu-2-source-kv",
];

function visibleJournalEvents(histories: History[], events?: TrendJournalEvents) {
  const segments = histories.flatMap((history) => history.segments);
  const epochs = new Set(segments.map((segment) => segment.cache_epoch));
  const samples = segments.flatMap((segment) => segment.samples);
  const times = samples.map((sample) => Date.parse(sample.ingested_at)).filter(Number.isFinite);
  if (!events || !epochs.has(events.cacheEpoch) || times.length === 0) return { markers: [], commandNotices: [], epochMismatch: Boolean(events && segments.length && !epochs.has(events.cacheEpoch)) };
  const start = Math.min(...times), end = Math.max(...times);
  const inWindow = (at: string) => {
    const time = Date.parse(at);
    return Number.isFinite(time) && time >= start && time <= end;
  };
  return {
    markers: events.markers.filter((event) => inWindow(event.at)),
    commandNotices: events.commandNotices.filter((notice) => inWindow(notice.at)),
    epochMismatch: false,
  };
}

function TrendEventNotes({ markers, commandNotices, epochMismatch, available }: ReturnType<typeof visibleJournalEvents> & { available: boolean }) {
  if (!available && !markers.length && !commandNotices.length && !epochMismatch) return null;
  return <div className="space-y-1 text-xs">
    <p className="text-slate-400">Event ticks use CC receive time; the voltage axis uses historian ingest time.</p>
    {epochMismatch && <p className="text-amber-300">No event markers: the journal and historian belong to different CC cache epochs.</p>}
    {markers.length > 0 && <ul aria-label="Switching and protection events" className="flex flex-wrap gap-x-4 gap-y-1 text-slate-200">
      {markers.slice(-8).reverse().map((event) => <li key={event.seq} className={event.critical ? "text-rose-300" : "text-amber-200"}>
        <span className="font-semibold">{event.label}</span> · {formatUtcTime(event.at)} UTC{event.completedCommand ? " · completed command feedback" : ""}
      </li>)}
    </ul>}
    {commandNotices.length > 0 && <ul aria-label="Commands without completed switching markers" className="space-y-1 text-amber-300">
      {commandNotices.map((notice) => <li key={notice.seq}>{notice.label} — {notice.result.toUpperCase()}; no completed switching marker · {formatUtcTime(notice.at)} UTC</li>)}
    </ul>}
  </div>;
}

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

export function HistoryPlot({ history, band, expandableSnapshots = false, journalEvents }: { history: History; band?: VoltageBand | null; expandableSnapshots?: boolean; journalEvents?: TrendJournalEvents }) {
  const samples = history.segments.flatMap((s) => s.samples);
  const times = samples.map((s) => Date.parse(s.ingested_at)).filter(Number.isFinite);
  const runs = historyRuns(history);
  const values = runs.flat().map((s) => numericSample(s)!);
  const xmin = Math.min(...times), xmax = Math.max(...times);
  const ymin = Math.min(...values, ...(band ? [band.low] : [])), ymax = Math.max(...values, ...(band ? [band.high] : []));
  const x = (t: string) => (Date.parse(t) - xmin) / (xmax - xmin || 1) * 840;
  const y = (n: number) => 135 - (n - ymin) / (ymax - ymin || 1) * 120;
  const eventNotes = visibleJournalEvents([history], journalEvents);
  return <div className="min-w-0 space-y-3 wrap-anywhere">
    {values.length ? <div className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] gap-x-1 gap-y-2 pt-6 text-xs text-slate-300 sm:pr-10">
      {/* HTML axes keep their type size while the plot scales horizontally.
          Its desktop proportions stay the same; phones get a 150px floor. */}
      <div className="relative">
        <span className="absolute right-0 top-[10%] -translate-y-1/2">{ymax.toFixed(2)}</span>
        <span className="absolute right-0 top-[90%] -translate-y-1/2">{ymin.toFixed(2)}</span>
      </div>
      <svg viewBox="0 0 840 150" preserveAspectRatio="none" role="img" aria-label={`${pointLabel(history.point)} trend by ingest time UTC; breaks mark missing or invalid observations`} className="aspect-840/150 h-auto min-h-[150px] w-full overflow-visible">
        {band && <rect x="0" y={y(band.high)} width="840" height={Math.max(1, y(band.low) - y(band.high))} fill="#64748b" fillOpacity="0.3"><title>Current PLC band, not historical setpoints</title></rect>}
        <path d="M0 0 V150 H840" fill="none" stroke="#64748b" vectorEffect="non-scaling-stroke" />
        {history.gaps.map((gap, i) => <rect key={i} x={Math.max(0, x(gap.from))} y="0" width={Math.max(0, Math.min(840, x(gap.to)) - Math.max(0, x(gap.from)))} height="150" fill="#fbbf24" fillOpacity="0.15"><title>{`Ingest gap ${gap.from} to ${gap.to}`}</title></rect>)}
        {runs.map((run, i) => <g key={i} data-history-run="true">
          <polyline points={run.map((s) => `${x(s.ingested_at)},${y(numericSample(s)!)}`).join(" ")} fill="none" stroke="#38bdf8" strokeWidth="2" vectorEffect="non-scaling-stroke" />
          {run.map((s) => <circle key={s.snapshot_seq} cx={x(s.ingested_at)} cy={y(numericSample(s)!)} r="1" fill="#38bdf8" stroke="#38bdf8" strokeWidth="2" vectorEffect="non-scaling-stroke"><title>{`${s.ingested_at}: ${formatPointValue(history.point, s.value, s.quality)}`}</title></circle>)}
        </g>)}
        {eventNotes.markers.map((event) => <g key={`event-${event.seq}`} data-trend-event={event.label}>
          <line x1={x(event.at)} x2={x(event.at)} y1="0" y2="18" stroke={event.critical ? "#f43f5e" : "#fbbf24"} strokeWidth="2" strokeDasharray={event.completedCommand ? undefined : "2 2"} vectorEffect="non-scaling-stroke" />
          <title>{`${event.label} · CC received ${event.at}${event.completedCommand ? " · completed command feedback" : ""}`}</title>
        </g>)}
      </svg>
      <div className="col-start-2 flex min-w-0 flex-wrap justify-between gap-x-4 gap-y-1">
        <span className="whitespace-nowrap">{formatUtcTime(new Date(xmin).toISOString())} UTC</span>
        <span className="ml-auto whitespace-nowrap">{formatUtcTime(new Date(xmax).toISOString())} UTC</span>
      </div>
    </div> : <p className="text-sm text-amber-300">No good numeric observations in this window. Unknown and stale samples are not plotted as zero. Select a numeric or single-point signal for a trend.</p>}
    <TrendEventNotes {...eventNotes} available={Boolean(journalEvents)} />
    {band && <p className="text-xs text-slate-300">Current PLC band {band.low.toFixed(2)}–{band.high.toFixed(2)} kV, not historical setpoints.</p>}
    <ul className="text-xs text-amber-300">{history.gaps.map((gap, i) => <li key={i}>Ingest gap: {gap.from} → {gap.to}</li>)}</ul>
    <p className="text-xs text-slate-400">{history.segments.length} cache-epoch segment(s). Lines never cross an epoch or a gap. Invalid, substituted, blocked, overflow and transient samples break the line.</p>
    <HistorySnapshots history={history} expandableSnapshots={expandableSnapshots} />
  </div>;
}

export function SectionHistoryPlot({ histories, journalEvents, expandableSnapshots = false }: { histories: History[]; journalEvents?: TrendJournalEvents; expandableSnapshots?: boolean }) {
  const plotted = SECTION_SIGNALS.flatMap((series) => {
    const history = histories.find((item) => item.point === series.id);
    return history ? [{ ...series, history, runs: historyRuns(history) }] : [];
  });
  const values = plotted.flatMap((series) => series.runs.flat()).map((sample) => numericSample(sample)!);
  const samples = plotted.flatMap((series) => series.history.segments.flatMap((segment) => segment.samples));
  const times = samples.map((sample) => Date.parse(sample.ingested_at)).filter(Number.isFinite);
  const xmin = Math.min(...times), xmax = Math.max(...times);
  const ymin = Math.min(...values), ymax = Math.max(...values);
  const x = (time: string) => (Date.parse(time) - xmin) / (xmax - xmin || 1) * 840;
  const y = (value: number) => 135 - (value - ymin) / (ymax - ymin || 1) * 120;
  const eventNotes = visibleJournalEvents(histories, journalEvents);
  const gapMap = new Map(plotted.flatMap((series) => series.history.gaps.map((gap) => [`${gap.from}/${gap.to}`, gap] as const)));
  const snapshotHistory = histories.find((history) => history.point === "rmu-1-section-kv");
  return <div className="min-w-0 space-y-3 wrap-anywhere">
    <ul aria-label="Section voltage series legend" className="flex flex-wrap gap-x-5 gap-y-2 text-xs text-slate-200">
      {SECTION_SIGNALS.map((series) => <li key={series.id} className="flex items-center gap-2">
        <svg aria-hidden="true" viewBox="0 0 32 8" className="h-2 w-8 overflow-visible"><line x1="0" x2="32" y1="4" y2="4" stroke={series.color} strokeWidth="2" strokeDasharray={series.dash} /></svg>
        <span>{series.label} · {series.id === "rmu-1-source-kv" ? "RMU1 source VT" : series.id === "rmu-1-section-kv" ? "RMU1 section VT (S2)" : "RMU2 source VT"}</span>
      </li>)}
    </ul>
    {values.length ? <div className="grid min-w-0 grid-cols-[3.5rem_minmax(0,1fr)] gap-x-1 gap-y-2 pt-2 text-xs text-slate-300 sm:pr-10">
      <div className="relative">
        <span className="absolute right-0 top-[10%] -translate-y-1/2">{ymax.toFixed(2)}</span>
        <span className="absolute right-0 top-[90%] -translate-y-1/2">{ymin.toFixed(2)}</span>
      </div>
      <svg viewBox="0 0 840 150" preserveAspectRatio="none" role="img" aria-label="S1, S2 and S3 section voltage trends by historian ingest time UTC; lines break at epochs, gaps and invalid samples" className="aspect-840/150 h-auto min-h-[150px] w-full overflow-visible">
        <path d="M0 0 V150 H840" fill="none" stroke="#64748b" vectorEffect="non-scaling-stroke" />
        {[...gapMap.values()].map((gap) => <rect key={`${gap.from}/${gap.to}`} x={Math.max(0, x(gap.from))} y="0" width={Math.max(0, Math.min(840, x(gap.to)) - Math.max(0, x(gap.from)))} height="150" fill="#fbbf24" fillOpacity="0.15"><title>{`Ingest gap ${gap.from} to ${gap.to}`}</title></rect>)}
        {plotted.map((series) => series.runs.map((run, i) => <g key={`${series.id}-${i}`} data-history-run={series.id}>
          <polyline points={run.map((sample) => `${x(sample.ingested_at)},${y(numericSample(sample)!)}`).join(" ")} fill="none" stroke={series.color} strokeWidth="2" strokeDasharray={series.dash} vectorEffect="non-scaling-stroke" />
          {run.map((sample) => <circle key={sample.snapshot_seq} cx={x(sample.ingested_at)} cy={y(numericSample(sample)!)} r="1" fill="#020617" stroke={series.color} strokeWidth="2" vectorEffect="non-scaling-stroke"><title>{`${series.label} ${sample.ingested_at}: ${formatPointValue(series.id, sample.value, sample.quality)}`}</title></circle>)}
        </g>))}
        {eventNotes.markers.map((event) => <g key={`event-${event.seq}`} data-trend-event={event.label}>
          <line x1={x(event.at)} x2={x(event.at)} y1="0" y2="18" stroke={event.critical ? "#f43f5e" : "#fbbf24"} strokeWidth="2" strokeDasharray={event.completedCommand ? undefined : "2 2"} vectorEffect="non-scaling-stroke" />
          <title>{`${event.label} · CC received ${event.at}${event.completedCommand ? " · completed command feedback" : ""}`}</title>
        </g>)}
      </svg>
      <div className="col-start-2 flex min-w-0 flex-wrap justify-between gap-x-4 gap-y-1">
        <span className="whitespace-nowrap">{formatUtcTime(new Date(xmin).toISOString())} UTC</span>
        <span className="ml-auto whitespace-nowrap">{formatUtcTime(new Date(xmax).toISOString())} UTC</span>
      </div>
    </div> : <p className="text-sm text-amber-300">No good numeric section observations in this window. Unknown and stale samples are not plotted as zero.</p>}
    <TrendEventNotes {...eventNotes} available={Boolean(journalEvents)} />
    <p className="text-xs text-slate-400">One shared time axis; each section line independently breaks at cache epochs, ingest gaps, invalid quality or transient samples. S2 uses RMU1 section VT; the RMU2 section VT is the same S2 bus and remains selectable on its own.</p>
    {snapshotHistory
      ? <HistorySnapshots history={snapshotHistory} expandableSnapshots={expandableSnapshots} captionPrefix="S2 snapshots · RMU1 section VT (same S2 bus measured at RMU2)" />
      : <p className="text-xs text-slate-400">S2 snapshot table unavailable: no RMU1 section VT history was received.</p>}
  </div>;
}

function getHistory(point: string) {
  const until = Date.now();
  return getHistorianHistory(point, new Date(until - 15 * 60_000).toISOString(), new Date(until).toISOString());
}

export function HistorianTrend({ points = [], voltageOnly = false, expandableSnapshots = false }: { points?: OperatorPoint[]; voltageOnly?: boolean; expandableSnapshots?: boolean }) {
  const [point, setPoint] = useState(() => voltageOnly ? SECTION_VIEW : "oltc-1-bus-kv");
  const sectionMode = voltageOnly && point === SECTION_VIEW;
  const catalogue = useQuery({ queryKey: ["historian", "points"], queryFn: getHistorianPoints, refetchInterval: 10000 });
  const history = useQuery({
    queryKey: ["historian", "history", point],
    queryFn: () => getHistory(point),
    refetchInterval: 5000,
    enabled: !sectionMode,
  });
  const sectionHistory = useQueries({
    queries: SECTION_SIGNALS.map((series) => ({
      queryKey: ["historian", "history", series.id],
      queryFn: () => getHistory(series.id),
      refetchInterval: 5000,
      enabled: sectionMode,
    })),
  });
  const journal = useQuery({
    queryKey: ["operator", "journal"],
    queryFn: getOperatorJournal,
    refetchInterval: 5000,
    enabled: voltageOnly,
  });
  const sectionHistories = sectionHistory.flatMap((query) => query.data ? [query.data] : []);
  const sectionError = sectionHistory.find((query) => query.error)?.error;
  const error = catalogue.error ?? (sectionMode ? sectionError : history.error);
  const signals = catalogue.data?.filter((p) => !voltageOnly || VOLTAGE_SIGNAL_IDS.includes(p.id));
  const journalEvents = journal.data ? voltageTrendJournalEvents(journal.data) : undefined;
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
    {voltageOnly && journal.error ? <p role="status" className="text-xs text-amber-300">Event journal unavailable: {errorMessage(journal.error)}. {journal.data ? "Showing the last journal answer, not a live feed." : "No switching or protection markers shown."}</p> : null}
    {sectionMode
      ? sectionHistories.length ? <SectionHistoryPlot histories={sectionHistories} journalEvents={journalEvents} expandableSnapshots={expandableSnapshots} /> : !error ? <p className="text-sm text-slate-400">Waiting for section voltage snapshots…</p> : null
      : history.data ? <HistoryPlot history={history.data} band={point === "oltc-1-bus-kv" ? regulationBand(points) : null} expandableSnapshots={expandableSnapshots} journalEvents={voltageOnly ? journalEvents : undefined} /> : !error ? <p className="text-sm text-slate-400">Waiting for historian snapshots…</p> : null}
  </section>;
}
