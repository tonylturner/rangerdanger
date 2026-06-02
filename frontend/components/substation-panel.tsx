"use client";

import { useEffect, useState, useCallback, useRef } from "react";
import {
  getSubstationState,
  getSubstationAudit,
  getSubstationNetworkEvents,
  sendSubstationCommand,
  type SubstationState,
  type AuditEntry,
  type NetworkEvent,
} from "../lib/api";

export function SubstationPanel() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [networkEvents, setNetworkEvents] = useState<NetworkEvent[]>([]);
  const [tab, setTab] = useState<"diagram" | "commands" | "correlation" | "electrical">("diagram");
  const [cmdResult, setCmdResult] = useState<string | null>(null);

  const poll = useCallback(async () => {
    try {
      const [s, a, ne] = await Promise.all([
        getSubstationState(),
        getSubstationAudit(),
        getSubstationNetworkEvents(),
      ]);
      setState(s);
      setAudit(a.entries ?? []);
      setNetworkEvents(ne.events ?? []);
    } catch {
      // offline
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, [poll]);

  const elec = state?.electrical;
  const relay = state?.devices?.relay;
  const recloser = state?.devices?.recloser;
  const regulator = state?.devices?.regulator;
  const capbank = state?.devices?.capbank;

  const execCmd = async (device: string, command: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, undefined, value);
      setCmdResult(`${res.result}: ${res.process_impact || res.detail}`);
      setTimeout(poll, 500);
    } catch (e) {
      setCmdResult(`Error: ${e}`);
    }
  };

  const tabs = [
    { id: "diagram" as const, label: "Feeder One-Line" },
    { id: "commands" as const, label: "Supervisory Control" },
    { id: "correlation" as const, label: "Command Audit" },
    { id: "electrical" as const, label: "Electrical Detail" },
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
          <OneLine elec={elec} relay={relay} recloser={recloser} regulator={regulator} capbank={capbank} />
        )}
        {tab === "commands" && (
          <CommandPanel
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            capbank={capbank}
            execCmd={execCmd}
            cmdResult={cmdResult}
          />
        )}
        {tab === "correlation" && <CommandAuditView entries={audit} networkEvents={networkEvents} />}
        {tab === "electrical" && (
          <ElectricalDetailView
            elec={elec}
            relay={relay}
            recloser={recloser}
            regulator={regulator}
            capbank={capbank}
            audit={audit}
          />
        )}
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

// ── Supervisory Control Panel ────────────────────────────────────

function CommandPanel({
  relay,
  recloser,
  regulator,
  capbank,
  execCmd,
  cmdResult,
}: {
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  capbank?: Record<string, number | boolean | string>;
  execCmd: (device: string, command: string, value?: number) => void;
  cmdResult: string | null;
}) {
  // In AUTO the regulator holds its setpoint and the cap bank self-corrects, so
  // a direct manual command is ambiguous: the cap reverts a switch within a
  // cycle, and a small regulator tap inside the deadband can actually persist.
  // Rather than expose a button whose effect is inconsistent, disable the
  // manual actuators and point the operator at the mode toggle.
  const regManual = Boolean(regulator?.manual_mode);
  const capAuto = Boolean(capbank?.auto_mode);
  return (
    <div className="space-y-4">
      {/* SCADA → RTAC → field-device hierarchy - the RTAC is the controller;
          the four cards below are its peer feeder devices. */}
      <div className="flex flex-wrap items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/30 px-3 py-2 text-[10px]">
        <span className="font-bold text-sky-400">SCADA / HMI</span>
        <span className="text-slate-600">→</span>
        <span className="font-bold text-orange-400">RTAC</span>
        <span className="text-slate-600">→</span>
        <span className="font-bold text-green-400">Field Devices</span>
        <span className="text-slate-600">· 10.40.40.0/24</span>
        <span className="ml-auto text-slate-600">RTAC relays operator commands to the peer feeder devices below</span>
      </div>

      {cmdResult && (
        <div className={`rounded border px-3 py-2 text-xs ${
          cmdResult.includes("executed") || cmdResult.includes("CLOSED") || cmdResult.includes("ENABLED")
            ? "border-green-800/60 bg-green-950/20 text-green-400"
            : cmdResult.includes("Error") || cmdResult.includes("rejected")
            ? "border-red-800/60 bg-red-950/20 text-red-400"
            : "border-slate-700 bg-slate-900 text-slate-300"
        }`}>
          {cmdResult}
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2">
        <DeviceGroup title="Feeder Breaker (52)" subtitle="10.40.40.20" role="Service interruption - energize / de-energize feeder">
          <CmdButton label="TRIP" onClick={() => execCmd("relay", "trip")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("relay", "close")} variant="success" />
          <CmdButton label="Lockout" onClick={() => execCmd("relay", "lockout")} variant="warning" />
          <CmdButton label="Unlock" onClick={() => execCmd("relay", "unlock")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("relay", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("relay", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Recloser (79)" subtitle="10.40.40.21" role="Reliability - automatic fault recovery">
          <CmdButton label="OPEN" onClick={() => execCmd("recloser", "open")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("recloser", "close")} variant="success" />
          <CmdButton label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose")} variant="success" />
          <CmdButton label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose")} variant="warning" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("recloser", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("recloser", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Capacitor Bank (CAP)" subtitle="10.40.40.23" role="Power factor - reactive support">
          <CmdButton label="Switch In" onClick={() => execCmd("capbank", "switch_in")} variant="success" disabled={capAuto} title={capAuto ? "Cap bank in AUTO - switch to Manual to control" : undefined} />
          <CmdButton label="Switch Out" onClick={() => execCmd("capbank", "switch_out")} variant="danger" disabled={capAuto} title={capAuto ? "Cap bank in AUTO - switch to Manual to control" : undefined} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Manual Mode" onClick={() => execCmd("capbank", "set_manual")} variant="warning" />
          <CmdButton label="Auto Mode" onClick={() => execCmd("capbank", "set_auto")} variant="success" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("capbank", "reset_lockout")} />
        </DeviceGroup>

        <DeviceGroup title="Voltage Regulator (90)" subtitle="10.40.40.22" role="Voltage control - tap regulation">
          <CmdButton label="Raise Tap" onClick={() => execCmd("regulator", "raise_tap")} disabled={!regManual} title={!regManual ? "Regulator in AUTO - switch to Manual to adjust taps" : undefined} />
          <CmdButton label="Lower Tap" onClick={() => execCmd("regulator", "lower_tap")} disabled={!regManual} title={!regManual ? "Regulator in AUTO - switch to Manual to adjust taps" : undefined} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Manual Mode" onClick={() => execCmd("regulator", "set_manual")} variant="warning" />
          <CmdButton label="Auto Mode" onClick={() => execCmd("regulator", "set_auto")} variant="success" />
        </DeviceGroup>
      </div>

      <div className="text-[10px] text-slate-600">
        The containd firewall controls which zones can reach these devices on 10.40.40.0/24 -
        an attacker who reaches that segment can drive any of them.
      </div>
    </div>
  );
}

function DeviceGroup({ title, subtitle, role, children }: { title: string; subtitle: string; role: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <h4 className="text-xs font-bold text-slate-300">{title}</h4>
      <div className="text-[9px] text-slate-600">{subtitle}</div>
      <div className="mb-2 text-[9px] italic text-slate-500">{role}</div>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function CmdButton({ label, onClick, variant, disabled, title }: { label: string; onClick: () => void; variant?: "danger" | "success" | "warning"; disabled?: boolean; title?: string }) {
  const colors = {
    danger: "border-red-800/60 bg-red-950/40 text-red-400 hover:bg-red-900/40",
    success: "border-green-800/60 bg-green-950/40 text-green-400 hover:bg-green-900/40",
    warning: "border-yellow-800/60 bg-yellow-950/40 text-yellow-400 hover:bg-yellow-900/40",
  };
  const cls = disabled
    ? "border-slate-800 bg-slate-900/30 text-slate-600 cursor-not-allowed"
    : variant ? colors[variant] : "border-slate-700 bg-slate-800/40 text-slate-300 hover:bg-slate-700/40";
  return (
    <button onClick={onClick} disabled={disabled} title={title} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}

function CommandAuditView({ entries, networkEvents }: { entries: AuditEntry[]; networkEvents: NetworkEvent[] }) {
  const [showDPI, setShowDPI] = useState(networkEvents.length > 0);
  const dpiSeenRef = useRef(false);
  useEffect(() => {
    if (networkEvents.length > 0 && !dpiSeenRef.current) {
      dpiSeenRef.current = true;
      setShowDPI(true);
    }
  }, [networkEvents.length]);

  if (entries.length === 0 && networkEvents.length === 0) {
    return <div className="py-8 text-center text-sm text-slate-500">No commands recorded yet. Run an exercise to see the audit trail.</div>;
  }

  // Zone labels that make sense for substation context
  const zoneLabel: Record<string, string> = {
    enterprise: "Enterprise",
    vendor: "Vendor",
    ot_ops: "OT Ops",
    field: "Field",
    operator: "Operator",
    "lab-control": "Lab Control",
    auto: "Auto",
  };

  const zoneBorder: Record<string, string> = {
    enterprise: "border-l-red-500",
    vendor: "border-l-purple-500",
    ot_ops: "border-l-orange-500",
    field: "border-l-green-500",
    operator: "border-l-sky-500",
    "lab-control": "border-l-[#c6f24e]",
    auto: "border-l-amber-500",
  };

  return (
    <div className="space-y-3">
      {/* Controls */}
      <div className="flex items-center justify-between">
        <span className="text-[10px] text-slate-500">
          Who sent commands, from which zone, and what happened to the feeder.
        </span>
        {networkEvents.length > 0 && (
          <button
            onClick={() => setShowDPI(!showDPI)}
            className={`rounded border px-2 py-1 text-[10px] font-medium ${
              showDPI ? "border-purple-700 bg-purple-950/30 text-purple-400" : "border-slate-700 bg-slate-800/40 text-slate-500"
            }`}
          >
            {showDPI ? "Hide" : "Show"} DPI ({networkEvents.length})
          </button>
        )}
      </div>

      <div className="max-h-[500px] overflow-y-auto space-y-1">
        {entries.map((e, i) => {
              const zone = e.source_zone || "unknown";
              const wasAttack = zone === "enterprise" || zone === "vendor";
              const succeeded = e.result === "executed";
              const harmful = e.process_impact?.includes("de-energized") || e.process_impact?.includes("DISABLED") || e.process_impact?.includes("OPENED") || e.process_impact?.includes("LOCKED");

              return (
                <div key={`a-${e.timestamp}-${e.command}-${i}`} className={`rounded border-l-2 border border-slate-800/60 bg-slate-900/40 p-2 text-xs ${zoneBorder[zone] || "border-l-slate-500"}`}>
                  <div className="flex items-center gap-2">
                    <span className="text-slate-600 w-14 shrink-0 text-[10px]">
                      {e.timestamp ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "--"}
                    </span>
                    <span className="text-[10px] font-bold text-slate-400">
                      {zoneLabel[zone] || zone}
                    </span>
                    <span className="text-slate-600">sent</span>
                    <span className="text-amber-400 font-medium">{e.command}</span>
                    <span className="text-slate-600">to</span>
                    <span className="text-slate-300 font-medium">{e.target}</span>
                    <span className="ml-auto">
                      {succeeded ? (
                        harmful ? (
                          <span className="text-red-400 font-bold">SUCCEEDED</span>
                        ) : (
                          <span className="text-green-400 font-bold">OK</span>
                        )
                      ) : (
                        <span className="text-yellow-400 font-bold">BLOCKED</span>
                      )}
                    </span>
                  </div>
                  {/* Operational consequence - always prominent */}
                  {e.process_impact && e.process_impact !== "command executed" && (
                    <div className={`mt-1 ml-14 rounded px-2 py-1 text-[11px] font-medium ${
                      succeeded && harmful
                        ? "bg-red-950/30 text-red-300 border border-red-900/40"
                        : succeeded
                        ? "bg-green-950/20 text-green-300 border border-green-900/30"
                        : "bg-slate-800/40 text-slate-400"
                    }`}>
                      {wasAttack && succeeded && harmful ? "Attack impact: " : ""}
                      {e.process_impact}
                    </div>
                  )}
                </div>
              );
            })}

        {showDPI && networkEvents.map((e, i) => (
          <div key={`n-${e.id ?? i}`} className="rounded border border-purple-900/30 bg-purple-950/10 p-2 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-slate-600 w-14 shrink-0 text-[10px]">
                {e.timestamp ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "--"}
              </span>
              <span className="text-[10px] font-bold text-purple-400">DPI</span>
              <span className="text-slate-500">{e.source} → {e.dest}</span>
              {e.protocol && e.protocol !== "-" && (
                <span className="text-purple-400 text-[10px]">[{e.protocol}]</span>
              )}
            </div>
            {e.details && <div className="mt-0.5 ml-14 text-slate-500 text-[10px]">{e.details}</div>}
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Electrical Detail View ─────────────────────────────────────────

function ElectricalDetailView({
  elec,
  relay,
  recloser,
  regulator,
  capbank,
  audit,
}: {
  elec?: SubstationState["electrical"];
  relay?: Record<string, number | boolean | string>;
  recloser?: Record<string, number | boolean | string>;
  regulator?: Record<string, number | boolean | string>;
  capbank?: Record<string, number | boolean | string>;
  audit: AuditEntry[];
}) {
  const nomKV = 12.47;
  const nomV = 120.0;
  const feederRatingA = 600; // typical 12.47 kV feeder rating

  // Per-unit voltages
  const subPU = elec?.substation_bus_voltage_v ? elec.substation_bus_voltage_v / nomV : 0;
  const downPU = elec?.downstream_voltage_v ? elec.downstream_voltage_v / nomV : 0;
  const critPU = elec?.critical_load_voltage_v ? elec.critical_load_voltage_v / nomV : 0;

  // Loading
  const currentA = elec?.feeder_current_a ?? 0;
  const loadingPct = feederRatingA > 0 ? (currentA / feederRatingA) * 100 : 0;

  // Power flow
  const sourceKW = elec?.source_power_kw ?? 0;
  const genKW = elec?.general_load_kw ?? 0;
  const critKW = elec?.critical_load_kw ?? 0;
  const lossesKW = elec?.total_losses_kw ?? 0;
  const pf = elec?.power_factor ?? 0;

  // Device states
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const tap = elec?.regulator_tap ?? 0;
  const shotCount = Number(recloser?.shot_count ?? 0);
  const recloseEnabled = recloser?.reclose_enabled ?? true;
  const rclLockout = recloser?.lockout ?? false;
  const relayLockout = relay?.lockout ?? false;
  const relayFault = relay?.fault_seen ?? false;
  const rclFault = recloser?.fault_seen ?? false;
  const regManual = regulator?.manual_mode ?? false;
  const regSetpoint = Number(regulator?.voltage_setpoint_v ?? 120);
  const capIn = Boolean(capbank?.switched_in ?? elec?.capbank_switched_in);
  const capAuto = Boolean(capbank?.auto_mode);
  const capLockout = Boolean(capbank?.lockout);
  const capKvar = Number(capbank?.kvar_rating ?? 300);
  const capSwitchCount = Number(capbank?.switch_count ?? 0);
  const faultCurrentA = elec?.fault_current_a ?? 0;
  const faultActive = faultCurrentA > 0 || !!relayFault || !!rclFault;

  // Operational state
  const allEnergized = bkrClosed && rclClosed;
  const protectionDegraded = !recloseEnabled || rclLockout || relayLockout;

  // Determine operational impact
  const impactLevel: "normal" | "degraded" | "outage" =
    !bkrClosed ? "outage"
    : !rclClosed ? "outage"
    : faultActive || protectionDegraded || critPU < 0.95 || critPU > 1.05 ? "degraded"
    : "normal";

  // Last trip cause from audit
  const lastTrip = audit.filter(
    (e) => e.command === "trip" || e.command === "open" || e.command === "inject_fault"
  ).slice(-1)[0];

  const lastTripCause = lastTrip
    ? `${lastTrip.command} from ${lastTrip.source_zone || "unknown"} (${lastTrip.source || "-"})`
    : "None";

  // Expected protection response
  const expectedResponse = faultActive
    ? recloseEnabled && !rclLockout
      ? "Recloser trips and auto-recloses (up to 3 shots)"
      : rclLockout
      ? "Recloser locked out - manual intervention required"
      : "Recloser trips, reclose disabled - sustained outage"
    : "No fault - normal coordination";

  const actualResponse = faultActive
    ? rclLockout
      ? "Lockout after " + shotCount + " shots - sustained outage"
      : !recloseEnabled
      ? "Reclose disabled - no automatic recovery"
      : !rclClosed
      ? "Recloser open - fault clearing in progress"
      : "Fault present - monitoring"
    : "Idle";

  return (
    <div className="space-y-4">
      {/* Operational impact banner */}
      {impactLevel !== "normal" && (
        <div className={`rounded border px-3 py-2 ${
          impactLevel === "outage"
            ? "border-red-800 bg-red-950/50"
            : "border-yellow-800 bg-yellow-950/30"
        }`}>
          <div className={`text-xs font-bold ${impactLevel === "outage" ? "text-red-400" : "text-yellow-400"}`}>
            {!bkrClosed
              ? `FEEDER OUTAGE - ${Math.round((genKW + critKW) || 700)} kW load lost, ~${Math.round(((genKW + critKW) || 700) * 3)} customers affected`
              : !rclClosed
              ? `PARTIAL OUTAGE - downstream loads de-energized, ~${Math.round(((genKW + critKW) || 700) * 3)} customers affected`
              : faultActive
              ? (faultCurrentA > 0
                  ? `FAULT ACTIVE - ${faultCurrentA.toFixed(0)} A at the ${relayFault ? "feeder breaker" : "recloser bus"}`
                  : `FAULT INTERRUPTED - protection opened the ${relayFault ? "feeder breaker" : "recloser"}, downstream service lost`)
              : protectionDegraded
              ? "PROTECTION DEGRADED - auto-reclose disabled, restoration risk increased"
              : critPU < 0.95
              ? `DEGRADED VOLTAGE - critical load at ${critPU.toFixed(2)} pu (${elec?.critical_load_voltage_v?.toFixed(1)}V)`
              : critPU > 1.05
              ? `HIGH VOLTAGE - critical load at ${critPU.toFixed(2)} pu (${elec?.critical_load_voltage_v?.toFixed(1)}V)`
              : "ABNORMAL CONDITION"
            }
          </div>
          {lastTrip && (impactLevel === "outage" || faultActive) && (
            <div className="text-[10px] text-slate-400 mt-1">
              Last event: <span className="text-slate-300">{lastTripCause}</span>
            </div>
          )}
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Left column: Electrical Snapshot + Voltage Profile */}
        <div className="space-y-4">

          {/* Electrical Snapshot */}
          <DetailPanel title="Electrical Snapshot" subtitle={`${nomKV} kV Distribution Feeder`}>
            <div className="grid grid-cols-3 gap-3">
              <MetricCell label="Feeder Voltage" value={allEnergized ? `${subPU.toFixed(3)} pu` : "-"} sub={allEnergized ? `${nomKV} kV nominal` : "De-energized"} ok={allEnergized && subPU >= 0.95 && subPU <= 1.05} />
              <MetricCell label="Feeder Current" value={`${currentA.toFixed(1)} A`} sub={`${loadingPct.toFixed(0)}% of ${feederRatingA}A rating`} ok={loadingPct < 80} warn={loadingPct >= 80 && loadingPct < 100} />
              <MetricCell label="Power Factor" value={pf > 0 ? pf.toFixed(3) : "-"} sub={pf >= 0.95 ? "Within target" : pf > 0 ? "Below 0.95 target" : ""} ok={pf >= 0.95} warn={pf > 0 && pf < 0.95} />
              <MetricCell label="Source Power" value={sourceKW > 0 ? `${sourceKW.toFixed(0)} kW` : "-"} sub={`${(genKW + critKW).toFixed(0)} kW served`} ok={sourceKW > 0} />
              <MetricCell label="Feeder Losses" value={lossesKW > 0 ? `${lossesKW.toFixed(1)} kW` : "-"} sub={sourceKW > 0 ? `${((lossesKW / sourceKW) * 100).toFixed(1)}% of source` : ""} ok={true} />
              <MetricCell label="Total Load" value={`${(genKW + critKW).toFixed(0)} kW`} sub={`General ${genKW.toFixed(0)} + Critical ${critKW.toFixed(0)}`} ok={genKW + critKW > 0} />
            </div>
          </DetailPanel>

          {/* Voltage Profile */}
          <DetailPanel title="Feeder Voltage Profile" subtitle="Per-unit voltage along feeder path">
            <div className="space-y-1">
              <VoltageProfileBar label="Substation Bus" pu={subPU} active={bkrClosed || !bkrClosed} />
              <div className="ml-3 border-l border-slate-800 pl-3 space-y-1">
                <div className="text-[9px] text-slate-600 -ml-1">▾ Line.Breaker ({bkrClosed ? "Closed" : "Open"})</div>
                <VoltageProfileBar label="Mid-Feeder (Recloser Bus)" pu={bkrClosed ? downPU : 0} active={bkrClosed} />
                <div className="ml-3 border-l border-slate-800 pl-3 space-y-1">
                  <div className="text-[9px] text-slate-600 -ml-1">▾ Line.Recloser ({rclClosed ? "Closed" : "Open"})</div>
                  <VoltageProfileBar label="General Service Load" pu={allEnergized ? downPU : 0} active={allEnergized} />
                  <VoltageProfileBar label="Critical Load (post-regulator)" pu={allEnergized ? critPU : 0} active={allEnergized} highlight />
                </div>
              </div>
            </div>
            {allEnergized && (
              <div className="mt-2 text-[10px] text-slate-500">
                Voltage drop: {((subPU - critPU) * 100).toFixed(1)}% source to critical load
                {tap !== 0 && ` · Regulator compensating ${tap > 0 ? "+" : ""}${(tap * 0.625).toFixed(1)}%`}
              </div>
            )}
          </DetailPanel>

          {/* Power Flow Summary */}
          <DetailPanel title="Power Flow Summary" subtitle="Active power distribution">
            <div className="font-mono text-[11px] space-y-1.5">
              <div className="flex items-center gap-2">
                <span className="text-slate-600 w-20">Source</span>
                <PowerBar kw={sourceKW} maxKw={800} color="sky" />
                <span className="text-sky-400 font-bold w-20 text-right">{sourceKW.toFixed(0)} kW</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-slate-600 w-20">General</span>
                <PowerBar kw={genKW} maxKw={800} color="green" />
                <span className="text-green-400 font-bold w-20 text-right">{genKW.toFixed(0)} kW</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-slate-600 w-20">Critical</span>
                <PowerBar kw={critKW} maxKw={800} color="amber" />
                <span className="text-amber-400 font-bold w-20 text-right">{critKW.toFixed(0)} kW</span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-slate-600 w-20">Losses</span>
                <PowerBar kw={lossesKW} maxKw={800} color="red" />
                <span className="text-red-400 font-bold w-20 text-right">{lossesKW.toFixed(1)} kW</span>
              </div>
            </div>
          </DetailPanel>
        </div>

        {/* Right column: Device Detail + Fault/Protection */}
        <div className="space-y-4">

          {/* Device / Protection Detail */}
          <DetailPanel title="Protection & Device Status" subtitle="Field device state from RTAC polling">
            <div className="space-y-3">
              {/* Feeder Breaker */}
              <DeviceDetail
                ansi="52"
                name="Feeder Breaker"
                ip="10.40.40.20"
                fields={[
                  { label: "State", value: bkrClosed ? "Closed" : "Open", ok: bkrClosed },
                  { label: "Lockout", value: relayLockout ? "Active" : "No", ok: !relayLockout, warn: !!relayLockout },
                  { label: "Fault Flag", value: relayFault ? "Set" : "Clear", ok: !relayFault, warn: !!relayFault },
                  { label: "Mode", value: relay?.remote_control_enabled ? "Remote" : "Local" },
                  { label: "Last Command", value: String(relay?.last_command_source || "-"), mono: true },
                ]}
              />

              {/* Recloser */}
              <DeviceDetail
                ansi="79"
                name="Mid-Feeder Recloser"
                ip="10.40.40.21"
                fields={[
                  { label: "State", value: rclClosed ? "Closed" : "Open", ok: rclClosed },
                  { label: "Auto-Reclose", value: recloseEnabled ? "Enabled" : "Disabled", ok: !!recloseEnabled, warn: !recloseEnabled },
                  { label: "Shots", value: `${shotCount} / 3`, ok: shotCount === 0, warn: shotCount > 0 && !rclLockout },
                  { label: "Lockout", value: rclLockout ? "Active" : "No", ok: !rclLockout, warn: !!rclLockout },
                  { label: "Last Command", value: String(recloser?.last_command_source || "-"), mono: true },
                ]}
              />

              {/* Capacitor Bank - shunt at load_bus, upstream of the regulator */}
              <DeviceDetail
                ansi="CAP"
                name="Capacitor Bank"
                ip="10.40.40.23"
                fields={[
                  { label: "State", value: capIn ? "Switched In" : "Switched Out", ok: capIn },
                  { label: "Mode", value: capAuto ? "Auto" : "Manual", warn: !capAuto },
                  { label: "Rating", value: `${capKvar} kVAR` },
                  { label: "Switch Ops", value: `${capSwitchCount} / 6`, ok: capSwitchCount < 5, warn: capSwitchCount >= 5 && !capLockout },
                  { label: "Lockout", value: capLockout ? "Active" : "No", ok: !capLockout, warn: capLockout },
                  { label: "Last Command", value: String(capbank?.last_command_source || "-"), mono: true },
                ]}
              />

              {/* Voltage Regulator - series element feeding the critical load */}
              <DeviceDetail
                ansi="90"
                name="Voltage Regulator"
                ip="10.40.40.22"
                fields={[
                  { label: "Mode", value: regManual ? "Manual" : "Auto", ok: !regManual, warn: !!regManual },
                  { label: "Tap Position", value: `${tap > 0 ? "+" : ""}${tap}`, ok: Math.abs(tap) <= 8, warn: Math.abs(tap) > 8 },
                  { label: "Tap Effect", value: `${tap > 0 ? "+" : ""}${(tap * 0.625).toFixed(1)}%` },
                  { label: "Target", value: `${(regSetpoint / nomV).toFixed(3)} pu (${regSetpoint}V)` },
                  { label: "Controlled Bus", value: "Critical Load Branch" },
                  { label: "Last Command", value: String(regulator?.last_command_source || "-"), mono: true },
                ]}
              />
            </div>
          </DetailPanel>

          {/* Fault & Protection Coordination */}
          <DetailPanel
            title="Fault & Protection Coordination"
            subtitle={faultActive ? "Fault condition active" : "No active faults"}
            alert={faultActive}
          >
            {faultActive ? (
              <div className="space-y-2">
                <div className="grid grid-cols-2 gap-2 text-[11px]">
                  <div>
                    <span className="text-slate-500">Status</span>
                    <div className="text-red-400 font-bold">Fault Active</div>
                  </div>
                  <div>
                    <span className="text-slate-500">Location</span>
                    <div className="text-slate-300">{relayFault ? "Feeder breaker (substation)" : "Recloser bus (mid-feeder)"}</div>
                  </div>
                  <div>
                    <span className="text-slate-500">Type</span>
                    <div className="text-slate-300">3-Phase Bolted</div>
                  </div>
                  <div>
                    <span className="text-slate-500">Fault Current</span>
                    <div className="text-red-400 font-bold">{faultCurrentA > 0 ? `${faultCurrentA.toFixed(0)} A` : "Cleared (device open)"}</div>
                  </div>
                </div>
                <div className="border-t border-slate-800/60 pt-2 space-y-1 text-[11px]">
                  <div>
                    <span className="text-slate-500">Expected Response: </span>
                    <span className="text-slate-300">{expectedResponse}</span>
                  </div>
                  <div>
                    <span className="text-slate-500">Actual Response: </span>
                    <span className={`font-medium ${
                      rclLockout || !recloseEnabled ? "text-red-400" : "text-yellow-400"
                    }`}>
                      {actualResponse}
                    </span>
                  </div>
                  {(!recloseEnabled || rclLockout) && (
                    <div className="mt-1 rounded border border-red-900/40 bg-red-950/20 px-2 py-1.5 text-[10px] text-red-300">
                      {!recloseEnabled
                        ? "Reclose was disabled - this removes automatic fault recovery capability. If a fault clears, the recloser cannot restore service without manual close command."
                        : "Recloser has locked out after exhausting all reclose attempts. Manual intervention required to restore service."
                      }
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <div className="space-y-2">
                <div className="text-[11px] text-slate-400">No active faults on the feeder.</div>
                <div className="rounded border border-slate-800/60 bg-slate-900/30 p-2.5 text-[10px] text-slate-500 space-y-1">
                  <div className="text-slate-400 font-medium mb-1">Protection Coordination Hierarchy</div>
                  <div>1. <span className="text-green-400">Recloser (79)</span> clears downstream faults first (up to 3 shots)</div>
                  <div>2. <span className="text-sky-400">Feeder Breaker (52)</span> is upstream backup if recloser fails to clear</div>
                  <div>3. <span className="text-cyan-400">Regulator (90)</span> maintains voltage at critical load through tap adjustment</div>
                  <div className="mt-1.5 border-t border-slate-800/40 pt-1.5 text-slate-500">
                    If auto-reclose is disabled by an attacker, the recloser cannot automatically restore service after a transient fault - this is the core cyber-to-physical impact in Exercise 2.
                  </div>
                </div>
              </div>
            )}
          </DetailPanel>
        </div>
      </div>

      {/* OpenDSS reference */}
      <div className="mt-3 border-t border-slate-800/40 pt-2 text-[10px] text-slate-600">
        Power flow calculated by{" "}
        <a
          href="https://www.epri.com/pages/sa/opendss"
          target="_blank"
          rel="noopener noreferrer"
          className="text-sky-600 hover:text-sky-400 underline underline-offset-2"
        >
          EPRI OpenDSS
        </a>
        {" "}via{" "}
        <a
          href="https://dss-extensions.org/"
          target="_blank"
          rel="noopener noreferrer"
          className="text-sky-600 hover:text-sky-400 underline underline-offset-2"
        >
          DSS-Extensions
        </a>
        {" "}- 3-phase unbalanced solver with IEEE 13-bus line impedances.
        Load values include simulated demand fluctuation (±3%).
      </div>
    </div>
  );
}

// ── Electrical Detail Subcomponents ───────────────────────────────

function DetailPanel({
  title,
  subtitle,
  alert,
  children,
}: {
  title: string;
  subtitle?: string;
  alert?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className={`rounded-lg border p-3 ${
      alert ? "border-red-900/60 bg-red-950/10" : "border-slate-800 bg-slate-900/40"
    }`}>
      <div className="mb-2">
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">{title}</div>
        {subtitle && <div className="text-[9px] text-slate-600">{subtitle}</div>}
      </div>
      {children}
    </div>
  );
}

function MetricCell({
  label,
  value,
  sub,
  ok,
  warn,
}: {
  label: string;
  value: string;
  sub?: string;
  ok?: boolean;
  warn?: boolean;
}) {
  return (
    <div className="rounded border border-slate-800/60 bg-slate-950/50 px-2 py-1.5">
      <div className="text-[9px] text-slate-500 font-medium">{label}</div>
      <div className={`text-sm font-bold font-mono ${
        ok === false ? "text-red-400" : warn ? "text-yellow-400" : ok ? "text-slate-200" : "text-slate-400"
      }`}>
        {value}
      </div>
      {sub && <div className="text-[9px] text-slate-600">{sub}</div>}
    </div>
  );
}

function VoltageProfileBar({ label, pu, active, highlight }: { label: string; pu: number; active: boolean; highlight?: boolean }) {
  // Map pu to bar width (0.90 pu = 0%, 1.10 pu = 100%)
  const pct = active && pu > 0 ? Math.max(0, Math.min(100, ((pu - 0.90) / 0.20) * 100)) : 0;
  const inBand = pu >= 0.95 && pu <= 1.05;
  const barColor = !active || pu === 0
    ? "bg-slate-800"
    : inBand
    ? highlight ? "bg-amber-500/60" : "bg-green-500/50"
    : "bg-red-500/50";

  return (
    <div className="flex items-center gap-2 text-[11px]">
      <span className={`w-44 shrink-0 truncate ${highlight ? "text-amber-400 font-medium" : "text-slate-400"}`}>{label}</span>
      <div className="flex-1 h-3 bg-slate-900 rounded-sm border border-slate-800/60 relative overflow-hidden">
        {/* ANSI C84.1 band indicator */}
        <div className="absolute inset-0 flex">
          <div style={{ width: "25%" }} />
          <div style={{ width: "50%" }} className="bg-green-900/15 border-x border-green-800/20" />
        </div>
        <div className={`h-full rounded-sm ${barColor} transition-all duration-300`} style={{ width: `${pct}%` }} />
      </div>
      <span className={`w-16 text-right font-mono font-bold ${
        !active || pu === 0 ? "text-slate-600" : inBand ? "text-green-400" : "text-red-400"
      }`}>
        {active && pu > 0 ? `${pu.toFixed(3)}` : "- "}
      </span>
    </div>
  );
}

function PowerBar({ kw, maxKw, color }: { kw: number; maxKw: number; color: string }) {
  const pct = maxKw > 0 ? Math.max(0, Math.min(100, (kw / maxKw) * 100)) : 0;
  const colorMap: Record<string, string> = {
    sky: "bg-sky-500/50",
    green: "bg-green-500/50",
    amber: "bg-amber-500/50",
    red: "bg-red-500/40",
  };
  return (
    <div className="flex-1 h-2.5 bg-slate-900 rounded-sm border border-slate-800/60 overflow-hidden">
      <div className={`h-full rounded-sm ${colorMap[color] || colorMap.sky} transition-all duration-300`} style={{ width: `${pct}%` }} />
    </div>
  );
}

type DeviceField = {
  label: string;
  value: string;
  ok?: boolean;
  warn?: boolean;
  mono?: boolean;
};

function DeviceDetail({ ansi, name, ip, fields }: { ansi: string; name: string; ip: string; fields: DeviceField[] }) {
  return (
    <div className="rounded border border-slate-800/60 bg-slate-950/40 p-2.5">
      <div className="flex items-center gap-2 mb-1.5">
        <span className="rounded border border-slate-700 bg-slate-800 px-1.5 py-0.5 text-[10px] font-bold text-slate-300">{ansi}</span>
        <span className="text-xs font-medium text-slate-300">{name}</span>
        <span className="text-[9px] text-slate-600 font-mono">{ip}</span>
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-0.5 text-[11px]">
        {fields.map((f, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <span className="text-slate-500 w-24 shrink-0">{f.label}</span>
            <span className={`font-medium ${
              f.ok === false || f.warn ? (f.warn ? "text-yellow-400" : "text-red-400")
              : f.ok ? "text-green-400"
              : "text-slate-300"
            } ${f.mono ? "font-mono text-[10px]" : ""}`}>
              {f.value}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
