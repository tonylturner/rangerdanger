"use client";
import { useState } from "react";
import type { OperatorJournal, OperatorState } from "../lib/operator-api";
import { pointCondition } from "../lib/operator-format";
import { activeAlarms } from "../lib/operator-workstation";
import { errorMessage } from "../lib/utils";
import { ControlCentreJournal } from "./control-centre-journal";
import { PointTable } from "./control-centre-points";
import { HistorianTrend } from "./historian-trend";

export type PaneTab = "Alarms" | "Events" | "Points" | "Trends";
export type PointFilter = "all" | "bad quality" | "status" | "measurands";
export function ControlCentrePane({ state, journal, journalError, now, initialTab = "Alarms" }: { state: OperatorState; journal: OperatorJournal | undefined; journalError?: unknown; now: number; initialTab?: PaneTab }) {
  const [tab, setTab] = useState<PaneTab>(initialTab), [filter, setFilter] = useState<PointFilter>("all");
  const alarms = activeAlarms(state), bad = state.points.filter((p) => pointCondition(p) !== "good");
  const points = state.points.filter((p) => filter === "all" || (filter === "bad quality" ? pointCondition(p) !== "good" : filter === "status" ? p.kind !== "float" : p.kind === "float"));
  const tabs: PaneTab[] = ["Alarms", "Events", "Points", "Trends"];
  return <div className="min-w-0 space-y-3">
    <div role="tablist" aria-label="Operator records" className="flex flex-wrap gap-1 border-b border-slate-700 pb-2">
      {tabs.map((t, i) => <button key={t} role="tab" id={`cc-tab-${t}`} aria-selected={tab === t} aria-controls={`cc-pane-${t}`} tabIndex={tab === t ? 0 : -1} className={`rounded px-3 py-2 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400 ${tab === t ? "bg-slate-700 text-white" : "text-slate-300 hover:bg-slate-800"}`} onClick={() => setTab(t)} onKeyDown={(e) => {
        const index = e.key === "ArrowRight" ? (i + 1) % tabs.length : e.key === "ArrowLeft" ? (i + tabs.length - 1) % tabs.length : e.key === "Home" ? 0 : e.key === "End" ? tabs.length - 1 : null;
        if (index !== null) { e.preventDefault(); setTab(tabs[index]); document.getElementById(`cc-tab-${tabs[index]}`)?.focus(); }
      }}>{t}{t === "Alarms" ? ` (${alarms.length})` : t === "Events" && journalError ? " ⚠" : ""}</button>)}
    </div>
    <div role="tabpanel" id={`cc-pane-${tab}`} aria-labelledby={`cc-tab-${tab}`} className="min-w-0">
      {tab === "Alarms" && <div className="space-y-3 text-sm">
        <p className="text-slate-400">Derived from GOOD-quality indications only. No acknowledgement. Bad quality is uncertainty, not a process alarm.</p>
        {alarms.length ? <ul className="divide-y divide-slate-800 text-amber-200">{alarms.map((a) => <li key={a.id} className="py-2">⚠ {a.text}</li>)}</ul> : <p className="text-slate-200">No active alarms in received GOOD indications.</p>}
        <button className="rounded border border-slate-600 px-3 py-2 text-slate-200 focus-visible:outline focus-visible:outline-sky-400" onClick={() => { setFilter("bad quality"); setTab("Points"); }}>{bad.length} {bad.length === 1 ? "point" : "points"} with bad quality · open Points</button>
      </div>}
      {tab === "Events" && <>{journalError ? <p role="alert" className="mb-3 text-xs text-amber-300">Journal unavailable: {errorMessage(journalError)}. {journal ? "Showing cached events, not a live journal." : "No journal received."}</p> : null}{journal || !journalError ? <ControlCentreJournal journal={journal} /> : null}</>}
      {tab === "Points" && <div className="space-y-3"><label className="text-xs text-slate-300">Show <select aria-label="Point filter" className="rounded border border-slate-600 bg-slate-900 p-2" value={filter} onChange={(e) => setFilter(e.target.value as PointFilter)}>{(["all", "bad quality", "status", "measurands"] as const).map((f) => <option key={f}>{f}</option>)}</select></label><div className="max-h-[32rem] overflow-auto"><PointTable points={points} now={now} /></div></div>}
      {tab === "Trends" && <HistorianTrend points={state.points} />}
    </div>
  </div>;
}
