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
  const [tab, setTab] = useState<"diagram" | "commands" | "correlation">("diagram");
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

  const execCmd = async (device: string, command: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, undefined, value);
      setCmdResult(`${device}/${command}: ${res.result} — ${res.process_impact || res.detail}`);
      setTimeout(poll, 500);
    } catch (e) {
      setCmdResult(`Error: ${e}`);
    }
  };

  const tabs = [
    { id: "diagram" as const, label: "One-Line Diagram" },
    { id: "commands" as const, label: "Supervisory Control" },
    { id: "correlation" as const, label: "Cyber → Process" },
  ];

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950">
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
        {tab === "diagram" && (
          <OneLine elec={elec} relay={relay} recloser={recloser} regulator={regulator} />
        )}
        {tab === "commands" && (
          <CommandPanel
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            execCmd={execCmd}
            cmdResult={cmdResult}
          />
        )}
        {tab === "correlation" && <CyberProcessCorrelation entries={audit} />}
      </div>
    </div>
  );
}

// ── One-Line Diagram ─────────────────────────────────────────────

function OneLine({
  elec,
  relay,
  recloser,
  regulator,
}: {
  elec?: SubstationState["electrical"];
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
}) {
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;
  const genEnergized = elec?.general_load_energized ?? false;
  const critEnergized = elec?.critical_load_energized ?? false;

  // Alarm conditions
  const lowVoltage = (elec?.critical_load_voltage_v ?? 120) < 114;
  const highVoltage = (elec?.critical_load_voltage_v ?? 120) > 126;
  const recloseOff = recloser && !recloser.reclose_enabled;
  const anyAlarm = !bkrClosed || !rclClosed || lowVoltage || highVoltage || recloseOff;

  return (
    <div className="space-y-3">
      {/* Alarm banner */}
      {anyAlarm && (
        <div className="rounded border border-red-800 bg-red-950/60 px-3 py-2">
          <div className="flex flex-wrap items-center gap-3 text-xs font-bold text-red-400">
            <span className="animate-pulse">ALARM</span>
            {!bkrClosed && <span className="rounded bg-red-900/70 px-2 py-0.5">BREAKER 52 OPEN</span>}
            {!rclClosed && <span className="rounded bg-red-900/70 px-2 py-0.5">RECLOSER 79 OPEN</span>}
            {recloseOff && <span className="rounded bg-yellow-900/70 px-2 py-0.5 text-yellow-400">AUTO-RECLOSE DISABLED</span>}
            {lowVoltage && <span className="rounded bg-red-900/70 px-2 py-0.5">LOW VOLTAGE</span>}
            {highVoltage && <span className="rounded bg-red-900/70 px-2 py-0.5">HIGH VOLTAGE</span>}
          </div>
        </div>
      )}

      <div className="font-mono text-xs leading-relaxed">
        {/* Zone header */}
        <div className="mb-2 rounded bg-slate-900 px-2 py-1 text-[10px] font-bold uppercase tracking-widest text-slate-500">
          Feeder 101 — Distribution Substation
        </div>

        {/* Substation Bus */}
        <div className="flex items-center gap-2">
          <span className="w-32 text-right text-slate-500">12.47 kV Bus</span>
          <span className="text-sky-400 font-bold">
            {elec?.substation_bus_voltage_v?.toFixed(1) ?? "--"}V
          </span>
          <span className="text-slate-600 text-[10px]">
            ({elec?.substation_bus_voltage_kv?.toFixed(2) ?? "--"} kV)
          </span>
        </div>

        {/* Vertical line */}
        <div className="ml-[8.5rem] border-l-2 border-sky-800 pl-3">

          {/* Device 52: Feeder Breaker */}
          <div className="py-1">
            <div className="flex items-center gap-2">
              <DeviceSymbol closed={bkrClosed} label="52" />
              <span className="text-slate-400">Feeder Breaker</span>
              {relay?.lockout && <Tag color="red">LOCKOUT</Tag>}
              {relay?.fault_seen && <Tag color="yellow">FAULT</Tag>}
              {relay?.remote_control_enabled && <Tag color="slate">REMOTE</Tag>}
            </div>
            <div className="ml-8 text-slate-600">
              Last cmd: <span className="text-slate-400">{relay?.last_command_source ?? "—"}</span>
            </div>
          </div>

          <div className={`border-l-2 pl-3 ${bkrClosed ? "border-green-800" : "border-red-900"}`}>
            {/* Current measurement */}
            <div className="py-0.5 text-slate-500">
              Feeder: <span className="text-amber-400">{elec?.feeder_current_a?.toFixed(1) ?? "0"}A</span>
              {" / "}
              <VoltageIndicator voltage={elec?.downstream_voltage_v} label="downstream" />
            </div>

            {/* Device 79: Recloser */}
            <div className="py-1">
              <div className="flex items-center gap-2">
                <DeviceSymbol closed={rclClosed} label="79" />
                <span className="text-slate-400">Recloser</span>
                {recloser?.lockout && <Tag color="red">LOCKOUT</Tag>}
                {recloser?.reclose_enabled
                  ? <Tag color="green">AUTO-RECLOSE</Tag>
                  : <Tag color="yellow">RECLOSE OFF</Tag>
                }
                <span className="text-slate-600">shots: {recloser?.shot_count ?? 0}/3</span>
              </div>
              <div className="ml-8 text-slate-600">
                Last cmd: <span className="text-slate-400">{recloser?.last_command_source ?? "—"}</span>
              </div>
            </div>

            <div className={`border-l-2 pl-3 ${rclClosed ? "border-green-800" : "border-red-900"}`}>
              {/* Branch A */}
              <div className="flex items-center gap-2 py-1">
                <span className="text-slate-500">Branch A:</span>
                <span className="text-slate-400">General Load</span>
                <LoadIndicator energized={genEnergized} kw={elec?.general_load_kw} />
              </div>

              {/* Branch B with regulator */}
              <div className="py-1">
                <div className="flex items-center gap-2">
                  <span className="text-slate-500">Branch B:</span>
                  <span className="text-slate-400">Critical Load</span>
                  <LoadIndicator energized={critEnergized} kw={elec?.critical_load_kw} />
                </div>

                {/* Device 90: Voltage Regulator */}
                <div className="ml-4 mt-1 flex items-center gap-2">
                  <span className="rounded border border-cyan-800 bg-cyan-950/30 px-1.5 py-0.5 text-cyan-400 font-bold">
                    90
                  </span>
                  <span className="text-slate-400">Voltage Regulator</span>
                  <span className="text-cyan-400 font-bold">
                    Tap: {tap > 0 ? "+" : ""}{tap}
                  </span>
                  <span className="text-slate-600">
                    ({regulator?.manual_mode ? "MANUAL" : "AUTO"})
                  </span>
                  <span className="text-slate-500">→</span>
                  <VoltageIndicator voltage={elec?.critical_load_voltage_v} label="critical" />
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Zone boundary legend */}
        <div className="mt-3 flex items-center gap-4 border-t border-slate-800 pt-2 text-[10px] text-slate-600">
          <span>Zone: OT Operations (10.30.30.0/24) + Field Devices (10.40.40.0/24)</span>
          <span>|</span>
          <span>Physics: OpenDSS engine (10.50.50.0/24)</span>
        </div>
      </div>
    </div>
  );
}

function DeviceSymbol({ closed, label }: { closed: boolean; label: string }) {
  return (
    <span
      className={`inline-flex h-6 w-8 items-center justify-center rounded border font-bold text-[10px] ${
        closed
          ? "border-green-700 bg-green-950/50 text-green-400"
          : "border-red-700 bg-red-950/50 text-red-400"
      }`}
    >
      {label}
    </span>
  );
}

function Tag({ color, children }: { color: string; children: React.ReactNode }) {
  const cls: Record<string, string> = {
    red: "border-red-800 bg-red-950/50 text-red-400",
    yellow: "border-yellow-800 bg-yellow-950/50 text-yellow-400",
    green: "border-green-800 bg-green-950/50 text-green-400",
    slate: "border-slate-700 bg-slate-900/50 text-slate-400",
  };
  return (
    <span className={`rounded border px-1 py-0.5 text-[9px] font-bold ${cls[color] || cls.slate}`}>
      {children}
    </span>
  );
}

function LoadIndicator({ energized, kw }: { energized?: boolean; kw?: number }) {
  return (
    <span className={`font-bold ${energized ? "text-green-400" : "text-red-400"}`}>
      {energized ? "ENERGIZED" : "DE-ENERGIZED"}
      <span className="ml-1 text-amber-400 font-normal">{kw ?? 0} kW</span>
    </span>
  );
}

function VoltageIndicator({ voltage, label }: { voltage?: number; label?: string }) {
  if (voltage === undefined || voltage === 0) return <span className="text-slate-600">--V</span>;
  const isLow = voltage < 108;
  const isWarnLow = voltage < 114;
  const isHigh = voltage > 132;
  const isWarnHigh = voltage > 126;
  const color = isLow || isHigh ? "text-red-400 font-bold" : isWarnLow || isWarnHigh ? "text-yellow-400" : "text-green-400";
  return (
    <span className={color}>
      {voltage.toFixed(1)}V
      {isLow && " BELOW RANGE B"}
      {!isLow && isWarnLow && " RANGE B"}
      {isHigh && " ABOVE RANGE B"}
      {!isHigh && isWarnHigh && " RANGE B"}
    </span>
  );
}

// ── Supervisory Control Panel ────────────────────────────────────

function CommandPanel({
  relay,
  recloser,
  regulator,
  execCmd,
  cmdResult,
}: {
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  execCmd: (device: string, command: string, value?: number) => void;
  cmdResult: string | null;
}) {
  return (
    <div className="space-y-4">
      {cmdResult && (
        <div className={`rounded border px-3 py-2 font-mono text-xs ${
          cmdResult.includes("executed") || cmdResult.includes("CLOSED") || cmdResult.includes("ENABLED")
            ? "border-green-800 bg-green-950/30 text-green-400"
            : cmdResult.includes("Error") || cmdResult.includes("rejected")
            ? "border-red-800 bg-red-950/30 text-red-400"
            : "border-slate-700 bg-slate-900 text-slate-300"
        }`}>
          {cmdResult}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <DeviceGroup title="Device 52 — Feeder Breaker" color="text-sky-400">
          <CmdButton label="TRIP" onClick={() => execCmd("relay", "trip")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("relay", "close")} variant="success" />
          <div className="w-full border-t border-slate-800 my-1" />
          <CmdButton label="Lockout" onClick={() => execCmd("relay", "lockout")} variant="warning" />
          <CmdButton label="Unlock" onClick={() => execCmd("relay", "unlock")} />
          <div className="w-full border-t border-slate-800 my-1" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("relay", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("relay", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Device 79 — Recloser" color="text-orange-400">
          <CmdButton label="OPEN" onClick={() => execCmd("recloser", "open")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("recloser", "close")} variant="success" />
          <div className="w-full border-t border-slate-800 my-1" />
          <CmdButton label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose")} variant="success" />
          <CmdButton label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose")} variant="warning" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout")} />
          <div className="w-full border-t border-slate-800 my-1" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("recloser", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("recloser", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Device 90 — Voltage Regulator" color="text-cyan-400">
          <CmdButton label="Raise Tap (+)" onClick={() => execCmd("regulator", "raise_tap")} />
          <CmdButton label="Lower Tap (−)" onClick={() => execCmd("regulator", "lower_tap")} />
          <div className="w-full border-t border-slate-800 my-1" />
          <CmdButton label="Set Manual" onClick={() => execCmd("regulator", "set_manual")} variant="warning" />
          <CmdButton label="Set Auto" onClick={() => execCmd("regulator", "set_auto")} variant="success" />
        </DeviceGroup>
      </div>
    </div>
  );
}

function DeviceGroup({ title, color, children }: { title: string; color: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <h4 className={`mb-2 text-xs font-bold uppercase tracking-wider ${color}`}>{title}</h4>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function CmdButton({ label, onClick, variant }: { label: string; onClick: () => void; variant?: "danger" | "success" | "warning" }) {
  const colors = {
    danger: "border-red-800 bg-red-950/50 text-red-400 hover:bg-red-900/50",
    success: "border-green-800 bg-green-950/50 text-green-400 hover:bg-green-900/50",
    warning: "border-yellow-800 bg-yellow-950/50 text-yellow-400 hover:bg-yellow-900/50",
  };
  const cls = variant ? colors[variant] : "border-slate-700 bg-slate-800/50 text-slate-300 hover:bg-slate-700/50";
  return (
    <button onClick={onClick} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}

// ── Cyber → Process Correlation View ─────────────────────────────

function CyberProcessCorrelation({ entries }: { entries: AuditEntry[] }) {
  if (entries.length === 0) {
    return <div className="py-8 text-center text-sm text-slate-500">No commands recorded yet. Run a scenario to see cyber-to-process event correlation.</div>;
  }

  const zoneColors: Record<string, string> = {
    enterprise: "text-red-400 bg-red-950/40 border-red-800",
    vendor: "text-purple-400 bg-purple-950/40 border-purple-800",
    ot_ops: "text-orange-400 bg-orange-950/40 border-orange-800",
    field: "text-cyan-400 bg-cyan-950/40 border-cyan-800",
    operator: "text-sky-400 bg-sky-950/40 border-sky-800",
    unknown: "text-slate-400 bg-slate-900/40 border-slate-700",
  };

  const resultColors: Record<string, string> = {
    executed: "text-green-400",
    rejected: "text-yellow-400",
    blocked: "text-red-400",
    error: "text-red-400",
  };

  return (
    <div className="space-y-4">
      <div className="text-xs text-slate-500">
        Shows each command with its source zone, firewall decision, and physical process consequence.
      </div>

      <div className="max-h-[500px] overflow-y-auto space-y-2">
        {entries.slice().reverse().map((e, i) => (
          <div key={i} className="rounded border border-slate-800 bg-slate-900/50 p-2 text-xs">
            {/* Header row: time, source zone, command */}
            <div className="flex items-center gap-2">
              <span className="text-slate-600 w-16 shrink-0">
                {e.timestamp ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "--"}
              </span>
              <span className={`rounded border px-1.5 py-0.5 text-[10px] font-bold ${zoneColors[e.source_zone || "unknown"] || zoneColors.unknown}`}>
                {(e.source_zone || "unknown").toUpperCase()}
              </span>
              <span className="text-slate-500">→</span>
              <span className="text-sky-400 font-bold">{e.target}</span>
              <span className="text-slate-500">/</span>
              <span className="text-amber-400">{e.command}</span>
              <span className="ml-auto">
                <span className={`font-bold ${resultColors[e.result] || "text-slate-400"}`}>
                  {e.result?.toUpperCase()}
                </span>
              </span>
            </div>

            {/* Detail */}
            {e.detail && (
              <div className="mt-1 ml-[4.5rem] text-slate-500">
                {e.detail}
              </div>
            )}

            {/* Process impact */}
            {e.process_impact && e.process_impact !== "command executed" && (
              <div className={`mt-1 ml-[4.5rem] rounded px-2 py-1 text-[11px] ${
                e.result === "executed"
                  ? e.process_impact.includes("de-energized") || e.process_impact.includes("DISABLED") || e.process_impact.includes("OPENED") || e.process_impact.includes("LOCKED")
                    ? "bg-red-950/40 text-red-300 border border-red-900/50"
                    : "bg-green-950/40 text-green-300 border border-green-900/50"
                  : "bg-slate-800/50 text-slate-400"
              }`}>
                Process: {e.process_impact}
              </div>
            )}

            {/* Source attribution */}
            <div className="mt-1 ml-[4.5rem] text-[10px] text-slate-600">
              src: {e.source}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
