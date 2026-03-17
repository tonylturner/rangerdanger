"use client";

import { useEffect, useState, useCallback } from "react";
import {
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  Area,
  AreaChart,
  ReferenceLine,
  ReferenceArea,
} from "recharts";
import { getSubstationState, type SubstationState } from "../lib/api";

type TimePoint = { time: string; voltage: number; current: number; critVoltage: number };

export function MetricsOverview() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [history, setHistory] = useState<TimePoint[]>([]);
  const [error, setError] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const data = await getSubstationState();
      setState(data);
      setError(null);

      const now = new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
      setHistory((prev) => {
        const next = [
          ...prev,
          {
            time: now,
            voltage: data.electrical.downstream_voltage_v ?? 0,
            current: data.electrical.feeder_current_a ?? 0,
            critVoltage: data.electrical.critical_load_voltage_v ?? 0,
          },
        ];
        return next.slice(-60);
      });
    } catch {
      setError("RTAC offline");
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, [poll]);

  const elec = state?.electrical;
  const comms = state?.device_comms ?? {};
  const totalLoad = (elec?.general_load_kw ?? 0) + (elec?.critical_load_kw ?? 0);
  const critV = elec?.critical_load_voltage_v ?? 0;
  const voltageStatus = critV === 0 ? "DEAD" : critV < 108 ? "DANGER" : critV < 114 ? "LOW" : critV > 132 ? "DANGER" : critV > 126 ? "HIGH" : "NORMAL";

  return (
    <div className="space-y-4">
      {/* Key status cards */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <StatusCard
          label="Device 52 — Breaker"
          value={elec?.breaker_closed ? "CLOSED" : "OPEN"}
          color={elec?.breaker_closed ? "text-green-400" : "text-red-400"}
          alert={!elec?.breaker_closed}
        />
        <StatusCard
          label="Device 79 — Recloser"
          value={elec?.recloser_closed ? "CLOSED" : "OPEN"}
          color={elec?.recloser_closed ? "text-green-400" : "text-red-400"}
          alert={!elec?.recloser_closed}
        />
        <StatusCard
          label="Device 90 — Regulator"
          value={elec?.regulator_tap !== undefined ? `Tap ${elec.regulator_tap > 0 ? "+" : ""}${elec.regulator_tap}` : "--"}
          color="text-cyan-400"
        />
        <StatusCard
          label="Critical Load Voltage"
          value={critV > 0 ? `${critV.toFixed(1)}V` : "DEAD"}
          color={voltageStatus === "NORMAL" ? "text-green-400" : voltageStatus === "DEAD" || voltageStatus === "DANGER" ? "text-red-400" : "text-yellow-400"}
          alert={voltageStatus === "DANGER" || voltageStatus === "DEAD"}
          sublabel={voltageStatus !== "NORMAL" && voltageStatus !== "DEAD" ? `ANSI C84.1 ${voltageStatus}` : undefined}
        />
        <StatusCard
          label="Total Feeder Load"
          value={`${totalLoad} kW`}
          color={totalLoad > 0 ? "text-amber-400" : "text-red-400"}
          sublabel={totalLoad > 0 ? `${elec?.general_load_kw ?? 0} gen + ${elec?.critical_load_kw ?? 0} crit` : "ALL LOADS LOST"}
          alert={totalLoad === 0 && elec !== undefined}
        />
      </div>

      {/* Device comms */}
      <div className="flex items-center gap-4 rounded-lg border border-slate-800 bg-slate-900/70 px-4 py-2">
        <span className="text-xs font-medium uppercase tracking-wider text-slate-500">RTAC Comms</span>
        {Object.entries(comms).map(([name, ok]) => (
          <div key={name} className="flex items-center gap-1.5">
            <span className={`h-2 w-2 rounded-full ${ok ? "bg-green-500" : "bg-red-500 animate-pulse"}`} />
            <span className="text-xs text-slate-400">{name}</span>
          </div>
        ))}
        {error && <span className="ml-auto text-xs text-red-400">{error}</span>}
      </div>

      {/* Voltage chart with ANSI C84.1 bands */}
      <div className="grid gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
          <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
            <span>Feeder Voltage</span>
            <div className="flex items-center gap-3 text-[10px]">
              <span className="flex items-center gap-1"><span className="inline-block h-2 w-3 bg-sky-500 rounded-sm" /> Downstream</span>
              <span className="flex items-center gap-1"><span className="inline-block h-2 w-3 bg-green-500 rounded-sm" /> Critical Load</span>
            </div>
          </div>
          <ResponsiveContainer width="100%" height={200}>
            <LineChart data={history}>
              <XAxis dataKey="time" stroke="#94a3b8" tick={{ fontSize: 10 }} />
              <YAxis stroke="#94a3b8" domain={[95, 135]} tick={{ fontSize: 10 }} />
              <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b", fontSize: 12 }} />
              {/* ANSI C84.1 Range A (normal) */}
              <ReferenceArea y1={114} y2={126} fill="#22c55e" fillOpacity={0.05} />
              {/* Range B warning bands */}
              <ReferenceArea y1={108} y2={114} fill="#f59e0b" fillOpacity={0.05} />
              <ReferenceArea y1={126} y2={132} fill="#f59e0b" fillOpacity={0.05} />
              <ReferenceLine y={120} stroke="#475569" strokeDasharray="3 3" label={{ value: "120V nom", fill: "#64748b", fontSize: 9 }} />
              <ReferenceLine y={108} stroke="#ef4444" strokeDasharray="3 3" label={{ value: "108V", fill: "#ef4444", fontSize: 9 }} />
              <ReferenceLine y={132} stroke="#ef4444" strokeDasharray="3 3" label={{ value: "132V", fill: "#ef4444", fontSize: 9 }} />
              <Line dataKey="voltage" stroke="#0ea5e9" strokeWidth={2} dot={false} name="Downstream (V)" />
              <Line dataKey="critVoltage" stroke="#22c55e" strokeWidth={2} dot={false} name="Critical Load (V)" />
            </LineChart>
          </ResponsiveContainer>
          <div className="mt-1 flex justify-center gap-4 text-[9px] text-slate-600">
            <span>Range A: 114–126V</span>
            <span>Range B: 108–132V</span>
            <span>Danger: &lt;108V or &gt;132V</span>
          </div>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
          <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
            <span>Feeder Current</span>
            <span className="text-[10px]">total load current</span>
          </div>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={history}>
              <defs>
                <linearGradient id="currentGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#f97316" stopOpacity={0.6} />
                  <stop offset="95%" stopColor="#f97316" stopOpacity={0} />
                </linearGradient>
              </defs>
              <XAxis dataKey="time" stroke="#94a3b8" tick={{ fontSize: 10 }} />
              <YAxis stroke="#94a3b8" tick={{ fontSize: 10 }} />
              <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b", fontSize: 12 }} />
              <Area type="monotone" dataKey="current" stroke="#f97316" fillOpacity={1} fill="url(#currentGrad)" name="Current (A)" />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      </div>
    </div>
  );
}

function StatusCard({
  label,
  value,
  color,
  alert,
  sublabel,
}: {
  label: string;
  value: string;
  color: string;
  alert?: boolean;
  sublabel?: string;
}) {
  return (
    <div className={`rounded-lg border px-3 py-2 ${
      alert ? "border-red-800 bg-red-950/30" : "border-slate-800 bg-slate-900/70"
    }`}>
      <div className="text-[10px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-lg font-bold ${color} ${alert ? "animate-pulse" : ""}`}>{value}</div>
      {sublabel && <div className="text-[10px] text-slate-500">{sublabel}</div>}
    </div>
  );
}
