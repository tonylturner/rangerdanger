"use client";

import type { ReactNode } from "react";
import type { ControlCentreLink, OperatorJournal, OperatorState } from "../lib/operator-api";
import {
  GI_STATE_TEXT,
  LINK_STATE_TEXT,
  ageMs,
  formatAge,
  formatUtcTime,
  telemetrySummary,
  telemetryText,
} from "../lib/operator-format";
import { useNow, useOperatorJournal, useOperatorState } from "../lib/operator-queries";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { BreakerCommandPanel } from "./control-centre-command";
import { ControlCentreJournal } from "./control-centre-journal";
import { ControlCentreOneLine } from "./control-centre-one-line";
import { PointTable, STALE_HATCH } from "./control-centre-points";

const POLL_MS = 2000;

function Cell({ label, children, emphasis }: { label: string; children: ReactNode; emphasis?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`truncate font-mono ${emphasis ? "text-sm font-bold" : "text-[11px] text-slate-300"}`}>{children}</div>
    </div>
  );
}

export function LinkStrip({ link, now }: { link: ControlCentreLink; now: number }) {
  const started = link.state === "started";
  const giDone = link.gi.state === "complete";
  return (
    <section
      aria-label="IEC 104 link"
      className={`rounded-xl border p-3 ${started ? "border-slate-800 bg-slate-900/70" : `border-amber-600 ${STALE_HATCH}`}`}
    >
      {!started && (
        <div role="alert" className="mb-2 text-xs font-bold tracking-wide text-amber-300">
          ⚠ IEC 104 LINK {LINK_STATE_TEXT[link.state]}: telemetry is stale. Every value below is the last one received.
        </div>
      )}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
        <Cell label="Link" emphasis>
          <span className={started ? "text-green-400" : "text-amber-300"}>
            {started ? "● " : "○ "}
            {LINK_STATE_TEXT[link.state]}
          </span>
        </Cell>
        <Cell label="General interrogation" emphasis>
          <span className={giDone ? "text-green-400" : "text-amber-300"}>{GI_STATE_TEXT[link.gi.state]}</span>
        </Cell>
        <Cell label="RTU (peer)">{link.peer}</Cell>
        <Cell label="CC (local)">{link.local ?? "—"}</Cell>
        <Cell label="Since (UTC)">{formatUtcTime(link.since)}</Cell>
        <Cell label="Last receive">{formatAge(ageMs(link.last_rx_at, now))} ago</Cell>
        <Cell label="Connection #">{link.connection_seq}</Cell>
        <Cell label="GI complete (UTC)">{formatUtcTime(link.gi.completed_at)}</Cell>
      </div>
    </section>
  );
}

function Panel({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="rounded-xl border border-slate-800 bg-slate-950 p-4">
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-[10px] font-bold uppercase tracking-wider text-slate-400">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

export function ControlCentreScreen({
  state,
  stateError,
  stateUpdatedAt,
  journal,
  now,
}: {
  state: OperatorState | undefined;
  stateError: unknown;
  stateUpdatedAt: number;
  journal: OperatorJournal | undefined;
  now: number;
}) {
  const waiting = !state && isRangeNotReady(stateError);
  const apiDown = stateError !== null && stateError !== undefined && !isRangeNotReady(stateError);
  const summary = state ? telemetrySummary(state.points) : null;
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-4 px-6 py-10">
      <header>
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">20 kV feeder — IEC 60870-5-104 telecontrol</p>
        <h1 className="text-2xl font-bold text-white">Control Centre</h1>
        <p className="mt-1 max-w-3xl text-sm text-slate-500">
          What the control centre knows, learned only over IEC 104 from the RTU at 10.30.30.20:2404. When the link is
          lost it keeps the last values and flags them stale (IV, NT) with the reason and their age.
        </p>
      </header>

      {apiDown && (
        <div role="alert" className="rounded-lg border border-red-800/70 bg-red-950/30 px-4 py-2 text-xs text-red-200">
          <span className="font-bold text-red-400">Control centre API unavailable</span>: {errorMessage(stateError)}.{" "}
          {state
            ? `Showing the last answer, received ${formatUtcTime(new Date(stateUpdatedAt).toISOString())} UTC (${formatAge(now - stateUpdatedAt)} ago).`
            : "Nothing received yet."}
        </div>
      )}

      {waiting && (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 px-4 py-3 text-sm text-slate-300">
          The control centre shows once the range is ready.
        </div>
      )}

      {!state && !waiting && !apiDown && (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 px-4 py-3 text-sm text-slate-400">
          Loading the control centre…
        </div>
      )}

      {state && summary && (
        <>
          <LinkStrip link={state.link} now={now} />
          <div className="grid gap-4 lg:grid-cols-[3fr_2fr]">
            <Panel title="Feeder one-line">
              <ControlCentreOneLine points={state.points} />
            </Panel>
            <Panel title="Supervisory control">
              <BreakerCommandPanel
                linkState={state.link.state}
                position={state.points.find((p) => p.id === "feeder-breaker-position")}
              />
            </Panel>
          </div>
          <Panel
            title="Monitored points"
            aside={
              <span className={`font-mono text-[11px] ${summary.good === summary.total ? "text-slate-400" : "font-bold text-amber-300"}`}>
                {telemetryText(summary)}
              </span>
            }
          >
            <PointTable points={state.points} now={now} />
          </Panel>
          <Panel title="Control centre journal">
            <ControlCentreJournal journal={journal} />
          </Panel>
          <p className="text-[10px] text-slate-600">
            cache epoch {state.cache_epoch} · range generation {state.range_generation} · package {state.package_id}
          </p>
        </>
      )}
    </main>
  );
}

// The EU package's operator screen.
export function ControlCentreView() {
  const stateQuery = useOperatorState(POLL_MS);
  const journal = useOperatorJournal(3000).data;
  const now = useNow(1000);
  return (
    <ControlCentreScreen
      state={stateQuery.data}
      stateError={stateQuery.error}
      stateUpdatedAt={stateQuery.dataUpdatedAt}
      journal={journal}
      now={now}
    />
  );
}
