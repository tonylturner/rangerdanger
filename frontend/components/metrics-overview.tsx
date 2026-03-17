"use client";

import { useEffect, useState, useCallback } from "react";
import {
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  YAxis,
  ReferenceLine,
  ReferenceArea,
} from "recharts";
import { getSubstationState, type SubstationState } from "../lib/api";

type TimePoint = { time: string; voltage: number; critVoltage: number };

export function MetricsOverview() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [history, setHistory] = useState<TimePoint[]>([]);

  const poll = useCallback(async () => {
    try {
      const data = await getSubstationState();
      setState(data);

      const now = new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
      setHistory((prev) => [
        ...prev,
        {
          time: now,
          voltage: data.electrical.downstream_voltage_v ?? 0,
          critVoltage: data.electrical.critical_load_voltage_v ?? 0,
        },
      ].slice(-60));
    } catch {
      // offline
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 2000);
    return () => clearInterval(id);
  }, [poll]);

  const elec = state?.electrical;
  const critV = elec?.critical_load_voltage_v ?? 0;
  const genKw = elec?.general_load_kw ?? 0;
  const critKw = elec?.critical_load_kw ?? 0;
  const totalKw = genKw + critKw;

  // Translate technical state into operational language
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;

  const protectionOk = bkrClosed && rclClosed;
  const voltageOk = critV >= 114 && critV <= 126;
  const customersServed = totalKw > 0;

  // Customer impact estimate (simplified: 1 kW ~ 3 residential customers)
  const estCustomers = Math.round(totalKw * 3);
  const estLost = customersServed ? 0 : Math.round(350 * 3); // baseline ~350 kW

  return (
    <div className="space-y-3">
      {/* Operational summary strip */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <OpCard
          label="Customer Service"
          value={customersServed ? `~${estCustomers} served` : `~${estLost} without power`}
          ok={customersServed}
          detail={customersServed ? `${totalKw} kW load` : "ALL LOADS DE-ENERGIZED"}
        />
        <OpCard
          label="Feeder Protection"
          value={protectionOk ? "Normal" : "DEGRADED"}
          ok={protectionOk}
          detail={
            !bkrClosed && !rclClosed ? "Breaker + recloser open"
            : !bkrClosed ? "Feeder breaker open"
            : !rclClosed ? "Recloser open"
            : "Breaker + recloser closed"
          }
        />
        <OpCard
          label="Voltage Quality"
          value={critV === 0 ? "Dead" : voltageOk ? `${critV.toFixed(0)}V Normal` : `${critV.toFixed(0)}V Out of Range`}
          ok={critV > 0 && voltageOk}
          detail={critV === 0 ? "No voltage at critical load" : `Regulator tap ${tap > 0 ? "+" : ""}${tap}`}
        />
        <OpCard
          label="Critical Load"
          value={elec?.critical_load_energized ? "Energized" : "NO POWER"}
          ok={elec?.critical_load_energized}
          detail={elec?.critical_load_energized ? `${critKw} kW hospital / fire station` : "Hospital and fire station offline"}
        />
      </div>

      {/* Voltage trend — single chart, focused */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/70 p-3">
        <div className="mb-1 flex items-center justify-between">
          <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
            Voltage Trend
          </span>
          <div className="flex items-center gap-3 text-[9px] text-slate-600">
            <span className="flex items-center gap-1">
              <span className="inline-block h-1.5 w-3 rounded-sm bg-sky-500" /> Feeder
            </span>
            <span className="flex items-center gap-1">
              <span className="inline-block h-1.5 w-3 rounded-sm bg-green-500" /> Critical Load
            </span>
            <span>Normal: 114–126V</span>
          </div>
        </div>
        <ResponsiveContainer width="100%" height={140}>
          <LineChart data={history}>
            <YAxis stroke="#334155" domain={[100, 135]} tick={{ fontSize: 9, fill: "#64748b" }} width={30} />
            <Tooltip contentStyle={{ background: "#020617", border: "1px solid #1e293b", fontSize: 11 }} />
            <ReferenceArea y1={114} y2={126} fill="#22c55e" fillOpacity={0.04} />
            <ReferenceArea y1={108} y2={114} fill="#f59e0b" fillOpacity={0.04} />
            <ReferenceArea y1={126} y2={132} fill="#f59e0b" fillOpacity={0.04} />
            <ReferenceLine y={120} stroke="#334155" strokeDasharray="3 3" />
            <Line dataKey="voltage" stroke="#0ea5e9" strokeWidth={1.5} dot={false} name="Feeder (V)" />
            <Line dataKey="critVoltage" stroke="#22c55e" strokeWidth={1.5} dot={false} name="Critical Load (V)" />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

function OpCard({ label, value, ok, detail }: { label: string; value: string; ok?: boolean; detail?: string }) {
  return (
    <div className={`rounded-lg border px-3 py-2 ${
      ok === false ? "border-red-800/60 bg-red-950/20" : ok === true ? "border-slate-800 bg-slate-900/70" : "border-slate-800 bg-slate-900/70"
    }`}>
      <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm font-bold ${
        ok === false ? "text-red-400" : ok === true ? "text-green-400" : "text-slate-400"
      }`}>
        {value}
      </div>
      {detail && <div className="text-[10px] text-slate-500 mt-0.5">{detail}</div>}
    </div>
  );
}
