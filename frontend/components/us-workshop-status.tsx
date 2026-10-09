"use client";

import { useQuery } from "@tanstack/react-query";
import { getWorkshopStatus, getSubstationHealth } from "../lib/api";
import { isRangeNotReady } from "../lib/range";
import { WorkshopStatusCard } from "./overview-cards";

// The US package's workshop status: RTAC and field-device health.
export function UsWorkshopStatus() {
  const { data: ws, error: wsError } = useQuery({
    queryKey: ["workshop-status"],
    queryFn: getWorkshopStatus,
    refetchInterval: 5000,
  });

  const { data: health } = useQuery({
    queryKey: ["substation-health"],
    queryFn: getSubstationHealth,
    refetchInterval: 5000,
  });

  // While the range is being replaced its devices are not offline, they
  // are not there yet; the range banner says why.
  const waiting = isRangeNotReady(wsError);

  const deviceCount = ws?.device_comms ? Object.keys(ws.device_comms).length : 0;
  const onlineCount = ws?.device_comms ? Object.values(ws.device_comms).filter(Boolean).length : 0;

  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <header className="mb-6">
        <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">Workshop Environment</p>
        <h1 className="text-3xl font-bold text-white">Distribution Substation Lab</h1>
        <p className="mt-1 text-sm text-slate-500">
          Co-op OT segmentation workshop. All services are managed by Docker Compose.
        </p>
      </header>

      <div className="space-y-4">
        {/* Infrastructure Status */}
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
          <h2 className="text-sm font-bold text-white mb-3">Infrastructure Status</h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <WorkshopStatusCard
              label="containd NGFW"
              status={waiting ? "waiting" : ws?.firewall_online ? "online" : "offline"}
              detail={ws?.firewall_config === "improved" ? "Hardened" : "Weak Baseline"}
            />
            <WorkshopStatusCard
              label="RTAC Controller"
              status={waiting ? "waiting" : ws?.rtac_online ? "online" : "offline"}
              detail="10.30.30.20"
            />
            <WorkshopStatusCard
              label="Field Devices"
              status={waiting ? "waiting" : onlineCount === deviceCount && deviceCount > 0 ? "online" : onlineCount > 0 ? "degraded" : "offline"}
              detail={`${onlineCount}/${deviceCount} responding`}
            />
            <WorkshopStatusCard
              label="Scenarios"
              status={waiting ? "waiting" : ws?.scenario_count && ws.scenario_count > 0 ? "online" : "offline"}
              detail={`${ws?.scenario_count ?? 0} available`}
            />
          </div>
        </div>

        {/* Device Communications */}
        {ws?.device_comms && Object.keys(ws.device_comms).length > 0 && (
          <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
            <h2 className="text-sm font-bold text-white mb-3">Device Communications</h2>
            <div className="grid gap-2 sm:grid-cols-3">
              {Object.entries(ws.device_comms).map(([device, online]) => (
                <div key={device} className="flex items-center gap-2 rounded-lg border border-slate-800 bg-slate-950/50 px-3 py-2">
                  <span className={`h-2 w-2 rounded-full ${online ? "bg-green-500" : "bg-red-500"}`} />
                  <span className="text-xs text-slate-300 capitalize">{device}</span>
                  <span className={`ml-auto text-[10px] font-bold ${online ? "text-green-400" : "text-red-400"}`}>
                    {online ? "OK" : "DOWN"}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Quick Links */}
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
          <h2 className="text-sm font-bold text-white mb-3">Workshop Resources</h2>
          <div className="grid gap-3 sm:grid-cols-3">
            <a href="/containd/" target="_blank" rel="noopener noreferrer"
              className="block rounded-lg border border-amber-800/50 bg-slate-950/50 p-3 hover:border-amber-700 transition-colors">
              <div className="text-sm font-bold text-amber-400">containd Firewall</div>
              <div className="mt-0.5 text-[11px] text-slate-500">Direct access to NGFW management UI</div>
            </a>
            <a href="/scenarios"
              className="block rounded-lg border border-sky-800/50 bg-slate-950/50 p-3 hover:border-sky-700 transition-colors">
              <div className="text-sm font-bold text-sky-400">Start Exercises</div>
              <div className="mt-0.5 text-[11px] text-slate-500">Guided attack/defend scenarios</div>
            </a>
            <a href="/console"
              className="block rounded-lg border border-purple-800/50 bg-slate-950/50 p-3 hover:border-purple-700 transition-colors">
              <div className="text-sm font-bold text-purple-400">Network Map</div>
              <div className="mt-0.5 text-[11px] text-slate-500">Interactive topology with node inspector</div>
            </a>
          </div>
        </div>
      </div>
    </main>
  );
}
