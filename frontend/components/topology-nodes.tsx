"use client";

import { memo } from "react";
import { Handle, Position, NodeProps } from "reactflow";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import {
  Shield,
  Server,
  Network,
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

function UbuntuIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  // Ubuntu "Circle of Friends" logo
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="12" cy="12" r="10" stroke={color} strokeWidth="1.5" fill="none" />
      {/* Three "friends" circles at 120° intervals */}
      <circle cx="12" cy="4.5" r="2" fill={color} />
      <circle cx="5.5" cy="16" r="2" fill={color} />
      <circle cx="18.5" cy="16" r="2" fill={color} />
      {/* Connecting arcs represented as lines */}
      <path d="M13.7 6.2C15.8 7.4 17.2 9.6 17.2 12.1" stroke={color} strokeWidth="1.5" strokeLinecap="round" fill="none" />
      <path d="M7.5 14.5C6.8 13.2 6.5 11.7 6.8 10.2" stroke={color} strokeWidth="1.5" strokeLinecap="round" fill="none" />
      <path d="M16.8 17.2C15.5 18.2 13.8 18.8 12 18.8C10.8 18.8 9.6 18.5 8.5 17.9" stroke={color} strokeWidth="1.5" strokeLinecap="round" fill="none" />
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
  corp_workstation: UbuntuIcon,
  vendor_jumpbox: UbuntuIcon,
  eng_workstation: UbuntuIcon,
  ews: UbuntuIcon,
  jump_host: UbuntuIcon,
  ubuntu_jumpbox: UbuntuIcon,

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


type HostNodeData = {
  label: string;
  nodeType: string;
  zone: string;
  status?: string;
  ip?: string;
  networks?: string[];
  ui_path?: string;
  multiHomedZones?: string[];
  // Live health, derived in useStyledGraph from workshopStatus.
  // "ok" / "down" = real telemetry is available (renders a dot).
  // undefined = no probe for this node (no dot rendered).
  health?: "ok" | "down";
  healthSource?: string; // e.g. "RTAC device_comms[relay]"
};

// Hidden handle style — React Flow needs the handles in the DOM to
// attach edges, but they shouldn't be visually rendered as dots on
// every node. Width/height of 1 keeps them clickable for edge layout
// while making them effectively invisible.
const HIDDEN_HANDLE: React.CSSProperties = {
  width: 1,
  height: 1,
  opacity: 0,
  border: "none",
  background: "transparent",
};

// Custom Host Node - represents a container/server
export const HostNode = memo(({ data, selected }: NodeProps<HostNodeData>) => {
  const colors = zoneColors[data.zone] || defaultColors;
  const Icon = nodeTypeIcons[data.nodeType] || nodeTypeIcons.default;
  const multiHomed = (data.multiHomedZones?.length ?? 0) > 0;

  return (
    <div
      className={`flex flex-col items-center transition-all ${
        selected ? "scale-105" : ""
      }`}
      style={{ minWidth: 120 }}
    >
      {/* Icon container — wraps the icon, its corner badges, AND the
          React Flow handles so edges attach to the visible 56×56 icon
          edge instead of the 120px-wide outer flex container (which
          would put "right" handles ~30px past the icon). Side handles
          let traffic edges attach horizontally without colliding with
          the vertical zone-membership edges. */}
      <div
        className="relative flex h-14 w-14 items-center justify-center rounded-xl border-2 shadow-lg"
        style={{
          borderColor: colors.border,
          backgroundColor: colors.bg,
          boxShadow: selected ? `0 0 20px ${colors.border}40` : "0 4px 12px rgba(0,0,0,0.3)",
        }}
      >
        {/* Connection handles — invisible, pinned to the icon box
            edges so edge paths connect directly to the icon. */}
        <Handle type="target" position={Position.Top} style={HIDDEN_HANDLE} />
        <Handle type="source" position={Position.Bottom} style={HIDDEN_HANDLE} />
        <Handle id="left-source" type="source" position={Position.Left} style={HIDDEN_HANDLE} />
        <Handle id="left-target" type="target" position={Position.Left} style={HIDDEN_HANDLE} />
        <Handle id="right-source" type="source" position={Position.Right} style={HIDDEN_HANDLE} />
        <Handle id="right-target" type="target" position={Position.Right} style={HIDDEN_HANDLE} />

        <Icon size={28} color={colors.text} />

        {/* Live status dot — only rendered for nodes whose health
            comes from a real probe (set in useStyledGraph from
            workshopStatus). Hover the dot to see which API endpoint
            backs it. */}
        {data.health && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span
                className={`absolute -top-1 -right-1 h-3 w-3 cursor-help rounded-full border-2 border-slate-900 ${
                  data.health === "ok" ? "bg-green-500" : "bg-red-500"
                }`}
              />
            </TooltipTrigger>
            <TooltipContent side="right">
              <div className="text-[10px] font-bold uppercase tracking-wider">
                {data.health}
              </div>
              <div className="text-[10px] text-slate-400">
                source: {data.healthSource || "unknown"}
              </div>
            </TooltipContent>
          </Tooltip>
        )}

        {/* Multi-homed badge — shown only when this node has
            interfaces in more than one zone. */}
        {multiHomed && (
          <div
            className="absolute -top-2 -left-2 z-10 rounded-full border border-slate-900 bg-amber-500 px-1.5 py-[1px] text-[8px] font-bold uppercase tracking-wider text-slate-900 shadow"
            title={`Multi-homed: also has an interface in ${data.multiHomedZones!.join(", ")}. Cross-zone traffic still traverses the firewall — see inspector.`}
          >
            MULTI
          </div>
        )}
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

  return (
    <div
      className={`flex flex-col items-center transition-all ${
        selected ? "scale-105" : ""
      }`}
      style={{ minWidth: 140 }}
    >
      {/* Firewall icon container — wraps the icon AND the handles so
          edges attach to the visible 64×64 icon box, not the outer
          140px wide label column. The persistent multi-ring amber
          glow reinforces "this is the control plane" — every conduit
          on the map converges here. */}
      <div
        className="relative flex h-16 w-16 items-center justify-center rounded-lg border-2 shadow-xl"
        style={{
          borderColor: "#f59e0b",
          background: "linear-gradient(135deg, rgba(245, 158, 11, 0.25) 0%, rgba(217, 119, 6, 0.1) 100%)",
          boxShadow: selected
            ? "0 0 32px rgba(245, 158, 11, 0.55), 0 0 64px rgba(245, 158, 11, 0.25)"
            : "0 0 18px rgba(245, 158, 11, 0.45), 0 0 42px rgba(245, 158, 11, 0.18)",
        }}
      >
        {/* Connection handles — invisible, pinned to the icon box edges */}
        <Handle type="target" position={Position.Top} id="top" style={HIDDEN_HANDLE} />
        <Handle type="source" position={Position.Bottom} id="bottom" style={HIDDEN_HANDLE} />
        <Handle type="source" position={Position.Left} id="left" style={HIDDEN_HANDLE} />
        <Handle type="source" position={Position.Right} id="right" style={HIDDEN_HANDLE} />

        {/* Live status dot for the firewall, sourced from
            workshopStatus.firewall_online (containd health check). */}
        {data.health && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span
                className={`absolute -top-1 -right-1 z-10 h-3 w-3 cursor-help rounded-full border-2 border-slate-900 ${
                  data.health === "ok" ? "bg-green-500" : "bg-red-500"
                }`}
              />
            </TooltipTrigger>
            <TooltipContent side="right">
              <div className="text-[10px] font-bold uppercase tracking-wider">
                {data.health}
              </div>
              <div className="text-[10px] text-slate-400">
                source: {data.healthSource || "unknown"}
              </div>
            </TooltipContent>
          </Tooltip>
        )}

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

// Zone Group Node — now a 1×1 invisible anchor used only as an edge
// target so firewall→zone conduits have a fixed connection point
// inside the zone column. The visible zone label and tinted boundary
// are rendered by ZoneBoundaryNode.
export const ZoneNode = memo(() => {
  return (
    <div style={{ width: 1, height: 1, opacity: 0 }}>
      <Handle type="target" position={Position.Top} style={HIDDEN_HANDLE} />
      <Handle type="source" position={Position.Bottom} style={HIDDEN_HANDLE} />
    </div>
  );
});

ZoneNode.displayName = "ZoneNode";

// Zone Boundary - large translucent container that visually groups
// the hosts inside a zone. Renders BEHIND the host nodes (added
// first to the styled nodes array) and is non-interactive so clicks
// pass through to the actual hosts. The combination of soft tinted
// background + glowing outline + zone label at the top makes the
// segmentation grouping unmistakable without adding any heavy box.
type ZoneBoundaryData = {
  zone: string;
  label: string;
  subnet?: string;
  width: number;
  height: number;
};

export const ZoneBoundaryNode = memo(({ data }: NodeProps<ZoneBoundaryData>) => {
  const colors = zoneColors[data.zone] || defaultColors;
  return (
    <div
      style={{
        width: data.width,
        height: data.height,
        borderRadius: 18,
        border: `1px solid ${colors.border}55`,
        backgroundColor: `${colors.border}0d`,
        boxShadow: `inset 0 0 36px ${colors.border}1a, 0 0 18px ${colors.border}22`,
        pointerEvents: "none",
        position: "relative",
      }}
    >
      <div
        style={{
          position: "absolute",
          top: 10,
          left: 14,
          right: 14,
          display: "flex",
          alignItems: "baseline",
          justifyContent: "space-between",
          pointerEvents: "none",
        }}
      >
        <div
          style={{
            fontSize: 10,
            fontWeight: 800,
            letterSpacing: "0.2em",
            textTransform: "uppercase",
            color: colors.text,
            opacity: 0.9,
          }}
        >
          {data.label}
        </div>
        {data.subnet && (
          <div
            style={{
              fontSize: 9,
              fontFamily: "monospace",
              color: colors.text,
              opacity: 0.55,
            }}
          >
            {data.subnet}
          </div>
        )}
      </div>
    </div>
  );
});
ZoneBoundaryNode.displayName = "ZoneBoundaryNode";

// Network Switch Node - optional for more complex topologies
export const SwitchNode = memo(({ data, selected }: NodeProps<{ label: string; zone: string }>) => {
  const colors = zoneColors[data.zone] || defaultColors;

  return (
    <div className="relative flex flex-col items-center">
      <Handle type="target" position={Position.Top} style={HIDDEN_HANDLE} />
      <Handle type="source" position={Position.Bottom} style={HIDDEN_HANDLE} />
      <Handle type="source" position={Position.Left} style={HIDDEN_HANDLE} />
      <Handle type="source" position={Position.Right} style={HIDDEN_HANDLE} />

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
  zoneBoundary: ZoneBoundaryNode,
  switch: SwitchNode,
};

// Export colors for edge styling
export { zoneColors };
