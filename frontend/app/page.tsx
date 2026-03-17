"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { MetricsOverview } from "../components/metrics-overview";
import { TopologyPreview } from "../components/topology-preview";
import { listScenarios, getSubstationHealth, getActiveFirewallConfig } from "../lib/api";

export default function DashboardPage() {
  const { data: scenarios } = useQuery({
    queryKey: ["scenarios", "all"],
    queryFn: () => listScenarios(),
  });

  const { data: health } = useQuery({
    queryKey: ["substation-health"],
    queryFn: getSubstationHealth,
    refetchInterval: 5000,
  });

  const { data: fwConfig } = useQuery({
    queryKey: ["fw-active"],
    queryFn: getActiveFirewallConfig,
    refetchInterval: 10000,
  });

  const rtacOnline = health?.status === "ok" || health?.status === "healthy";
  const deviceCount = health?.device_comms ? Object.keys(health.device_comms).length : 0;
  const devicesOk = health?.device_comms ? Object.values(health.device_comms).filter(Boolean).length : 0;
  const scenarioCount = scenarios?.scenarios.length ?? 0;

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
        <StatusCard
          label="RTAC Controller"
          value={rtacOnline ? "ONLINE" : "OFFLINE"}
          ok={rtacOnline}
        />
        <StatusCard
          label="Field Devices"
          value={`${devicesOk}/${deviceCount}`}
          ok={devicesOk === deviceCount && deviceCount > 0}
        />
        <StatusCard
          label="Firewall Policy"
          value={fwConfig?.active_config === "improved" ? "Hardened" : "Weak Baseline"}
          ok={fwConfig?.active_config === "improved"}
        />
        <StatusCard
          label="Scenarios"
          value={`${scenarioCount} available`}
          ok={scenarioCount > 0}
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

      <div>
        <h2 className="mb-2 text-sm font-bold text-white">Network Architecture</h2>
        <TopologyPreview />
      </div>
    </main>
  );
}

function StatusCard({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <div className={`rounded-lg border px-3 py-2 ${
      ok === false ? "border-red-800/60 bg-red-950/20" : ok === true ? "border-green-800/40 bg-slate-900/70" : "border-slate-800 bg-slate-900/70"
    }`}>
      <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm font-bold ${
        ok === false ? "text-red-400" : ok === true ? "text-green-400" : "text-slate-400"
      }`}>
        {value}
      </div>
    </div>
  );
}

function QuickLink({ href, title, desc, color }: { href: string; title: string; desc: string; color: string }) {
  const borderColor = color === "sky" ? "border-sky-800/50 hover:border-sky-700" : color === "orange" ? "border-orange-800/50 hover:border-orange-700" : "border-purple-800/50 hover:border-purple-700";
  const titleColor = color === "sky" ? "text-sky-400" : color === "orange" ? "text-orange-400" : "text-purple-400";
  return (
    <Link
      href={href}
      className={`block rounded-lg border bg-slate-950/50 p-3 transition-colors ${borderColor}`}
    >
      <div className={`text-sm font-bold ${titleColor}`}>{title}</div>
      <div className="mt-1 text-[11px] text-slate-500">{desc}</div>
    </Link>
  );
}
