"use client";

import Link from "next/link";
import type { OperatorState } from "../lib/operator-api";
import { LINK_STATE_TEXT, formatPointValue, pointCondition, telemetrySummary, telemetryText } from "../lib/operator-format";
import { useNow, useOperatorState } from "../lib/operator-queries";
import { EU_OPERATOR_VIEW, isView, operatorViewProblem, type OperatorViewResolution } from "../lib/operator-view";
import { errorMessage } from "../lib/utils";
import { isRangeNotReady } from "../lib/range";
import { MiniStatus } from "./scenario-runner-widgets";

export function ControlCentreMiniStatus({ state, waiting }: { state: OperatorState | undefined; waiting: boolean }) {
  if (!state) {
    return (
      <div className="grid grid-cols-2 gap-2">
        {["IEC 104 Link", "Interrogation", "Telemetry", "Breaker (as reported)"].map((label) => (
          <MiniStatus key={label} label={label} value={waiting ? "WAITING" : "—"} />
        ))}
      </div>
    );
  }
  const summary = telemetrySummary(state.points);
  const position = state.points.find((p) => p.id === "feeder-breaker-position");
  const positionStale = position ? pointCondition(position) !== "good" : true;
  return (
    <div className="grid grid-cols-2 gap-2">
      <MiniStatus label="IEC 104 Link" value={LINK_STATE_TEXT[state.link.state]} ok={state.link.state === "started"} />
      <MiniStatus label="Interrogation" value={state.link.gi.last_complete_at !== null ? "GI ok" : state.link.gi.state.toUpperCase()} ok={state.link.gi.last_complete_at !== null} />
      <MiniStatus label="Telemetry" value={telemetryText(summary)} ok={summary.total > 0 && summary.good === summary.total} />
      <MiniStatus
        label="Breaker (as reported)"
        value={`${position ? formatPointValue(position.id, position.value) : "not reported"}${positionStale ? " ⚠ STALE" : ""}`}
        ok={positionStale ? undefined : true}
      />
    </div>
  );
}

function EuStatus() {
  const query = useOperatorState(3000);
  const now = useNow(1000);
  return (
    <>
      <div className="flex items-baseline justify-between">
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Control Centre Status</div>
        <Link href="/substation" className="text-[10px] text-sky-400 hover:text-sky-300">
          Open the Control Centre →
        </Link>
      </div>
      {query.error && <p role="alert" className="text-xs text-amber-300">Operator API unavailable: {errorMessage(query.error)}. {query.data ? `Cached snapshot · ${Math.max(0, Math.floor((now - query.dataUpdatedAt) / 1000))} s since last API answer; not live.` : "No operator snapshot."}</p>}
      <ControlCentreMiniStatus state={query.data} waiting={!query.data && isRangeNotReady(query.error)} />
    </>
  );
}

// The exercise runner's operator status for packages without the US
// RTAC feeder.
export function RunnerOperatorStatus({ resolution }: { resolution: OperatorViewResolution }) {
  if (isView(resolution, EU_OPERATOR_VIEW)) return <EuStatus />;
  const problem = operatorViewProblem(resolution);
  if (problem === null) return null;
  return <div className="text-[11px] text-red-300">{problem}</div>;
}
