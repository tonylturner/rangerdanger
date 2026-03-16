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
        return next.slice(-60); // Keep last 60 data points
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

  return (
    <div className="space-y-4">
      {/* Status indicators */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <StatusCard
          label="Feeder Breaker"
          value={elec?.breaker_closed ? "CLOSED" : "OPEN"}
          color={elec?.breaker_closed ? "text-green-400" : "text-red-400"}
        />
        <StatusCard
          label="Recloser"
          value={elec?.recloser_closed ? "CLOSED" : "OPEN"}
          color={elec?.recloser_closed ? "text-green-400" : "text-red-400"}
        />
        <StatusCard
          label="Regulator Tap"
          value={elec?.regulator_tap !== undefined ? `${elec.regulator_tap > 0 ? "+" : ""}${elec.regulator_tap}` : "--"}
          color="text-cyan-400"
        />
        <StatusCard
          label="Bus Voltage"
          value={elec?.substation_bus_voltage_v ? `${elec.substation_bus_voltage_v.toFixed(1)}V` : "--"}
          color="text-sky-400"
        />
        <StatusCard
          label="Load"
          value={
            elec
              ? `${(elec.general_load_kw ?? 0) + (elec.critical_load_kw ?? 0)} kW`
              : "--"
          }
          color="text-amber-400"
        />
      </div>

      {/* Device comms status */}
      <div className="flex items-center gap-4 rounded-lg border border-slate-800 bg-slate-900/70 px-4 py-2">
        <span className="text-xs font-medium uppercase tracking-wider text-slate-500">Device Comms</span>
        {Object.entries(comms).map(([name, ok]) => (
          <div key={name} className="flex items-center gap-1.5">
            <span className={`h-2 w-2 rounded-full ${ok ? "bg-green-500" : "bg-red-500 animate-pulse"}`} />
            <span className="text-xs text-slate-400">{name}</span>
          </div>
        ))}
        {error && (
          <span className="ml-auto text-xs text-red-400">{error}</span>
        )}
      </div>

      {/* Charts */}
      <div className="grid gap-4 md:grid-cols-2">
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
          <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
            <span>Feeder Voltage (V)</span>
            <span className="text-xs">downstream / critical load</span>
          </div>
          <ResponsiveContainer width="100%" height={180}>
            <LineChart data={history}>
              <XAxis dataKey="time" stroke="#94a3b8" tick={{ fontSize: 10 }} />
              <YAxis stroke="#94a3b8" domain={[100, 130]} tick={{ fontSize: 10 }} />
              <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b", fontSize: 12 }} />
              <ReferenceLine y={120} stroke="#475569" strokeDasharray="3 3" label={{ value: "120V", fill: "#64748b", fontSize: 10 }} />
              <ReferenceLine y={108} stroke="#ef4444" strokeDasharray="3 3" label={{ value: "Low", fill: "#ef4444", fontSize: 10 }} />
              <ReferenceLine y={132} stroke="#ef4444" strokeDasharray="3 3" label={{ value: "High", fill: "#ef4444", fontSize: 10 }} />
              <Line dataKey="voltage" stroke="#0ea5e9" strokeWidth={2} dot={false} name="Downstream" />
              <Line dataKey="critVoltage" stroke="#22c55e" strokeWidth={2} dot={false} name="Critical Load" />
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
          <div className="mb-2 flex items-center justify-between text-sm text-slate-400">
            <span>Feeder Current (A)</span>
            <span className="text-xs">total load</span>
          </div>
          <ResponsiveContainer width="100%" height={180}>
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

function StatusCard({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/70 px-3 py-2">
      <div className="text-[10px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-lg font-bold ${color}`}>{value}</div>
    </div>
  );
}
