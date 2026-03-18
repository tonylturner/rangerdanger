"use client";

import { useEffect, useState, useCallback } from "react";
import {
  getSubstationState,
  getSubstationAudit,
  getSubstationNetworkEvents,
  getFirewallComparison,
  getActiveFirewallConfig,
  applyFirewallConfig,
  sendSubstationCommand,
  executeScenarioStep,
  type SubstationState,
  type AuditEntry,
  type NetworkEvent,
  type PolicyComparison,
} from "../lib/api";

export function SubstationPanel() {
  const [state, setState] = useState<SubstationState | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [networkEvents, setNetworkEvents] = useState<NetworkEvent[]>([]);
  const [tab, setTab] = useState<"diagram" | "commands" | "correlation" | "segmentation">("diagram");
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
    { id: "segmentation" as const, label: "containd Segmentation" },
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
        {tab === "correlation" && <CommandAuditView entries={audit} networkEvents={networkEvents} />}
        {tab === "segmentation" && <SegmentationView />}
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

  const lowVoltage = (elec?.critical_load_voltage_v ?? 120) < 114;
  const highVoltage = (elec?.critical_load_voltage_v ?? 120) > 126;
  const recloseOff = recloser && !recloser.reclose_enabled;
  const anyAlarm = !bkrClosed || !rclClosed || lowVoltage || highVoltage || recloseOff;

  const totalKw = (elec?.general_load_kw ?? 0) + (elec?.critical_load_kw ?? 0);

  return (
    <div className="space-y-3">
      {/* Alarm banner — operational language */}
      {anyAlarm && (
        <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2 text-xs font-bold text-red-400">
            <span className="animate-pulse">ALARM</span>
            {!bkrClosed && <span className="rounded bg-red-900/60 px-2 py-0.5">FEEDER BREAKER OPEN — customers without power</span>}
            {bkrClosed && !rclClosed && <span className="rounded bg-red-900/60 px-2 py-0.5">RECLOSER OPEN — downstream loads lost</span>}
            {recloseOff && <span className="rounded bg-yellow-900/60 px-2 py-0.5 text-yellow-400">AUTO-RECLOSE DISABLED — no fault recovery</span>}
            {lowVoltage && <span className="rounded bg-red-900/60 px-2 py-0.5">LOW VOLTAGE — equipment damage risk</span>}
            {highVoltage && <span className="rounded bg-red-900/60 px-2 py-0.5">HIGH VOLTAGE — equipment damage risk</span>}
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
            {relay?.last_command_source && relay.last_command_source !== "—" && (
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
                  {genEnergized ? `${elec?.general_load_kw ?? 0} kW` : "NO POWER"}
                </span>
              </div>

              {/* Critical load with regulator */}
              <div className="py-1">
                <div className="flex items-center gap-2">
                  <LoadSymbol energized={critEnergized} critical />
                  <span className="text-slate-300 font-medium">Critical Load</span>
                  <span className={`font-bold text-xs ${critEnergized ? "text-green-400" : "text-red-400"}`}>
                    {critEnergized ? `${elec?.critical_load_kw ?? 0} kW` : "NO POWER"}
                  </span>
                </div>

                {/* Regulator 90 */}
                <div className="ml-6 mt-1 flex items-center gap-2 text-[11px]">
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
            {totalKw > 0 ? `${totalKw} kW serving ~${Math.round(totalKw * 3)} customers` : "ALL CUSTOMERS WITHOUT POWER"}
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

      <div className="grid gap-3 md:grid-cols-3">
        <DeviceGroup title="Feeder Breaker (52)" subtitle="10.40.40.20">
          <CmdButton label="TRIP" onClick={() => execCmd("relay", "trip")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("relay", "close")} variant="success" />
          <CmdButton label="Lockout" onClick={() => execCmd("relay", "lockout")} variant="warning" />
          <CmdButton label="Unlock" onClick={() => execCmd("relay", "unlock")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("relay", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("relay", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Recloser (79)" subtitle="10.40.40.21">
          <CmdButton label="OPEN" onClick={() => execCmd("recloser", "open")} variant="danger" />
          <CmdButton label="CLOSE" onClick={() => execCmd("recloser", "close")} variant="success" />
          <CmdButton label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose")} variant="success" />
          <CmdButton label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose")} variant="warning" />
          <CmdButton label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Inject Fault" onClick={() => execCmd("recloser", "inject_fault")} variant="danger" />
          <CmdButton label="Clear Fault" onClick={() => execCmd("recloser", "clear_fault")} />
        </DeviceGroup>

        <DeviceGroup title="Voltage Regulator (90)" subtitle="10.40.40.22">
          <CmdButton label="Raise Tap" onClick={() => execCmd("regulator", "raise_tap")} />
          <CmdButton label="Lower Tap" onClick={() => execCmd("regulator", "lower_tap")} />
          <div className="w-full border-t border-slate-800/50 my-0.5" />
          <CmdButton label="Manual Mode" onClick={() => execCmd("regulator", "set_manual")} variant="warning" />
          <CmdButton label="Auto Mode" onClick={() => execCmd("regulator", "set_auto")} variant="success" />
        </DeviceGroup>
      </div>

      <div className="text-[10px] text-slate-600">
        Commands are sent through the RTAC to field devices on the 10.40.40.0/24 network.
        The containd firewall controls which zones can reach these devices.
      </div>
    </div>
  );
}

function DeviceGroup({ title, subtitle, children }: { title: string; subtitle: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 p-3">
      <h4 className="text-xs font-bold text-slate-300">{title}</h4>
      <div className="text-[9px] text-slate-600 mb-2">{subtitle}</div>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function CmdButton({ label, onClick, variant }: { label: string; onClick: () => void; variant?: "danger" | "success" | "warning" }) {
  const colors = {
    danger: "border-red-800/60 bg-red-950/40 text-red-400 hover:bg-red-900/40",
    success: "border-green-800/60 bg-green-950/40 text-green-400 hover:bg-green-900/40",
    warning: "border-yellow-800/60 bg-yellow-950/40 text-yellow-400 hover:bg-yellow-900/40",
  };
  const cls = variant ? colors[variant] : "border-slate-700 bg-slate-800/40 text-slate-300 hover:bg-slate-700/40";
  return (
    <button onClick={onClick} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}

// ── Command Audit View ───────────────────────────────────────────

// Known host IP → friendly name mapping for the substation lab
const HOST_NAMES: Record<string, string> = {
  "10.10.10.10": "corp-ws",
  "10.10.10.50": "kali",
  "10.20.20.10": "vendor-jump",
  "10.20.20.20": "eng-ws",
  "10.30.30.10": "fuxa-hmi",
  "10.30.30.20": "rtac",
  "10.30.30.30": "openplc",
  "10.40.40.20": "relay",
  "10.40.40.21": "recloser",
  "10.40.40.22": "regulator",
  "10.50.50.20": "opendss",
  "10.10.10.2": "fw-ent",
  "10.20.20.2": "fw-dmz",
  "10.30.30.2": "fw-ot",
  "10.40.40.2": "fw-field",
};

const ZONE_FOR_IP: Record<string, string> = {
  "10.10.10": "enterprise",
  "10.20.20": "vendor",
  "10.30.30": "ot_ops",
  "10.40.40": "field",
  "10.50.50": "physics",
};

function ipToZone(ip: string): string {
  const prefix = ip.split(".").slice(0, 3).join(".");
  return ZONE_FOR_IP[prefix] || "unknown";
}

function hostLabel(ip: string): string {
  return HOST_NAMES[ip] || ip;
}

type MatrixEntry = {
  source: string;
  dest: string;
  protocols: Record<string, number>;
  totalEvents: number;
  srcZone: string;
  dstZone: string;
  crossZone: boolean;
};

function buildTrafficMatrix(entries: AuditEntry[], networkEvents: NetworkEvent[]): MatrixEntry[] {
  const map = new Map<string, MatrixEntry>();

  // Derive source IPs from audit entries using known zone→subnet mapping
  const zoneToSubnet: Record<string, string> = {
    enterprise: "10.10.10",
    vendor: "10.20.20",
    ot_ops: "10.30.30",
    field: "10.40.40",
  };

  // Build from audit entries (command-level)
  for (const e of entries) {
    // Source: use source field or infer from zone
    let srcIP = e.source;
    if (!srcIP.includes(".")) {
      // Not an IP — try to map from zone
      const subnet = zoneToSubnet[e.source_zone];
      srcIP = subnet ? `${subnet}.x` : e.source;
    }

    // Target: map device names to IPs
    const targetIPs: Record<string, string> = {
      "relay-sim": "10.40.40.20",
      "recloser-sim": "10.40.40.21",
      "regulator-sim": "10.40.40.22",
      "rtac-sim": "10.30.30.20",
    };
    const dstIP = targetIPs[e.target] || e.target;
    const key = `${srcIP}→${dstIP}`;

    if (!map.has(key)) {
      const srcZone = ipToZone(srcIP);
      const dstZone = ipToZone(dstIP);
      map.set(key, {
        source: srcIP,
        dest: dstIP,
        protocols: {},
        totalEvents: 0,
        srcZone,
        dstZone,
        crossZone: srcZone !== dstZone,
      });
    }
    const entry = map.get(key)!;
    const proto = "HTTP/API";
    entry.protocols[proto] = (entry.protocols[proto] || 0) + 1;
    entry.totalEvents++;
  }

  // Build from DPI network events
  for (const e of networkEvents) {
    const srcIP = e.source.split(":")[0]; // strip port
    const dstIP = e.dest.split(":")[0];
    const key = `${srcIP}→${dstIP}`;

    if (!map.has(key)) {
      const srcZone = ipToZone(srcIP);
      const dstZone = ipToZone(dstIP);
      map.set(key, {
        source: srcIP,
        dest: dstIP,
        protocols: {},
        totalEvents: 0,
        srcZone,
        dstZone,
        crossZone: srcZone !== dstZone,
      });
    }
    const entry = map.get(key)!;
    const proto = e.protocol && e.protocol !== "-" ? e.protocol : `port/${e.dst_port || "?"}`;
    entry.protocols[proto] = (entry.protocols[proto] || 0) + 1;
    entry.totalEvents++;
  }

  return Array.from(map.values()).sort((a, b) => b.totalEvents - a.totalEvents);
}

const ZONE_COLORS: Record<string, string> = {
  enterprise: "text-red-400",
  vendor: "text-purple-400",
  ot_ops: "text-orange-400",
  field: "text-green-400",
  physics: "text-cyan-400",
};

const ZONE_BG: Record<string, string> = {
  enterprise: "bg-red-950/20",
  vendor: "bg-purple-950/20",
  ot_ops: "bg-orange-950/20",
  field: "bg-green-950/20",
  physics: "bg-cyan-950/20",
};

const PROTOCOL_COLORS: Record<string, string> = {
  "Modbus/TCP": "bg-amber-900/40 text-amber-300 border-amber-800/50",
  "HTTP/API": "bg-blue-900/40 text-blue-300 border-blue-800/50",
  "HTTP": "bg-blue-900/40 text-blue-300 border-blue-800/50",
  "DNS": "bg-teal-900/40 text-teal-300 border-teal-800/50",
  "SSH": "bg-emerald-900/40 text-emerald-300 border-emerald-800/50",
  "TLS": "bg-indigo-900/40 text-indigo-300 border-indigo-800/50",
};

function protoColor(proto: string): string {
  return PROTOCOL_COLORS[proto] || "bg-slate-800/40 text-slate-300 border-slate-700/50";
}

function TrafficMatrixView({ entries, networkEvents }: { entries: AuditEntry[]; networkEvents: NetworkEvent[] }) {
  const [filterZone, setFilterZone] = useState<string>("all");
  const [crossZoneOnly, setCrossZoneOnly] = useState(false);

  const matrix = buildTrafficMatrix(entries, networkEvents);
  const filtered = matrix.filter((m) => {
    if (crossZoneOnly && !m.crossZone) return false;
    if (filterZone !== "all" && m.srcZone !== filterZone && m.dstZone !== filterZone) return false;
    return true;
  });

  // Collect unique protocols across all entries
  const allProtocols = new Set<string>();
  for (const m of matrix) {
    for (const p of Object.keys(m.protocols)) allProtocols.add(p);
  }

  // Summary stats
  const uniquePairs = matrix.length;
  const crossZonePairs = matrix.filter((m) => m.crossZone).length;
  const totalFlows = matrix.reduce((sum, m) => sum + m.totalEvents, 0);

  if (matrix.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-slate-500">
        No traffic observed yet. Run scenarios or generate traffic to build the communication matrix.
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Summary bar */}
      <div className="flex items-center gap-4 text-[10px]">
        <div className="flex items-center gap-1.5">
          <span className="text-slate-500">Pairs:</span>
          <span className="text-slate-300 font-bold">{uniquePairs}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-slate-500">Cross-zone:</span>
          <span className="text-amber-400 font-bold">{crossZonePairs}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-slate-500">Flows:</span>
          <span className="text-slate-300 font-bold">{totalFlows}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-slate-500">Protocols:</span>
          {Array.from(allProtocols).map((p) => (
            <span key={p} className={`rounded border px-1 py-0.5 text-[9px] ${protoColor(p)}`}>{p}</span>
          ))}
        </div>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-2">
        <select
          value={filterZone}
          onChange={(e) => setFilterZone(e.target.value)}
          className="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-[10px] text-slate-300"
        >
          <option value="all">All Zones</option>
          <option value="enterprise">Enterprise</option>
          <option value="vendor">Vendor</option>
          <option value="ot_ops">OT Ops</option>
          <option value="field">Field</option>
          <option value="physics">Physics</option>
        </select>
        <button
          onClick={() => setCrossZoneOnly(!crossZoneOnly)}
          className={`rounded border px-2 py-1 text-[10px] font-medium ${
            crossZoneOnly
              ? "border-amber-700 bg-amber-950/30 text-amber-400"
              : "border-slate-700 bg-slate-800/40 text-slate-500"
          }`}
        >
          Cross-zone only
        </button>
      </div>

      {/* Matrix table */}
      <div className="max-h-[440px] overflow-y-auto">
        <table className="w-full text-[11px]">
          <thead>
            <tr className="border-b border-slate-800 text-left text-[10px] text-slate-500">
              <th className="pb-1.5 pr-2 font-medium">Source</th>
              <th className="pb-1.5 pr-2 font-medium">Dest</th>
              <th className="pb-1.5 pr-2 font-medium">Protocols</th>
              <th className="pb-1.5 pr-2 font-medium text-right">Events</th>
              <th className="pb-1.5 font-medium">Seg. Note</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((m, i) => (
              <tr key={i} className={`border-b border-slate-800/40 ${m.crossZone ? "bg-amber-950/5" : ""}`}>
                <td className="py-1.5 pr-2">
                  <div className="flex items-center gap-1.5">
                    <span className={`inline-block h-1.5 w-1.5 rounded-full ${m.srcZone === "enterprise" ? "bg-red-500" : m.srcZone === "vendor" ? "bg-purple-500" : m.srcZone === "ot_ops" ? "bg-orange-500" : m.srcZone === "field" ? "bg-green-500" : "bg-cyan-500"}`} />
                    <span className={`font-medium ${ZONE_COLORS[m.srcZone] || "text-slate-300"}`}>{hostLabel(m.source)}</span>
                    <span className="text-slate-600 text-[9px]">{m.source}</span>
                  </div>
                </td>
                <td className="py-1.5 pr-2">
                  <div className="flex items-center gap-1.5">
                    <span className={`inline-block h-1.5 w-1.5 rounded-full ${m.dstZone === "enterprise" ? "bg-red-500" : m.dstZone === "vendor" ? "bg-purple-500" : m.dstZone === "ot_ops" ? "bg-orange-500" : m.dstZone === "field" ? "bg-green-500" : "bg-cyan-500"}`} />
                    <span className={`font-medium ${ZONE_COLORS[m.dstZone] || "text-slate-300"}`}>{hostLabel(m.dest)}</span>
                    <span className="text-slate-600 text-[9px]">{m.dest}</span>
                  </div>
                </td>
                <td className="py-1.5 pr-2">
                  <div className="flex flex-wrap gap-1">
                    {Object.entries(m.protocols).map(([proto, count]) => (
                      <span key={proto} className={`rounded border px-1 py-0 text-[9px] ${protoColor(proto)}`}>
                        {proto} ({count})
                      </span>
                    ))}
                  </div>
                </td>
                <td className="py-1.5 pr-2 text-right font-mono text-slate-400">{m.totalEvents}</td>
                <td className="py-1.5 text-[10px]">
                  {m.crossZone ? (
                    <span className="text-amber-400">
                      {m.srcZone} → {m.dstZone}
                    </span>
                  ) : (
                    <span className="text-slate-600">intra-zone</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Segmentation insights */}
      {crossZonePairs > 0 && (
        <div className="rounded border border-amber-900/30 bg-amber-950/10 p-2.5 text-[10px]">
          <div className="font-medium text-amber-400 mb-1">Segmentation Planning Insights</div>
          <div className="space-y-0.5 text-slate-400">
            {Array.from(new Set(matrix.filter((m) => m.crossZone).map((m) => `${m.srcZone}→${m.dstZone}`))).map((pair) => {
              const flows = matrix.filter((m) => `${m.srcZone}→${m.dstZone}` === pair);
              const protos = new Set(flows.flatMap((f) => Object.keys(f.protocols)));
              return (
                <div key={pair} className="flex items-center gap-2">
                  <span className="text-amber-300/70 font-medium w-28">{pair}</span>
                  <span className="text-slate-500">{flows.length} host pair(s) via</span>
                  {Array.from(protos).map((p) => (
                    <span key={p} className={`rounded border px-1 py-0 text-[9px] ${protoColor(p)}`}>{p}</span>
                  ))}
                </div>
              );
            })}
            <div className="mt-1.5 text-slate-500 italic">
              Use this matrix to define containd DPI allow-rules for each cross-zone pair.
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function CommandAuditView({ entries, networkEvents }: { entries: AuditEntry[]; networkEvents: NetworkEvent[] }) {
  const [view, setView] = useState<"timeline" | "matrix">("timeline");
  const [showDPI, setShowDPI] = useState(networkEvents.length > 0);

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
  };

  const zoneBorder: Record<string, string> = {
    enterprise: "border-l-red-500",
    vendor: "border-l-purple-500",
    ot_ops: "border-l-orange-500",
    field: "border-l-green-500",
    operator: "border-l-sky-500",
  };

  return (
    <div className="space-y-3">
      {/* View toggle + controls */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1">
          <button
            onClick={() => setView("timeline")}
            className={`rounded border px-2 py-1 text-[10px] font-medium ${
              view === "timeline" ? "border-sky-700 bg-sky-950/30 text-sky-400" : "border-slate-700 bg-slate-800/40 text-slate-500"
            }`}
          >
            Timeline
          </button>
          <button
            onClick={() => setView("matrix")}
            className={`rounded border px-2 py-1 text-[10px] font-medium ${
              view === "matrix" ? "border-amber-700 bg-amber-950/30 text-amber-400" : "border-slate-700 bg-slate-800/40 text-slate-500"
            }`}
          >
            Traffic Matrix
          </button>
        </div>
        {view === "timeline" && networkEvents.length > 0 && (
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

      {view === "matrix" ? (
        <TrafficMatrixView entries={entries} networkEvents={networkEvents} />
      ) : (
        <>
          <span className="text-[10px] text-slate-500">
            Who sent commands, from which zone, and what happened to the feeder.
          </span>
          <div className="max-h-[500px] overflow-y-auto space-y-1">
            {entries.map((e, i) => {
              const zone = e.source_zone || "unknown";
              const wasAttack = zone === "enterprise" || zone === "vendor";
              const succeeded = e.result === "executed";
              const harmful = e.process_impact?.includes("de-energized") || e.process_impact?.includes("DISABLED") || e.process_impact?.includes("OPENED") || e.process_impact?.includes("LOCKED");

              return (
                <div key={`a-${i}`} className={`rounded border-l-2 border border-slate-800/60 bg-slate-900/40 p-2 text-xs ${zoneBorder[zone] || "border-l-slate-500"}`}>
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
                  {/* Operational consequence — always prominent */}
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
              <div key={`n-${i}`} className="rounded border border-purple-900/30 bg-purple-950/10 p-2 text-xs">
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
        </>
      )}
    </div>
  );
}

// ── containd Segmentation View ───────────────────────────────────

function SegmentationView() {
  const [activeConfig, setActiveConfig] = useState<string | null>(null);
  const [comparison, setComparison] = useState<PolicyComparison | null>(null);
  const [applying, setApplying] = useState(false);
  const [lastApply, setLastApply] = useState(0);
  const [testResult, setTestResult] = useState<{blocked: boolean; detail: string} | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    getActiveFirewallConfig().then((r) => setActiveConfig(r.active_config)).catch(() => {});
    getFirewallComparison().then(setComparison).catch(() => {});
  }, [lastApply]);

  const handleApply = async (config: "weak" | "improved") => {
    setApplying(true);
    try {
      const res = await applyFirewallConfig(config);
      setActiveConfig(res.active_config);
      setLastApply((c) => c + 1);
      setTestResult(null);
    } catch {
      // error
    } finally {
      setApplying(false);
    }
  };

  const handleTestConfig = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      // Use the step execution endpoint to test an enterprise->field command
      const res = await executeScenarioStep("enterprise-to-breaker", 5);
      // Step 5 is "Re-test attack" which sends trip from 10.10.10.50
      const blocked = !res.success;
      setTestResult({
        blocked,
        detail: res.results?.[0]?.detail || (blocked ? "Command blocked by containd" : "Command reached field device"),
      });
      // If the command actually succeeded (weak config), restore the breaker
      if (!blocked) {
        await executeScenarioStep("enterprise-to-breaker", 3); // restore step
      }
    } catch (e) {
      setTestResult({ blocked: false, detail: `Test error: ${e}` });
    } finally {
      setTesting(false);
    }
  };

  if (!comparison) {
    return <div className="py-8 text-center text-sm text-slate-500">Loading policy comparison...</div>;
  }

  return (
    <div className="space-y-4">
      {/* containd as the central element */}
      <div className={`rounded-lg border-2 p-4 ${
        activeConfig === "improved"
          ? "border-green-700/60 bg-green-950/10"
          : "border-red-700/60 bg-red-950/10"
      }`}>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
              containd NGFW — Active Policy
            </div>
            <div className={`text-lg font-bold mt-0.5 ${activeConfig === "improved" ? "text-green-400" : "text-red-400"}`}>
              {activeConfig === "improved" ? "Hardened Segmentation" : "Weak Baseline (vulnerable)"}
            </div>
            <div className="text-[11px] text-slate-500 mt-0.5">
              {activeConfig === "improved"
                ? "Only the RTAC can reach field devices. Enterprise and vendor zones are blocked."
                : "All zones can reach field devices directly. Attackers have clear paths to breakers and regulators."
              }
            </div>
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => handleApply("weak")}
              disabled={applying || activeConfig === "weak"}
              className={`rounded border px-3 py-1.5 text-[11px] font-medium transition-colors disabled:opacity-40 ${
                activeConfig === "weak"
                  ? "border-red-700/60 bg-red-950/30 text-red-400"
                  : "border-slate-600 text-slate-400 hover:border-red-700 hover:text-red-400"
              }`}
            >
              {activeConfig === "weak" ? "Weak (active)" : "Reset to Weak"}
            </button>
            <button
              onClick={() => handleApply("improved")}
              disabled={applying || activeConfig === "improved"}
              className={`rounded border px-3 py-1.5 text-[11px] font-medium transition-colors disabled:opacity-40 ${
                activeConfig === "improved"
                  ? "border-green-700/60 bg-green-950/30 text-green-400"
                  : "border-slate-600 text-slate-400 hover:border-green-700 hover:text-green-400"
              }`}
            >
              {activeConfig === "improved" ? "Hardened (active)" : "Apply Hardened"}
            </button>
            <button
              onClick={handleTestConfig}
              disabled={testing}
              className="rounded border border-sky-700 bg-sky-950/30 px-3 py-1.5 text-[11px] font-medium text-sky-400 hover:bg-sky-900/30 disabled:opacity-40 transition-colors"
            >
              {testing ? "Testing..." : "Test Segmentation"}
            </button>
          </div>
        </div>
      </div>

      {/* Test result */}
      {testResult && (
        <div className={`rounded border p-3 text-xs ${
          testResult.blocked
            ? "border-green-800/60 bg-green-950/20 text-green-400"
            : "border-red-800/60 bg-red-950/20 text-red-400"
        }`}>
          <span className="font-bold">{testResult.blocked ? "BLOCKED" : "ALLOWED"}</span>
          <span className="ml-2 text-slate-400">{testResult.detail}</span>
          {!testResult.blocked && activeConfig === "improved" && (
            <div className="mt-1 text-yellow-400">Warning: Command should be blocked with hardened policy</div>
          )}
          {testResult.blocked && activeConfig === "improved" && (
            <div className="mt-1">Enterprise→field traffic correctly blocked by containd NGFW</div>
          )}
        </div>
      )}

      {/* Zone-pair rules — simplified */}
      <div className="space-y-1.5">
        {comparison.diffs.map((d, i) => {
          const tightened = d.change === "tightened" || d.change === "added";
          return (
            <div key={i} className={`rounded border p-3 text-xs ${
              tightened ? "border-green-900/40 bg-green-950/10" : "border-slate-800/60 bg-slate-900/30"
            }`}>
              <div className="flex items-center justify-between">
                <span className="text-slate-300 font-medium">{d.zone_pair}</span>
                <div className="flex items-center gap-2">
                  <ActionChip action={d.weak_action} label="Weak" />
                  {d.improved_action !== d.weak_action && (
                    <>
                      <span className="text-slate-600">→</span>
                      <ActionChip action={d.improved_action} label="Hardened" />
                    </>
                  )}
                  {tightened && (
                    <span className="text-[9px] font-bold text-green-400 uppercase">
                      tightened
                    </span>
                  )}
                </div>
              </div>
              {tightened && d.improved_rule && (
                <div className="mt-1.5 text-[10px] text-slate-500">{d.improved_rule}</div>
              )}
            </div>
          );
        })}
      </div>

      {/* Core principle — single sentence */}
      <div className="text-[10px] text-slate-600 border-t border-slate-800/40 pt-2">
        The hardened policy ensures only the RTAC (10.40.40.10) can send control commands to field devices.
        Enterprise and vendor zones are blocked from direct field device access.
      </div>
    </div>
  );
}

function ActionChip({ action, label }: { action: string; label: string }) {
  if (!action) return null;
  const cls = action === "ALLOW"
    ? "text-green-400 border-green-800/40"
    : action === "DENY"
    ? "text-red-400 border-red-800/40"
    : "text-yellow-400 border-yellow-800/40";
  return <span className={`rounded border px-1.5 py-0.5 text-[9px] font-bold ${cls}`}>{action}</span>;
}
