"use client";

import { useEffect, useState, useCallback } from "react";
import {
  getSubstationState,
  getSubstationAudit,
  sendSubstationCommand,
  type SubstationState,
  type AuditEntry,
} from "../lib/api";

export function SubstationPanel() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [tab, setTab] = useState<"diagram" | "commands" | "audit">("diagram");
  const [cmdResult, setCmdResult] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const [s, a] = await Promise.all([getSubstationState(), getSubstationAudit()]);
      setState(s);
      setAudit(a.entries ?? []);
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
  const relay = state?.devices?.relay;
  const recloser = state?.devices?.recloser;
  const regulator = state?.devices?.regulator;

  const execCmd = async (device: string, command: string) => {
    try {
      const res = await sendSubstationCommand(device, command);
      setCmdResult(`${device}/${command}: ${res.result} - ${res.detail}`);
      poll();
    } catch (e) {
      setCmdResult(`Error: ${e}`);
    }
  };

  const tabs = [
    { id: "diagram" as const, label: "One-Line Diagram" },
    { id: "commands" as const, label: "Device Commands" },
    { id: "audit" as const, label: "Audit Log" },
  ];

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950">
      {/* Tab bar */}
      <div className="flex border-b border-slate-800">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`px-4 py-2 text-xs font-medium transition-colors ${
              tab === t.id
                ? "border-b-2 border-sky-500 text-sky-400"
                : "text-slate-500 hover:text-slate-300"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="p-4">
        {tab === "diagram" && <OneLine elec={elec} relay={relay} recloser={recloser} regulator={regulator} />}
        {tab === "commands" && (
          <CommandPanel
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            execCmd={execCmd}
            cmdResult={cmdResult}
          />
        )}
        {tab === "audit" && <AuditLog entries={audit} />}
      </div>
    </div>
  );
}

// One-line feeder diagram (text-based, terminal aesthetic)
function OneLine({
  elec,
  relay,
  recloser,
  regulator,
}: {
  elec?: SubstationState["electrical"];
  relay?: Record<string, number | boolean>;
  recloser?: Record<string, number | boolean>;
  regulator?: Record<string, number | boolean>;
}) {
  const breakerClosed = elec?.breaker_closed ?? false;
  const recloserClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;

  return (
    <div className="font-mono text-xs leading-relaxed">
      {/* Substation bus */}
      <div className="flex items-center gap-2">
        <span className="w-28 text-right text-slate-500">Source (Infinite Bus)</span>
        <span className="text-sky-400">
          {elec?.substation_bus_voltage_kv?.toFixed(2) ?? "--"} kV / {elec?.substation_bus_voltage_v?.toFixed(1) ?? "--"}V
        </span>
      </div>

      <div className="ml-32 border-l border-slate-600 pl-2">
        {/* Breaker */}
        <div className="flex items-center gap-2 py-1">
          <span className={`font-bold ${breakerClosed ? "text-green-400" : "text-red-400"}`}>
            [{breakerClosed ? "X" : " "}] Feeder Breaker
          </span>
          {relay?.lockout && <span className="rounded bg-red-900/50 px-1 text-red-400">LOCKOUT</span>}
          {relay?.fault_seen && <span className="rounded bg-yellow-900/50 px-1 text-yellow-400">FAULT</span>}
        </div>

        <div className="border-l border-slate-600 pl-4">
          <div className="py-0.5 text-slate-500">
            |-- Current: <span className="text-amber-400">{elec?.feeder_current_a?.toFixed(1) ?? "0"}A</span>
          </div>

          {/* Recloser */}
          <div className="flex items-center gap-2 py-1">
            <span className={`font-bold ${recloserClosed ? "text-green-400" : "text-red-400"}`}>
              [{recloserClosed ? "X" : " "}] Mid-Feeder Recloser
            </span>
            {recloser?.lockout && <span className="rounded bg-red-900/50 px-1 text-red-400">LOCKOUT</span>}
            {!recloser?.reclose_enabled && (
              <span className="rounded bg-yellow-900/50 px-1 text-yellow-400">RECLOSE OFF</span>
            )}
            <span className="text-slate-600">shots: {recloser?.shot_count ?? 0}/3</span>
          </div>

          <div className="border-l border-slate-600 pl-4">
            {/* Branch A: General Load */}
            <div className="py-0.5">
              <span className="text-slate-500">|-- Branch A: General Load</span>{" "}
              <span className={elec?.general_load_energized ? "text-green-400" : "text-red-400"}>
                {elec?.general_load_energized ? "ENERGIZED" : "DE-ENERGIZED"}
              </span>{" "}
              <span className="text-amber-400">{elec?.general_load_kw ?? 0} kW</span>
            </div>

            {/* Branch B: Critical Load + Regulator */}
            <div className="py-0.5">
              <span className="text-slate-500">|-- Branch B: Critical Load</span>{" "}
              <span className={elec?.critical_load_energized ? "text-green-400" : "text-red-400"}>
                {elec?.critical_load_energized ? "ENERGIZED" : "DE-ENERGIZED"}
              </span>{" "}
              <span className="text-amber-400">{elec?.critical_load_kw ?? 0} kW</span>
            </div>

            {/* Regulator */}
            <div className="flex items-center gap-2 py-0.5 pl-4">
              <span className="text-cyan-400">
                [REG] Tap: {tap > 0 ? "+" : ""}{tap}
              </span>
              <span className="text-slate-500">
                ({regulator?.manual_mode ? "MANUAL" : "AUTO"})
              </span>
              <span className="text-slate-500">→</span>
              <VoltageIndicator voltage={elec?.critical_load_voltage_v} />
            </div>

            {/* Downstream voltage */}
            <div className="py-0.5 pl-4 text-slate-500">
              Downstream bus: <VoltageIndicator voltage={elec?.downstream_voltage_v} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function VoltageIndicator({ voltage }: { voltage?: number }) {
  if (voltage === undefined) return <span className="text-slate-600">--V</span>;
  const isLow = voltage < 108;
  const isHigh = voltage > 132;
  const color = isLow || isHigh ? "text-red-400" : voltage < 114 ? "text-yellow-400" : "text-green-400";
  return (
    <span className={color}>
      {voltage.toFixed(1)}V
      {isLow && " LOW"}
      {isHigh && " HIGH"}
    </span>
  );
}

// Command panel for sending commands to field devices
function CommandPanel({
  relay,
  recloser,
  regulator,
  execCmd,
  cmdResult,
}: {
  relay?: Record<string, number | boolean>;
  recloser?: Record<string, number | boolean>;
  regulator?: Record<string, number | boolean>;
  execCmd: (device: string, command: string) => void;
  cmdResult: string | null;
}) {
  return (
    <div className="space-y-4">
      {cmdResult && (
        <div className="rounded border border-slate-700 bg-slate-900 px-3 py-1.5 font-mono text-xs text-slate-300">
          {cmdResult}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        {/* Relay controls */}
        <DeviceGroup title="Feeder Breaker (Relay)" color="text-sky-400">
          <CmdButton label="Trip" onClick={() => execCmd("relay", "trip")} variant="danger" />
          <CmdButton label="Close" onClick={() => execCmd("relay", "close")} variant="success" />
          <CmdButton label="Lockout" onClick={() => execCmd("relay", "lockout")} variant="warning" />
          <CmdButton label="Unlock" onClick={() => execCmd("relay", "unlock")} />
          <CmdButton label="Inject Fault" onClick={() => execCmd("relay", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("relay", "clear_fault")} />
        </DeviceGroup>

        {/* Recloser controls */}
        <DeviceGroup title="Recloser" color="text-orange-400">
          <CmdButton label="Open" onClick={() => execCmd("recloser", "open")} variant="danger" />
          <CmdButton label="Close" onClick={() => execCmd("recloser", "close")} variant="success" />
          <CmdButton label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose")} variant="success" />
          <CmdButton label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose")} variant="warning" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout")} />
          <CmdButton label="Inject Fault" onClick={() => execCmd("recloser", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("recloser", "clear_fault")} />
        </DeviceGroup>

        {/* Regulator controls */}
        <DeviceGroup title="Voltage Regulator" color="text-cyan-400">
          <CmdButton label="Raise Tap" onClick={() => execCmd("regulator", "raise_tap")} />
          <CmdButton label="Lower Tap" onClick={() => execCmd("regulator", "lower_tap")} />
          <CmdButton label="Set Manual" onClick={() => execCmd("regulator", "set_manual")} variant="warning" />
          <CmdButton label="Set Auto" onClick={() => execCmd("regulator", "set_auto")} variant="success" />
        </DeviceGroup>
      </div>
    </div>
  );
}

function DeviceGroup({
  title,
  color,
  children,
}: {
  title: string;
  color: string;
  children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <h4 className={`mb-2 text-xs font-bold uppercase tracking-wider ${color}`}>{title}</h4>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function CmdButton({
  label,
  onClick,
  variant,
}: {
  label: string;
  onClick: () => void;
  variant?: "danger" | "success" | "warning";
}) {
  const colors = {
    danger: "border-red-800 bg-red-950/50 text-red-400 hover:bg-red-900/50",
    success: "border-green-800 bg-green-950/50 text-green-400 hover:bg-green-900/50",
    warning: "border-yellow-800 bg-yellow-950/50 text-yellow-400 hover:bg-yellow-900/50",
  };
  const cls = variant
    ? colors[variant]
    : "border-slate-700 bg-slate-800/50 text-slate-300 hover:bg-slate-700/50";

  return (
    <button onClick={onClick} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}

// Audit log viewer
function AuditLog({ entries }: { entries: AuditEntry[] }) {
  if (entries.length === 0) {
    return <div className="text-center text-sm text-slate-500">No audit entries yet</div>;
  }

  return (
    <div className="max-h-64 overflow-y-auto font-mono text-xs">
      <table className="w-full">
        <thead>
          <tr className="border-b border-slate-800 text-left text-slate-500">
            <th className="px-2 py-1">Time</th>
            <th className="px-2 py-1">Source</th>
            <th className="px-2 py-1">Target</th>
            <th className="px-2 py-1">Command</th>
            <th className="px-2 py-1">Result</th>
            <th className="px-2 py-1">Detail</th>
          </tr>
        </thead>
        <tbody>
          {entries.slice().reverse().map((e, i) => (
            <tr key={i} className="border-b border-slate-800/50 hover:bg-slate-900">
              <td className="px-2 py-1 text-slate-500">
                {e.timestamp ? new Date(e.timestamp).toLocaleTimeString() : "--"}
              </td>
              <td className="px-2 py-1 text-purple-400">{e.source}</td>
              <td className="px-2 py-1 text-sky-400">{e.target}</td>
              <td className="px-2 py-1 text-amber-400">{e.command}</td>
              <td
                className={`px-2 py-1 ${
                  e.result === "ok" || e.result === "success"
                    ? "text-green-400"
                    : e.result === "error" || e.result === "rejected"
                    ? "text-red-400"
                    : "text-slate-400"
                }`}
              >
                {e.result}
              </td>
              <td className="px-2 py-1 text-slate-500">{e.detail}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
