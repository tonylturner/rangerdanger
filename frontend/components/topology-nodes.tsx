"use client";

import { memo } from "react";
import { Handle, Position, NodeProps } from "reactflow";
import {
  Shield,
  Server,
  Monitor,
  Cpu,
  Database,
  LineChart,
  Radio,
  Laptop,
  Wrench,
  Network,
  Cloud,
  ShieldAlert,
  Waypoints,
  type LucideIcon,
} from "lucide-react";

// Icon mapping for node types
const nodeTypeIcons: Record<string, LucideIcon> = {
  containd_ngfw: Shield,
  ews: Laptop,
  jump_host: Wrench,
  hmi_scada: Monitor,
  plc_trainer: Cpu,
  sis_plc: ShieldAlert,
  ot_ids: Radio,
  historian: Database,
  grafana: LineChart,
  opnsense_external: Shield,
  default: Server,
};

// Zone colors - both new and legacy zone names
const zoneColors: Record<string, { border: string; bg: string; text: string }> = {
  // New zone names
  wan: { border: "#f59e0b", bg: "rgba(245, 158, 11, 0.1)", text: "#f59e0b" },
  dmz: { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },
  ot_control: { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  ot_safety: { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
  it_workstations: { border: "#22d3ee", bg: "rgba(34, 211, 238, 0.1)", text: "#22d3ee" },
  // Legacy zone names
  it_net: { border: "#22d3ee", bg: "rgba(34, 211, 238, 0.1)", text: "#22d3ee" },
  dmz_net: { border: "#a855f7", bg: "rgba(168, 85, 247, 0.1)", text: "#a855f7" },
  ot_control_net: { border: "#f97316", bg: "rgba(249, 115, 22, 0.1)", text: "#f97316" },
  ot_safety_net: { border: "#22c55e", bg: "rgba(34, 197, 94, 0.1)", text: "#22c55e" },
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
        <Icon size={28} style={{ color: colors.text }} />
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
