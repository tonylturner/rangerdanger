"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { postOperatorGi, type ControlCentreLink, type OperatorJournal, type OperatorState } from "../lib/operator-api";
import { LINK_STATE_TEXT, ageMs, formatAge, formatUtcTime, telemetrySummary, telemetryText } from "../lib/operator-format";
import { OPERATOR_KEY, useNow, useOperatorJournal, useOperatorState } from "../lib/operator-queries";
import { activeAlarms, abnormalConditions, type DeviceId } from "../lib/operator-workstation";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { DeviceFaceplate } from "./control-centre-faceplate";
import { ControlCentreOneLine } from "./control-centre-one-line";
import { STALE_HATCH } from "./control-centre-points";
import { PhysicalSystem, PHYSICAL_DESCRIPTION } from "./physical-system";
import { ControlCentrePane, type PaneTab } from "./control-centre-pane";

const BUTTON = "rounded border border-slate-600 px-3 py-2 text-xs text-slate-100 hover:bg-slate-800 focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400 disabled:opacity-50";
export type WorkstationView = "control-centre" | "physical-system";
export function viewFromSearch(search: string): WorkstationView {
  return new URLSearchParams(search).get("view") === "physical-system" ? "physical-system" : "control-centre";
}
function Cell({ label, children }: { label: string; children: ReactNode }) {
  return <div className="min-w-0"><dt className="text-xs text-slate-400">{label}</dt><dd className="break-words font-mono text-xs text-slate-100">{children}</dd></div>;
}
export function LinkStrip({ link, now }: { link: ControlCentreLink; now: number }) {
  const integrity = link.gi.last_complete_at !== null;
  return <section aria-label="IEC 104 link" className={`rounded-lg border p-3 ${link.state === "started" ? "border-slate-700 bg-slate-900" : `border-dashed border-amber-600 ${STALE_HATCH}`}`}>
    {link.state !== "started" && <p role="alert" className="mb-2 text-xs text-amber-300">IEC 104 LINK {LINK_STATE_TEXT[link.state]}: telemetry is stale; retained values are not current.</p>}
    <dl className="flex flex-wrap gap-x-6 gap-y-3">
      <Cell label="Link">{LINK_STATE_TEXT[link.state]}</Cell>
      <Cell label="RTU peer">{link.peer}</Cell>
      <Cell label="Since (UTC)">{formatUtcTime(link.since)}</Cell>
      <Cell label="Last frame age">{formatAge(ageMs(link.last_rx_at, now))}</Cell>
      <Cell label="Integrity">{integrity ? "GI ok" : "No completed GI"}</Cell>
      <Cell label="GI last complete (UTC)">{formatUtcTime(link.gi.last_complete_at)}</Cell>
      <Cell label="GI trigger / state">{link.gi.trigger ?? "—"} / {link.gi.state === "running" ? "RUNNING" : link.gi.state}</Cell>
      <Cell label="GI next due (UTC)">{formatUtcTime(link.gi.next_due_at)}</Cell>
    </dl>
  </section>;
}
export function InterrogateStation({ state, unavailable }: { state: OperatorState; unavailable: boolean }) {
  const client = useQueryClient();
  const gi = useMutation({ mutationFn: postOperatorGi, retry: false, onSettled: () => client.invalidateQueries({ queryKey: OPERATOR_KEY }) });
  // Keep disabled while queued; state polling confirms that the CC sent it.
  const queued = gi.isSuccess && (!state.link.gi.started_at || Date.parse(state.link.gi.started_at) < Date.parse(gi.data.requested_at));
  return <div className="space-y-2">
    <button className={BUTTON} disabled={unavailable || state.link.state !== "started" || state.link.gi.state === "running" || gi.isPending || queued} onClick={() => gi.mutate()}>Interrogate station</button>
    {(gi.isPending || queued) && <p role="status" className="text-xs text-slate-300">Operator interrogation requested; awaiting CC start.</p>}
    {gi.error && <p role="alert" className="text-xs text-amber-300">Interrogation refused or unavailable: {errorMessage(gi.error)}. No automatic retry.</p>}
  </div>;
}
export function SnapshotMarker({ age }: { age: string | null }) {
  return age === null ? null : <p role="status" className="mb-2 border border-dashed border-amber-600 p-2 text-xs text-amber-200">Cached snapshot · {age} since last API answer. All link states, values and IEC quality below are from that snapshot, not a live feed.</p>;
}
export function AbnormalSummary({ state }: { state: OperatorState }) {
  const conditions = abnormalConditions(state);
  return <section aria-label="Abnormal conditions" className={`rounded-lg border p-3 text-sm ${conditions.length ? "border-amber-700 bg-amber-950/30 text-amber-200" : "border-slate-700 text-slate-300"}`}>
    {conditions.length ? <ul className="flex flex-wrap gap-x-5 gap-y-2">{conditions.map((c) => <li key={c.id} data-condition={c.id}>⚠ {c.text}</li>)}</ul> : <span data-condition="none">No abnormal conditions</span>}
  </section>;
}
export function ControlCentreScreen({ state, stateError, stateUpdatedAt, journal, journalError, now, physicalSystem, view = "control-centre", onViewChange = () => {}, initialTab = "Alarms", selected = "T1", onSelect = () => {} }: {
  state: OperatorState | undefined; stateError: unknown; stateUpdatedAt: number;
  journal: OperatorJournal | undefined; journalError?: unknown; now: number; physicalSystem?: ReactNode;
  selected?: DeviceId; onSelect?: (id: DeviceId) => void;
  view?: WorkstationView; onViewChange?: (view: WorkstationView) => void; initialTab?: PaneTab;
}) {
  const unavailable = stateError !== null && stateError !== undefined;
  const cachedAge = state && unavailable ? formatAge(Math.max(0, now - stateUpdatedAt)) : null;
  const summary = state ? telemetrySummary(state.points) : null;
  const alarms = state ? activeAlarms(state) : [];
  return <main className="mx-auto flex w-screen min-w-0 max-w-[1600px] md:w-[calc(100vw-12rem)] flex-col gap-4 px-3 py-6 sm:px-6">
    <header>
      <h1 className="text-2xl font-semibold text-white">{view === "control-centre" ? "Control Centre" : "Physical System State"}</h1>
      <p className="mt-1 max-w-3xl text-sm text-slate-400">{view === "control-centre" ? "20 kV teaching feeder · IEC 60870-5-104 observations only. Selected teaching reporting profile, not a universal European default." : PHYSICAL_DESCRIPTION}</p>
      {view === "physical-system" && <p className="mt-2 inline-block rounded-full border border-slate-700 px-2 py-1 text-xs text-slate-300">Simulation observations · Independent of IEC 104</p>}
    </header>
    <div role="group" aria-label="Observation view" className="flex flex-wrap gap-1 self-start rounded-lg border border-slate-700 p-1">
      {([['control-centre', 'Control Centre'], ['physical-system', 'Physical System']] as const).map(([id, label]) => <button key={id} className={`${BUTTON} ${view === id ? "bg-slate-700" : "border-transparent"}`} aria-pressed={view === id} onClick={() => onViewChange(id)}>{label}</button>)}
    </div>
    {unavailable && <p role="alert" className="rounded-lg border border-amber-700 p-3 text-sm text-amber-200">{isRangeNotReady(stateError) ? "Range not ready" : "Control centre API unavailable"}: {errorMessage(stateError)}. {state ? view === "control-centre" ? "Showing the last answer; controls disabled." : "Comparison uses the last CC answer. Physical data is fetched independently." : "Nothing received yet."}</p>}
    {view === "physical-system" ? <>{cachedAge !== null && <p role="status" className="text-xs text-amber-200">Field-vs-CC comparison uses a cached CC snapshot · {cachedAge} since last operator API answer. Field data is fetched independently.</p>}{physicalSystem}</> : <>
      <p className="text-xs text-slate-300">Portal API: {unavailable ? "unavailable" : state ? "available" : "waiting"} · {alarms.length} active alarms{cachedAge !== null ? " in cached snapshot" : ""}</p>
      {!state && <p className="text-sm text-slate-400">{isRangeNotReady(stateError) ? "The control centre shows once the range is ready." : unavailable ? "No operator snapshot available." : "Loading the control centre…"}</p>}
      {state && <section aria-label="CC observation snapshot" data-cached={unavailable} className="min-w-0 space-y-4">
        <div><SnapshotMarker age={cachedAge} /><LinkStrip link={state.link} now={now} /><div className="mt-3"><InterrogateStation state={state} unavailable={unavailable} /></div></div>
        <AbnormalSummary state={state} />
        {/* Keep 12px schematic labels; put the faceplate alongside only when both fit without shrinking the SVG. */}
        <div className="grid min-w-0 items-start gap-4 min-[1920px]:grid-cols-[minmax(0,1fr)_300px]">
          <section aria-label="Feeder schematic" className="min-w-0 rounded-lg border border-slate-700 bg-slate-950 p-3">
            <h2 className="mb-3 text-sm font-semibold text-slate-200">Feeder schematic</h2><SnapshotMarker age={cachedAge} />
            <ControlCentreOneLine points={state.points} unavailable={unavailable} selected={selected} onSelect={onSelect} />
          </section>
          <div className="min-w-0"><SnapshotMarker age={cachedAge} /><DeviceFaceplate key={state.cache_epoch} state={state} selected={selected} unavailable={unavailable} /></div>
        </div>
        <section aria-label="Operator records" className="min-w-0 rounded-lg border border-slate-700 bg-slate-950 p-3">
          <SnapshotMarker age={cachedAge} /><p className="mb-3 text-xs text-slate-400">{summary && telemetryText(summary)}</p>
          <ControlCentrePane state={state} journal={journal} journalError={journalError} now={now} initialTab={initialTab} />
        </section>
        <details className="break-all text-xs text-slate-400"><summary>Snapshot identity</summary>snapshot {state.snapshot_seq} · rendered {state.rendered_at} · cache epoch {state.cache_epoch} · generation {state.range_generation} · package {state.package_id}</details>
      </section>}
    </>}
  </main>;
}
export function ControlCentreView() {
  const state = useOperatorState(2000), journal = useOperatorJournal(3000), now = useNow(1000);
  const [selected, setSelected] = useState<DeviceId>("T1");
  const [selectionActivated, setSelectionActivated] = useState(false);
  const selectDevice = (id: DeviceId) => { setSelected(id); setSelectionActivated(true); };
  const [view, setView] = useState<WorkstationView>("control-centre");
  useEffect(() => {
    const sync = () => setView(viewFromSearch(window.location.search));
    sync(); window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);
  const changeView = (next: WorkstationView) => {
    const url = new URL(window.location.href); url.searchParams.set("view", next);
    window.history.pushState(null, "", url); setView(next);
  };
  return <ControlCentreScreen state={state.data} stateError={state.error} stateUpdatedAt={state.dataUpdatedAt} journal={journal.data} journalError={journal.error} now={now} view={view} onViewChange={changeView} selected={selected} onSelect={selectDevice} physicalSystem={view === "physical-system" ? <PhysicalSystem operator={state.data} operatorUnavailable={!!state.error} now={now} selected={selected} onSelect={selectDevice} revealSelected={selectionActivated} /> : null} />;
}
