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

// Kali Linux dragon silhouette — inline SVG so `color` prop flows
// through as `fill` via currentColor. Paths extracted from the
// official Kali dragon icon (commons.wikimedia.org).
function KaliIcon({ size = 28, color = "currentColor" }: { size?: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 512 512" fill={color} xmlns="http://www.w3.org/2000/svg">
      <path d="m69.929 127.71c-29.464.0337-48.949 2.4206-48.949 2.4206s107.73-5.1706 194.03 29.769c2.9338 13.122 11.759 34.976 16.513 45.481-13.602 9.4041-28.94 18.249-31.328 49.612-2.3877 31.362 24.576 58.947 58.013 59.798 31.751 1.6913 53.688 1.9323 80.277 15.718 25.38 14.034 46.192 56.794 48.251 95.252 2.2244-28.534-8.4845-89.895-58.453-108.53 69.826 12.219 75.982 63.973 75.984 63.994 0 0 5.2056-59.754-88.677-73.531-11.899-1.7458-26.947-3.0899-42.168-3.1283-75.246.99487-77.989-86.786-21.288-91.218 23.499-1.9389 51.556 10.738 78.986 23.512-.10182 3.4068.0404 6.4339 2.2787 9.2314 2.2375 2.7966 10.832 5.8474 13.579 7.4233 2.7464 1.5764 11.545 7.1715 16.937 14.189 1.1692-2.1872 10.933-8.5432 10.933-8.5432s-2.3391.0507-7.781-1.9832c-5.4426-2.0339-11.9-8.1876-12.053-8.5432-.15246-.35625-.25501-.91601 1.0169-1.1705.96572-.81305-1.2209-3.4575-2.1873-4.4232-.96572-.96636-7.4247-11.95-7.5769-12.204-.15245-.25447-.20376-.50931-.66105-.81465-1.4244-.45805-7.6799.66106-7.6799.66106s-9.6211-4.7252-12.937-14.913c.0482 1.7844-1.6497 3.734 0 7.8335-5.0162-2.122-9.325-5.7416-12.723-14.685-2.0227 5.0868 0 8.3215 0 8.3215s-11.811-3.3016-13.701-14.195c-2.0742 4.8902 0 7.8316 0 7.8316s-19.258-10.048-51.255-10.194c-21.422-1.965-25.882-39.65-23.897-45.994 0 0-30.899-16.285-91.723-23.479-22.809-2.6982-44.081-3.5238-61.76-3.5036zm71.276 32.521c-9.2697.0699-19.22.41577-29.674 1.1588-55.751 3.9628-112.04 23.477-112.04 23.477s115.18-28.98 211.77-11.971h.002l-2.512-8.1057s-27.373-4.5594-67.542-4.5594zm68.293 17.263c-9.75-.0412-40.02 1.1848-85.564 15.436-57.124 17.874-89.522 43.21-89.522 43.21s85.141-47.539 181.24-50.24l-2.7492-8.3176s-1.1564-.0779-3.4064-.0875zm62.559-.13932c26.38 10.723 49.34 24.931 67.165 44.514l.002.002 4.2794-.27998s-24.518-29.746-71.446-44.236zm74.662 61.731c.85796.17286 2.4779 3.3567 3.9644 5.6306.13054.18477.26356.38112.38886.53662.002.0138.003.0234.006.037.16796.23967.33693.4887.49774.68826.0812.47558.21641.76558-.89631.53663-.0936-.48886-.25471-.62995-.25471-.62995s-2.686-1.5982-3.5094-2.7317c-.82408-1.1335-.96948-3.1153-.56774-3.8672.0978-.16967.2236-.23004.37137-.20027z" />
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
  // IEC 62443 conduit assessment (firewall node only, set when
  // the 62443 layout is active).
  conduitSL?: number;
  targetSL?: number;
  conduitMet?: boolean;
};

// Hidden handle style — React Flow needs the handles in the DOM to
// attach edges and measure their bounding rects for edge routing.
// 8×8 keeps them large enough for React Flow to measure reliably
// (1×1 caused side handles to be undetectable, falling back to
// top/bottom defaults) while opacity:0 hides them visually.
const HIDDEN_HANDLE: React.CSSProperties = {
  width: 8,
  height: 8,
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
      {/* Icon container — circular Tron-style node. Thick zone-tinted
          ring + inner glow + outer halo so each host reads like a
          live disc on the grid. The 56×56 box doubles as the React
          Flow edge anchor so conduits land right on the disc edge. */}
      <div
        className="relative flex h-14 w-14 items-center justify-center rounded-full"
        style={{
          borderColor: colors.border,
          borderWidth: 3.5,
          borderStyle: "solid",
          backgroundColor: colors.bg,
          boxShadow: selected
            ? `0 0 22px ${colors.border}cc, 0 0 44px ${colors.border}66, inset 0 0 14px ${colors.border}55`
            : `0 0 14px ${colors.border}99, 0 0 28px ${colors.border}44, inset 0 0 10px ${colors.border}33`,
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
      style={{ minWidth: 160 }}
    >
      {/* Firewall icon container — Master Control disc.
          Circular Tron-style node, ~28% larger than host discs so
          it visually anchors the entire grid. Red warning ring on
          the outside (control plane = critical), warm amber core
          on the inside (the system the firewall protects). Every
          conduit on the map converges here. */}
      <div
        className="relative flex h-[88px] w-[88px] items-center justify-center rounded-full"
        style={{
          borderColor: "#ef4444",
          borderWidth: 5,
          borderStyle: "solid",
          background:
            "radial-gradient(circle at 50% 50%, rgba(245, 158, 11, 0.42) 0%, rgba(217, 119, 6, 0.18) 55%, rgba(15, 23, 42, 0.5) 100%)",
          boxShadow: selected
            ? "0 0 44px rgba(239, 68, 68, 0.9), 0 0 88px rgba(239, 68, 68, 0.45), inset 0 0 22px rgba(245, 158, 11, 0.65)"
            : "0 0 30px rgba(239, 68, 68, 0.75), 0 0 68px rgba(239, 68, 68, 0.32), inset 0 0 18px rgba(245, 158, 11, 0.45)",
        }}
      >
        {/* Connection handles — invisible, pinned to the icon box edges.
            The left handle is type="target" so 62443-view conduit edges
            (zone→firewall) can attach there. Default-view edges use
            "bottom" (source) for firewall→zone. */}
        <Handle type="target" position={Position.Top} id="top" style={HIDDEN_HANDLE} />
        <Handle type="source" position={Position.Bottom} id="bottom" style={HIDDEN_HANDLE} />
        <Handle type="target" position={Position.Left} id="left" style={HIDDEN_HANDLE} />
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

        {/* Substation vector icon — represents the lab system the
            firewall guards. Transparent variant so the radial amber
            fill behind it shows through. drop-shadow gives the icon
            its own glow on top of the container's halo. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/substation-icon-transparent.svg"
          alt=""
          className="h-16 w-16"
          style={{
            filter:
              "drop-shadow(0 0 6px rgba(252, 211, 77, 0.6)) drop-shadow(0 0 12px rgba(245, 158, 11, 0.4))",
          }}
        />
      </div>

      {/* Label */}
      <div className="mt-2 text-center">
        <div className="text-xs font-bold text-amber-400 leading-tight">{data.label}</div>
        <div className="text-[10px] text-slate-500">NGFW</div>
      </div>

      {/* IEC 62443 conduit SL assessment — only rendered when the
          62443 layout passes conduitSL/targetSL in the data. Shows
          whether the conduit meets the required SL of the zones it
          connects. Green = met, red = gap. Tooltip explains why. */}
      {data.conduitSL != null && data.targetSL != null && (
        <Tooltip>
          <TooltipTrigger asChild>
            <div
              className={`mt-2 cursor-help rounded border px-2 py-1 text-center text-[9px] font-bold uppercase tracking-wider ${
                data.conduitMet
                  ? "border-green-700/60 bg-green-950/30 text-green-400"
                  : "border-red-700/60 bg-red-950/30 text-red-400"
              }`}
            >
              <div>Target SL-{data.targetSL}</div>
              <div className="mt-0.5">
                {data.conduitMet ? "MET" : "GAP"} · SL-{data.conduitSL}
              </div>
            </div>
          </TooltipTrigger>
          <TooltipContent className="max-w-[280px]">
            {data.conduitMet ? (
              <>
                <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-green-300">
                  SL-{data.targetSL} Target Met
                </div>
                <div className="text-[10px] leading-snug">
                  Conduit achieves SL-{data.conduitSL}: deny-default policy,
                  protocol-aware DPI (Modbus/DNP3 function code filtering),
                  source-IP pinning to RTAC only. Enterprise and vendor zones
                  blocked from direct field device access.
                </div>
              </>
            ) : (
              <>
                <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-red-300">
                  SL-{data.targetSL} Target Not Met
                </div>
                <div className="text-[10px] leading-snug">
                  Conduit only achieves SL-{data.conduitSL}: all zones can
                  communicate freely, no DPI, no source restriction. The weak
                  baseline allows enterprise→field and vendor→field paths that
                  should be blocked. Apply the hardened policy to reach SL-{data.targetSL}.
                </div>
              </>
            )}
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  );
});

FirewallNode.displayName = "FirewallNode";

type ZoneNodeData = {
  label: string;
  zone: string;
  subnet?: string;
};

// Zone Group Node — a 1×1 invisible anchor used only as an edge
// target/source so conduits have a fixed connection point. Handles
// on all four sides so the same anchor works for vertical conduits
// (default view) and horizontal conduits (IEC 62443 view).
export const ZoneNode = memo(() => {
  return (
    <div style={{ width: 1, height: 1, opacity: 0 }}>
      <Handle type="target" position={Position.Top} style={HIDDEN_HANDLE} />
      <Handle type="source" position={Position.Bottom} style={HIDDEN_HANDLE} />
      <Handle id="right" type="source" position={Position.Right} style={HIDDEN_HANDLE} />
      <Handle id="left" type="target" position={Position.Left} style={HIDDEN_HANDLE} />
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
  securityLevel?: number; // IEC 62443 SL (0-4), shown when present
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
            fontSize: 11,
            fontWeight: 800,
            letterSpacing: "0.22em",
            textTransform: "uppercase",
            color: colors.text,
            opacity: 1,
            textShadow: `0 0 10px ${colors.border}99`,
          }}
        >
          {data.label}
        </div>
        {data.subnet && (
          <div
            style={{
              fontSize: 11,
              fontWeight: 600,
              fontFamily: "monospace",
              color: colors.text,
              opacity: 0.95,
              textShadow: `0 0 8px ${colors.border}88`,
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
