"use client";

import { memo } from "react";
import { Handle, Position, NodeProps } from "reactflow";
import {
  Shield,
  Server,
  Network,
  Waypoints,
  Monitor,
  Database,
  ShieldAlert,
  Satellite,
  Clock,
} from "lucide-react";

// ── Custom SVG icons ──────────────────────────────────────────────

function DockerIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M13 3h2v2h-2V3zm-3 0h2v2h-2V3zm-3 0h2v2H7V3zm6 3h2v2h-2V6zm-3 0h2v2h-2V6zm-3 0h2v2H7V6zM4 6h2v2H4V6zm3 3h2v2H7V9zm-3 0h2v2H4V9z" fill={color} />
      <path d="M22 10.5c-.7-.4-1.5-.5-2.3-.4-.2-1.2-1-2.2-2-2.8l-.4-.3-.3.4c-.4.6-.6 1.3-.5 2 0 .5.1 1 .4 1.4-.6.3-1.2.5-1.8.5H1.1l-.1.6c-.1 1.5.2 3 .9 4.3 1 1.6 2.6 2.4 4.5 2.4 4.2 0 7.4-1.9 8.9-5.4.6.1 1.7.1 2.3-.9l.1-.2-.3-.2c-.5-.2-1.5-.5-2.4.1z" fill={color} />
    </svg>
  );
}

function TuxIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M12 2C9.5 2 8 4.5 8 7c0 1.5.5 2.8 1 3.5-.5 1-1.5 2.5-2 4-.5 1.5-.5 3 0 4 .3.5.7 1 1.2 1.3-.2.4-.2.8-.2 1.2h2c0-.3.1-.5.2-.7.3.1.5.2.8.2.3 0 .6-.1.8-.2.1.2.2.4.2.7h2c0-.4-.1-.8-.2-1.2.5-.3.9-.8 1.2-1.3.5-1 .5-2.5 0-4-.5-1.5-1.5-3-2-4 .5-.7 1-2 1-3.5 0-2.5-1.5-5-4-5z" fill={color} />
      <circle cx="10.5" cy="6" r=".8" fill="#0f172a" />
      <circle cx="13.5" cy="6" r=".8" fill="#0f172a" />
      <ellipse cx="12" cy="8.5" rx="1" ry=".5" fill="#f59e0b" />
    </svg>
  );
}

function KaliIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path d="M12 2L6 8l2 2-4 4 3 3 2-2 1 5h4l1-5 2 2 3-3-4-4 2-2L12 2z" fill={color} />
      <path d="M10 10l2-2 2 2-2 2-2-2z" fill="#0f172a" />
    </svg>
  );
}

function OpenPLCIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect x="4" y="3" width="16" height="18" rx="2" stroke={color} strokeWidth="1.5" fill="none" />
      <rect x="6" y="5" width="5" height="3" rx=".5" fill={color} fillOpacity=".3" />
      <rect x="13" y="5" width="5" height="3" rx=".5" fill={color} fillOpacity=".3" />
      <circle cx="7.5" cy="12" r="1" fill="#22c55e" />
      <circle cx="10.5" cy="12" r="1" fill="#22c55e" />
      <circle cx="13.5" cy="12" r="1" fill={color} fillOpacity=".4" />
      <circle cx="16.5" cy="12" r="1" fill={color} fillOpacity=".4" />
      <line x1="6" y1="15" x2="7" y2="15" stroke={color} strokeWidth="1" />
      <line x1="6" y1="17" x2="7" y2="17" stroke={color} strokeWidth="1" />
      <line x1="6" y1="19" x2="7" y2="19" stroke={color} strokeWidth="1" />
      <line x1="17" y1="15" x2="18" y2="15" stroke={color} strokeWidth="1" />
      <line x1="17" y1="17" x2="18" y2="17" stroke={color} strokeWidth="1" />
      <line x1="17" y1="19" x2="18" y2="19" stroke={color} strokeWidth="1" />
      <text x="12" y="18" textAnchor="middle" fontSize="4" fontWeight="bold" fill={color}>PLC</text>
    </svg>
  );
}

// Icon component type for rendering
type IconComponent = React.FC<{ size?: number; color?: string }>;

// Lucide icons wrapped as IconComponent
const LucideShield: IconComponent = ({ size, color }) => <Shield size={size} color={color} />;
const LucideServer: IconComponent = ({ size, color }) => <Server size={size} color={color} />;
const LucideMonitor: IconComponent = ({ size, color }) => <Monitor size={size} color={color} />;
const LucideDatabase: IconComponent = ({ size, color }) => <Database size={size} color={color} />;
const LucideShieldAlert: IconComponent = ({ size, color }) => <ShieldAlert size={size} color={color} />;
const LucideSatellite: IconComponent = ({ size, color }) => <Satellite size={size} color={color} />;
const LucideClock: IconComponent = ({ size, color }) => <Clock size={size} color={color} />;

// Icon mapping for node types
const nodeTypeIcons: Record<string, IconComponent> = {
  // Firewall — keep as shield
  containd_ngfw: LucideShield,
  opnsense_external: LucideShield,

  // Stub/fake Go services — Docker whale
  relay_sim: DockerIcon,
  recloser_sim: DockerIcon,
  regulator_sim: DockerIcon,
  capbank_sim: DockerIcon,
  rtac_sim: DockerIcon,

  // OT infrastructure sims — distinctive icons
  historian_sim: LucideDatabase,
  gps_sim: LucideSatellite,

  // Full Linux containers — Tux penguin
  corp_workstation: TuxIcon,
  vendor_jumpbox: TuxIcon,
  eng_workstation: TuxIcon,
  ews: TuxIcon,
  jump_host: TuxIcon,
  ubuntu_jumpbox: TuxIcon,

  // FUXA HMI — monitor (real SCADA application)
  fuxa_hmi: LucideMonitor,
  hmi_view: LucideMonitor,
  hmi_control: LucideMonitor,
  hmi_scada: LucideMonitor,

  // Kali
  kali_pentest: KaliIcon,

  // OpenPLC
  openplc: OpenPLCIcon,
  plc_trainer: OpenPLCIcon,
  sis_plc: OpenPLCIcon,

  // Suricata / IDS
  ot_ids: LucideShieldAlert,

  // InfluxDB historian
  historian: LucideDatabase,

  // GPS / time server
  gps_clock: LucideClock,

  // Default
  default: LucideServer,
};

// Zone colors - substation segmentation lab + legacy zone names
const zoneColors: Record<string, { border: string; bg: string; text: string }> = {
  // Substation segmentation zones
  enterprise_net: { border: "#38bdf8", bg: "rgba(56, 189, 248, 0.1)", text: "#38bdf8" },  // sky blue
  vendor_net:     { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },  // purple
  ot_ops_net:     { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },  // orange
  field_net:      { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },   // green
  // Containd zone names (from firewall config)
  wan:        { border: "#38bdf8", bg: "rgba(56, 189, 248, 0.1)", text: "#38bdf8" },
  dmz:        { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },
  lan1:       { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  lan2:       { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
  ot_control: { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  ot_safety:  { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
  // Legacy oil-plant zone names
  it_net:          { border: "#38bdf8", bg: "rgba(56, 189, 248, 0.1)", text: "#38bdf8" },
  dmz_net:         { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },
  ot_control_net:  { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  ot_safety_net:   { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
};

const defaultColors = { border: "#64748b", bg: "rgba(100, 116, 139, 0.1)", text: "#64748b" };

// Status indicator colors
const statusColors: Record<string, string> = {
  running: "#22c55e",
  stopped: "#ef4444",
  pending: "#eab308",
  unknown: "#64748b",
};

type HostNodeData = {
  label: string;
  nodeType: string;
  zone: string;
  status?: string;
  ip?: string;
  networks?: string[];
  ui_path?: string;
};

// Custom Host Node - represents a container/server
export const HostNode = memo(({ data, selected }: NodeProps<HostNodeData>) => {
  const colors = zoneColors[data.zone] || defaultColors;
  const Icon = nodeTypeIcons[data.nodeType] || nodeTypeIcons.default;
  const statusColor = statusColors[data.status || "unknown"];

  return (
    <div
      className={`relative flex flex-col items-center transition-all ${
        selected ? "scale-105" : ""
      }`}
      style={{ minWidth: 120 }}
    >
      {/* Connection handles */}
      <Handle
        type="target"
        position={Position.Top}
        className="!bg-slate-600 !border-slate-500"
        style={{ width: 8, height: 8 }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        className="!bg-slate-600 !border-slate-500"
        style={{ width: 8, height: 8 }}
      />

      {/* Status indicator */}
      <div
        className="absolute -top-1 -right-1 h-3 w-3 rounded-full border-2 border-slate-900"
        style={{ backgroundColor: statusColor }}
      />

      {/* Icon container */}
      <div
        className="flex h-14 w-14 items-center justify-center rounded-xl border-2 shadow-lg"
        style={{
          borderColor: colors.border,
          backgroundColor: colors.bg,
          boxShadow: selected ? `0 0 20px ${colors.border}40` : "0 4px 12px rgba(0,0,0,0.3)",
        }}
      >
        <Icon size={28} color={colors.text} />
      </div>

      {/* Label */}
      <div className="mt-2 text-center">
        <div
          className="text-xs font-semibold leading-tight"
          style={{ color: "#e2e8f0", maxWidth: 100 }}
        >
          {data.label}
        </div>
        {data.ip && (
          <div className="mt-0.5 text-[10px] text-slate-500 font-mono">{data.ip}</div>
        )}
      </div>
    </div>
  );
});

HostNode.displayName = "HostNode";

// Firewall Node - special styling for the NGFW
export const FirewallNode = memo(({ data, selected }: NodeProps<HostNodeData>) => {
  const statusColor = statusColors[data.status || "unknown"];

  return (
    <div
      className={`relative flex flex-col items-center transition-all ${
        selected ? "scale-105" : ""
      }`}
      style={{ minWidth: 140 }}
    >
      {/* Multiple handles for zone connections */}
      <Handle
        type="target"
        position={Position.Top}
        id="top"
        className="!bg-amber-500 !border-amber-400"
        style={{ width: 10, height: 10 }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        id="bottom"
        className="!bg-amber-500 !border-amber-400"
        style={{ width: 10, height: 10 }}
      />
      <Handle
        type="source"
        position={Position.Left}
        id="left"
        className="!bg-amber-500 !border-amber-400"
        style={{ width: 10, height: 10 }}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="right"
        className="!bg-amber-500 !border-amber-400"
        style={{ width: 10, height: 10 }}
      />

      {/* Status indicator */}
      <div
        className="absolute -top-1 -right-1 h-3 w-3 rounded-full border-2 border-slate-900 z-10"
        style={{ backgroundColor: statusColor }}
      />

      {/* Firewall icon container - hexagonal feel */}
      <div
        className="relative flex h-16 w-16 items-center justify-center rounded-lg border-2 shadow-xl"
        style={{
          borderColor: "#f59e0b",
          background: "linear-gradient(135deg, rgba(245, 158, 11, 0.2) 0%, rgba(217, 119, 6, 0.1) 100%)",
          boxShadow: selected
            ? "0 0 25px rgba(245, 158, 11, 0.4)"
            : "0 4px 16px rgba(0,0,0,0.4)",
        }}
      >
        <Shield size={32} className="text-amber-500" />
        {/* Flame accent */}
        <div className="absolute -bottom-1 left-1/2 -translate-x-1/2">
          <div className="h-2 w-4 rounded-b bg-gradient-to-t from-orange-600 to-transparent" />
        </div>
      </div>

      {/* Label */}
      <div className="mt-2 text-center">
        <div className="text-xs font-bold text-amber-400 leading-tight">{data.label}</div>
        <div className="text-[10px] text-slate-500">NGFW</div>
      </div>
    </div>
  );
});

FirewallNode.displayName = "FirewallNode";

type ZoneNodeData = {
  label: string;
  zone: string;
  subnet?: string;
};

// Zone Group Node - represents a network zone
export const ZoneNode = memo(({ data }: NodeProps<ZoneNodeData>) => {
  const colors = zoneColors[data.zone] || defaultColors;

  return (
    <div className="flex flex-col items-center">
      <Handle
        type="target"
        position={Position.Top}
        className="!bg-slate-600 !border-slate-500"
        style={{ width: 8, height: 8 }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        className="!bg-slate-600 !border-slate-500"
        style={{ width: 8, height: 8 }}
      />
      <div
        className="flex h-12 w-12 items-center justify-center rounded-lg border-2"
        style={{
          borderColor: colors.border,
          backgroundColor: colors.bg,
          boxShadow: `0 0 15px ${colors.border}30`,
        }}
      >
        <Waypoints size={24} style={{ color: colors.text }} />
      </div>
      <div className="mt-1 text-center">
        <div className="text-[10px] font-semibold uppercase tracking-wide" style={{ color: colors.text }}>
          {data.label}
        </div>
        {data.subnet && (
          <div className="text-[9px] text-slate-500 font-mono">{data.subnet}</div>
        )}
      </div>
    </div>
  );
});

ZoneNode.displayName = "ZoneNode";

// Network Switch Node - optional for more complex topologies
export const SwitchNode = memo(({ data, selected }: NodeProps<{ label: string; zone: string }>) => {
  const colors = zoneColors[data.zone] || defaultColors;

  return (
    <div className="relative flex flex-col items-center">
      <Handle type="target" position={Position.Top} className="!bg-slate-600" />
      <Handle type="source" position={Position.Bottom} className="!bg-slate-600" />
      <Handle type="source" position={Position.Left} className="!bg-slate-600" />
      <Handle type="source" position={Position.Right} className="!bg-slate-600" />

      <div
        className="flex h-8 w-16 items-center justify-center rounded border"
        style={{
          borderColor: colors.border,
          backgroundColor: colors.bg,
        }}
      >
        <Network size={18} style={{ color: colors.text }} />
      </div>
      <div className="mt-1 text-[10px] text-slate-500">{data.label}</div>
    </div>
  );
});

SwitchNode.displayName = "SwitchNode";

// Export node types for React Flow
export const nodeTypes = {
  host: HostNode,
  firewall: FirewallNode,
  zone: ZoneNode,
  switch: SwitchNode,
};

// Export colors for edge styling
export { zoneColors };
