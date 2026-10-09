"use client";

import { useQuery } from "@tanstack/react-query";
import { getWorkshopStatus } from "../lib/api";
import type { OperatorState } from "../lib/operator-api";
import { GI_STATE_TEXT, LINK_STATE_TEXT, telemetrySummary, telemetryText } from "../lib/operator-format";
import { useOperatorState } from "../lib/operator-queries";
import { isRangeNotReady } from "../lib/range";
import { DashboardStatusCard, QuickLink, WorkshopStatusCard } from "./overview-cards";

// Overview and workshop status for the EU IEC 104 package. They read
// the generic workshop status (firewall, scenarios) and the control
// centre; there is no RTAC or US field-device health here.

function useEuStatus() {
  const ws = useQuery({ queryKey: ["workshop-status"], queryFn: getWorkshopStatus, refetchInterval: 5000 });
  const operator = useOperatorState(5000);
  return {
    ws: ws.data,
    waiting: isRangeNotReady(ws.error),
    operator: operator.data,
    operatorDown: !!operator.error && !isRangeNotReady(operator.error),
  };
}

function linkOk(operator: OperatorState | undefined): boolean | undefined {
  return operator ? operator.link.state === "started" : undefined;
}

export function EuDashboard({ title }: { title: string }) {
  const { ws, waiting, operator, operatorDown } = useEuStatus();
  const summary = operator ? telemetrySummary(operator.points) : null;
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <header>
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">OT/ICS Cyber Range</p>
        <h1 className="text-3xl font-bold text-white">{title}</h1>
        <p className="mt-1 max-w-2xl text-sm text-slate-500">
          A 20 kV distribution feeder telecontrolled over IEC 60870-5-104. Segment the station so only the control
          centre reaches the RTU, then see what the operator loses when the firewall cuts the link.
        </p>
      </header>

      <div className="grid gap-3 sm:grid-cols-4">
        <DashboardStatusCard
          label="Control Centre Link"
          value={waiting ? "WAITING" : operatorDown ? "API DOWN" : operator ? `${LINK_STATE_TEXT[operator.link.state]} · GI ${GI_STATE_TEXT[operator.link.gi.state]}` : "—"}
          ok={waiting ? undefined : operatorDown ? false : linkOk(operator)}
        />
        <DashboardStatusCard
          label="Telemetry"
          value={waiting ? "WAITING" : summary ? telemetryText(summary) : "—"}
          ok={waiting || !summary ? undefined : summary.total > 0 && summary.good === summary.total}
        />
        <DashboardStatusCard
          label="Firewall Policy"
          value={waiting ? "WAITING" : ws?.firewall_config === "improved" ? "Hardened" : "Weak Baseline"}
          ok={waiting ? undefined : ws?.firewall_config === "improved"}
        />
        <DashboardStatusCard
          label="Scenarios"
          value={waiting ? "WAITING" : `${ws?.scenario_count ?? 0} available`}
          ok={waiting ? undefined : (ws?.scenario_count ?? 0) > 0}
        />
      </div>

      <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
        <h2 className="mb-3 text-sm font-bold text-white">Workshop Quick Start</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <QuickLink href="/exercises" title="Start Exercises" desc="Telecontrol boundary scenario with validation" color="sky" />
          <QuickLink
            href="/substation"
            title="Control Centre"
            desc="Operator one-line, point quality, select-before-operate, journal"
            color="orange"
          />
          <QuickLink href="/console" title="Network Console" desc="Topology visualization with firewall policy overlays" color="purple" />
        </div>
      </div>
    </main>
  );
}

export function EuWorkshopStatus({ title }: { title: string }) {
  const { ws, waiting, operator, operatorDown } = useEuStatus();
  const summary = operator ? telemetrySummary(operator.points) : null;
  const linkStatus = waiting
    ? "waiting"
    : operatorDown || !operator
      ? "offline"
      : operator.link.state === "started"
        ? "online"
        : operator.link.state === "connecting"
          ? "degraded"
          : "offline";
  const telemetryStatus = waiting
    ? "waiting"
    : !summary || summary.good === 0
      ? "offline"
      : summary.good === summary.total
        ? "online"
        : "degraded";
  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <header className="mb-6">
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">Workshop Environment</p>
        <h1 className="text-3xl font-bold text-white">{title}</h1>
        <p className="mt-1 text-sm text-slate-500">IEC 104 telecontrol workshop. All services are managed by Docker Compose.</p>
      </header>

      <div className="space-y-4">
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
          <h2 className="mb-3 text-sm font-bold text-white">Infrastructure Status</h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <WorkshopStatusCard
              label="containd NGFW"
              status={waiting ? "waiting" : ws?.firewall_online ? "online" : "offline"}
              detail={ws?.firewall_config === "improved" ? "Hardened" : "Weak Baseline"}
            />
            <WorkshopStatusCard
              label="Control Centre Link"
              status={linkStatus}
              detail={operatorDown ? "control centre API unavailable" : "10.60.60.20 → 10.30.30.20:2404"}
            />
            <WorkshopStatusCard
              label="RTU Telemetry"
              status={telemetryStatus}
              detail={summary ? telemetryText(summary) : "—"}
            />
            <WorkshopStatusCard
              label="Scenarios"
              status={waiting ? "waiting" : ws?.scenario_count && ws.scenario_count > 0 ? "online" : "offline"}
              detail={`${ws?.scenario_count ?? 0} available`}
            />
          </div>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
          <h2 className="mb-3 text-sm font-bold text-white">Workshop Resources</h2>
          <div className="grid gap-3 sm:grid-cols-3">
            <QuickLink href="/exercises" title="Start Exercises" desc="Guided telecontrol boundary scenario" color="sky" />
            <QuickLink href="/instructor" title="Instructor Truth" desc="Device and solver truth, fault injection, session cut" color="orange" />
            <QuickLink href="/console" title="Network Map" desc="Interactive topology with node inspector" color="purple" />
          </div>
        </div>
      </div>
    </main>
  );
}
