"use client";

import { useQuery } from "@tanstack/react-query";
import { MetricsOverview } from "./metrics-overview";
import { DashboardStatusCard, QuickLink } from "./overview-cards";
import { getWorkshopStatus } from "../lib/api";
import { isRangeNotReady } from "../lib/range";

// The US package's overview: RTAC, field devices, feeder metrics.
export function UsDashboard() {
  const { data: ws, error: wsError } = useQuery({
    queryKey: ["workshop-status"],
    queryFn: getWorkshopStatus,
    refetchInterval: 5000,
  });

  // While the range is being replaced nothing is offline, it is not
  // there yet; the cards stay neutral and the range banner says why.
  const waiting = isRangeNotReady(wsError);

  const rtacOnline = ws?.rtac_online ?? false;
  const deviceCount = ws?.device_comms ? Object.keys(ws.device_comms).length : 0;
  const devicesOk = ws?.device_comms ? Object.values(ws.device_comms).filter(Boolean).length : 0;
  const scenarioCount = ws?.scenario_count ?? 0;

  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
      <header>
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">
          OT/ICS Cyber Range
        </p>
        <h1 className="text-3xl font-bold text-white">Substation Segmentation Lab</h1>
        <p className="mt-1 max-w-2xl text-sm text-slate-500">
          Electric co-op distribution substation. Validate network segmentation through
          guided attack scenarios targeting ICS field devices via containd NGFW.
        </p>
      </header>

      {/* Quick status strip */}
      <div className="grid gap-3 sm:grid-cols-4">
        <DashboardStatusCard
          label="RTAC Controller"
          value={waiting ? "WAITING" : rtacOnline ? "ONLINE" : "OFFLINE"}
          ok={waiting ? undefined : rtacOnline}
        />
        <DashboardStatusCard
          label="Field Devices"
          value={waiting ? "WAITING" : `${devicesOk}/${deviceCount}`}
          ok={waiting ? undefined : devicesOk === deviceCount && deviceCount > 0}
        />
        <DashboardStatusCard
          label="Firewall Policy"
          value={waiting ? "WAITING" : ws?.firewall_config === "improved" ? "Hardened" : "Weak Baseline"}
          ok={waiting ? undefined : ws?.firewall_config === "improved"}
        />
        <DashboardStatusCard
          label="Scenarios"
          value={waiting ? "WAITING" : `${scenarioCount} available`}
          ok={waiting ? undefined : scenarioCount > 0}
        />
      </div>

      {/* Workshop quick-start */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Workshop Quick Start</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <QuickLink
            href="/scenarios"
            title="Start Exercises"
            desc="Step-by-step attack/defend scenarios with validation"
            color="sky"
          />
          <QuickLink
            href="/substation"
            title="Substation Process View"
            desc="Live feeder diagram, control commands, cyber-to-process correlation"
            color="orange"
          />
          <QuickLink
            href="/console"
            title="Network Console"
            desc="Topology visualization with firewall policy overlays"
            color="purple"
          />
        </div>
      </div>

      <MetricsOverview />
    </main>
  );
}
