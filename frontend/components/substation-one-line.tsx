"use client";

import type { SubstationState } from "../lib/api";

// ── One-Line Diagram ─────────────────────────────────────────────

export function OneLine({
  elec,
  relay,
  recloser,
  regulator,
  capbank,
}: {
  elec?: SubstationState["electrical"];
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  capbank?: Record<string, number | boolean | string>;
}) {
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;
  const genEnergized = elec?.general_load_energized ?? false;
  const critEnergized = elec?.critical_load_energized ?? false;
  const capIn = Boolean(capbank?.switched_in ?? elec?.capbank_switched_in);
  const capSwitchCount = Number(capbank?.switch_count ?? 0);

  const lowVoltage = (elec?.critical_load_voltage_v ?? 120) < 114;
  const highVoltage = (elec?.critical_load_voltage_v ?? 120) > 126;
  const recloseOff = recloser && !recloser.reclose_enabled;
  const anyAlarm = !bkrClosed || !rclClosed || lowVoltage || highVoltage || recloseOff;

  const totalKw = (elec?.general_load_kw ?? 0) + (elec?.critical_load_kw ?? 0);

  return (
    <div className="space-y-3">
      {/* Alarm banner - operational language */}
      {anyAlarm && (
        <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2 text-xs font-bold text-red-400">
            <span className="animate-pulse">ALARM</span>
            {!bkrClosed && <span className="rounded bg-red-900/60 px-2 py-0.5">FEEDER BREAKER OPEN - customers without power</span>}
            {bkrClosed && !rclClosed && <span className="rounded bg-red-900/60 px-2 py-0.5">RECLOSER OPEN - downstream loads lost</span>}
            {recloseOff && <span className="rounded bg-yellow-900/60 px-2 py-0.5 text-yellow-400">AUTO-RECLOSE DISABLED - no fault recovery</span>}
            {lowVoltage && <span className="rounded bg-red-900/60 px-2 py-0.5">LOW VOLTAGE - equipment damage risk</span>}
            {highVoltage && <span className="rounded bg-red-900/60 px-2 py-0.5">HIGH VOLTAGE - equipment damage risk</span>}
          </div>
        </div>
      )}

      <div className="font-mono text-xs leading-relaxed">
        {/* Substation bus */}
        <div className="flex items-center gap-3 mb-1">
          <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500 w-28 text-right">12.47 kV Bus</span>
          <span className="text-sky-400 font-bold text-sm">
            {elec?.substation_bus_voltage_v?.toFixed(0) ?? "--"}V
          </span>
        </div>

        {/* Main feeder path */}
        <div className="ml-[7.5rem] border-l-2 border-sky-800/60 pl-4 space-y-0.5">

          {/* Breaker 52 */}
          <div className="py-1">
            <div className="flex items-center gap-2">
              <BreakerSymbol closed={bkrClosed} label="52" />
              <span className="text-slate-300 font-medium">Feeder Breaker</span>
              {relay?.lockout && <StatusBadge color="red">LOCKED OUT</StatusBadge>}
              {relay?.fault_seen && <StatusBadge color="yellow">FAULT DETECTED</StatusBadge>}
            </div>
            {relay?.last_command_source && relay.last_command_source !== "-" && (
              <div className="ml-10 text-[10px] text-slate-600">
                Last command from: <span className="text-slate-400">{String(relay.last_command_source)}</span>
              </div>
            )}
          </div>

          {/* Energized/de-energized section */}
          <div className={`border-l-2 pl-4 space-y-0.5 ${bkrClosed ? "border-green-800/50" : "border-red-900/50"}`}>
            <div className="flex items-center gap-2 text-slate-500 text-[10px]">
              <span className="text-amber-400 font-bold">{elec?.feeder_current_a?.toFixed(0) ?? "0"}A</span>
              <span>/</span>
              <VoltageChip voltage={elec?.downstream_voltage_v} />
            </div>

            {/* Recloser 79 */}
            <div className="py-1">
              <div className="flex items-center gap-2">
                <BreakerSymbol closed={rclClosed} label="79" />
                <span className="text-slate-300 font-medium">Recloser</span>
                {recloser?.lockout && <StatusBadge color="red">LOCKED OUT</StatusBadge>}
                {recloser?.reclose_enabled
                  ? <StatusBadge color="green">Auto-reclose ON</StatusBadge>
                  : <StatusBadge color="yellow">Auto-reclose OFF</StatusBadge>
                }
                <span className="text-slate-600 text-[10px]">shots {String(recloser?.shot_count ?? 0)}/3</span>
              </div>
            </div>

            <div className={`border-l-2 pl-4 ${rclClosed ? "border-green-800/50" : "border-red-900/50"}`}>
              {/* General load */}
              <div className="flex items-center gap-2 py-1">
                <LoadSymbol energized={genEnergized} />
                <span className="text-slate-400">General Load</span>
                <span className={`font-bold text-xs ${genEnergized ? "text-green-400" : "text-red-400"}`}>
                  {genEnergized ? `${Math.round(elec?.general_load_kw ?? 0)} kW` : "NO POWER"}
                </span>
              </div>

              {/* Capacitor Bank - shunt at the load bus (reactive support) */}
              <div className="flex items-center gap-2 py-1 text-[11px]">
                <span className="rounded border border-purple-800/60 bg-purple-950/20 px-1.5 py-0.5 font-bold text-[10px] text-purple-400">
                  CAP
                </span>
                <span className="text-slate-400">Capacitor Bank</span>
                <span className={`font-bold ${capIn ? "text-green-400" : "text-slate-500"}`}>
                  {capIn ? "SWITCHED IN" : "OUT"}
                </span>
                {capIn && <span className="text-purple-400/80">+{Number(capbank?.kvar_rating ?? 300)} kVAR</span>}
                <span className="text-slate-600">{capbank?.auto_mode ? "AUTO" : "MANUAL"}</span>
                <span className={`text-[10px] ${capSwitchCount >= 5 ? "text-yellow-400" : "text-slate-600"}`}>ops {capSwitchCount}/6</span>
                {capbank?.lockout ? <StatusBadge color="red">LOCKED OUT</StatusBadge> : null}
              </div>

              {/* Voltage Regulator 90 - series element feeding the critical load
                  (Transformer.VReg: load_bus -> reg_bus in substation_feeder.dss) */}
              <div className="flex items-center gap-2 py-1 text-[11px]">
                <span className="rounded border border-cyan-800/60 bg-cyan-950/20 px-1.5 py-0.5 text-cyan-400 font-bold text-[10px]">
                  90
                </span>
                <span className="text-slate-500">Voltage Regulator</span>
                <span className="text-cyan-400 font-bold">
                  Tap {tap > 0 ? "+" : ""}{tap}
                </span>
                <span className="text-slate-600">
                  {regulator?.manual_mode ? "MANUAL" : "AUTO"}
                </span>
              </div>

              {/* Critical load - downstream of (post-) the regulator at reg_bus */}
              <div className={`border-l-2 pl-4 ${critEnergized ? "border-green-800/50" : "border-red-900/50"}`}>
                <div className="flex items-center gap-2 py-1">
                  <LoadSymbol energized={critEnergized} critical />
                  <span className="text-slate-300 font-medium">Critical Load</span>
                  <span className={`font-bold text-xs ${critEnergized ? "text-green-400" : "text-red-400"}`}>
                    {critEnergized ? `${Math.round(elec?.critical_load_kw ?? 0)} kW` : "NO POWER"}
                  </span>
                  <VoltageChip voltage={elec?.critical_load_voltage_v} critical />
                </div>
                {!critEnergized && (
                  <div className="ml-6 mt-0.5 text-[10px] text-red-400 font-medium">
                    Hospital and fire station without power
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Service summary */}
        <div className="mt-3 flex items-center gap-4 border-t border-slate-800/60 pt-2 text-[10px]">
          <span className={`font-bold ${totalKw > 0 ? "text-green-400" : "text-red-400"}`}>
            {totalKw > 0 ? `${Math.round(totalKw)} kW serving ~${Math.round(totalKw * 3)} customers` : "ALL CUSTOMERS WITHOUT POWER"}
          </span>
          <span className="ml-auto text-slate-600">
            RTAC polls field devices on 10.40.40.x via containd
          </span>
        </div>
      </div>
    </div>
  );
}

function BreakerSymbol({ closed, label }: { closed: boolean; label: string }) {
  return (
    <span className={`inline-flex h-6 w-8 items-center justify-center rounded font-bold text-[10px] ${
      closed
        ? "border border-green-700/60 bg-green-950/40 text-green-400"
        : "border-2 border-red-600 bg-red-950/60 text-red-400"
    }`}>
      {label}
    </span>
  );
}

function LoadSymbol({ energized, critical }: { energized?: boolean; critical?: boolean }) {
  const baseColor = energized ? "border-green-700/50" : "border-red-700/50";
  return (
    <span className={`inline-flex h-5 w-5 items-center justify-center rounded-full border text-[8px] font-bold ${baseColor} ${
      critical ? "text-amber-400" : "text-slate-500"
    }`}>
      {critical ? "!" : "~"}
    </span>
  );
}

function StatusBadge({ color, children }: { color: string; children: React.ReactNode }) {
  const cls: Record<string, string> = {
    red: "border-red-800/60 bg-red-950/40 text-red-400",
    yellow: "border-yellow-800/60 bg-yellow-950/40 text-yellow-400",
    green: "border-green-800/60 bg-green-950/40 text-green-400",
  };
  return (
    <span className={`rounded border px-1.5 py-0.5 text-[9px] font-bold ${cls[color] || cls.red}`}>
      {children}
    </span>
  );
}

function VoltageChip({ voltage, critical }: { voltage?: number; critical?: boolean }) {
  if (voltage === undefined || voltage === 0) return <span className="text-red-400 font-bold text-[10px]">0V DEAD</span>;
  const low = voltage < 108;
  const warnLow = voltage < 114;
  const high = voltage > 132;
  const warnHigh = voltage > 126;
  const color = low || high ? "text-red-400 font-bold" : warnLow || warnHigh ? "text-yellow-400" : "text-green-400";
  return (
    <span className={`${color} text-[10px]`}>
      {voltage.toFixed(0)}V
      {(low || high) && " DANGER"}
      {(!low && warnLow) && " LOW"}
      {(!high && warnHigh) && " HIGH"}
    </span>
  );
}
