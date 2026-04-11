"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  Edge,
  EdgeProps,
  getBezierPath,
  MiniMap,
  Node,
  NodeChange,
  ReactFlowInstance,
} from "reactflow";
import "reactflow/dist/style.css";
import { useQuery } from "@tanstack/react-query";
import {
  getWorkshopGraph,
  getWorkshopStatus,
  getFirewallRules,
  getTrafficStatus,
  LabGraph,
  GraphNode as ApiGraphNode,
  ZoneRuleSummary,
  TrafficStatus,
} from "../lib/api";
import { Button } from "./ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { NodeTerminal } from "./node-terminal";
import { SharedTerminalPanel, useTerminals } from "./terminal-context";
import { nodeTypes, zoneColors } from "./topology-nodes";
import { SegmentationView } from "./segmentation-view";
import { TrafficMatrixView } from "./traffic-matrix-view";
import { OBSERVED_FLOWS, resolveLiveness, type ObservedFlow } from "../lib/observed-flows";
import {
  Activity,
  ChevronLeft,
  ExternalLink,
  Maximize2,
  ScrollText,
  Shield,
  X,
} from "lucide-react";

// All supported zone names
const zones = [
  "enterprise_net", "vendor_net", "ot_ops_net", "field_net",
  // Legacy zone names
  "wan", "dmz", "ot_control", "ot_safety", "it_workstations",
  "it_net", "dmz_net", "ot_control_net", "ot_safety_net"
] as const;

// Map well-known TCP/UDP ports to the protocol students recognize from
// the workshop. The ICS-relevant ports (Modbus, DNP3) are first since
// they're the load-bearing labels in this lab.
const PORT_PROTOCOL: Record<string, string> = {
  "502": "Modbus",
  "20000": "DNP3",
  "1881": "FUXA",
  "8080": "HTTP",
  "8443": "HTTPS",
  "80": "HTTP",
  "443": "HTTPS",
  "22": "SSH",
  "23": "Telnet",
  "3389": "RDP",
  "445": "SMB",
  "123": "NTP",
  "53": "DNS",
};

function humanizeSummary(summary: string): string[] {
  // containd's summary string is "502, 20000, 8080" or "8080 +4 more".
  // Strip the "+N more" tail (we surface that in the tooltip) and look up
  // each port. Anything we don't recognize is shown as "tcp/<port>".
  const cleaned = summary.replace(/\s*\+\d+\s*more\s*$/i, "").trim();
  if (!cleaned) return [];
  return cleaned
    .split(/[,\s]+/)
    .filter(Boolean)
    .map((tok) => PORT_PROTOCOL[tok] || `tcp/${tok}`);
}

// Operational flows that occur entirely inside a zone and never traverse
// the firewall. The lab teaches that the firewall view is intentionally
// incomplete: HMI→RTAC and Historian→RTAC carry critical state but never
// hit the segmentation boundary, so they need host- and network-level
// protections within the zone instead of firewall policy.
//
// Source/target are node IDs as emitted by the workshop graph (e.g.
// `hmi-1`, `rtac-1`). Direction is meaningful — source initiates.
const INTRA_ZONE_FLOWS: Array<{
  source: string;
  target: string;
  zone: string;
  protocol: string;
  description: string;
}> = [
  {
    source: "hmi-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "Modbus 502",
    description: "HMI polls RTAC tag DB ~2s — invisible to firewall",
  },
  {
    source: "historian-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "HTTP 8080",
    description: "Historian reads RTAC state ~5s — invisible to firewall",
  },
  {
    source: "openplc-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "Modbus 502",
    description: "OpenPLC tags exposed to RTAC — intra-zone",
  },
];

// View modes that toggle overlays on the map without changing the
// underlying topology. Each toggle is independent so a student can
// stack policy + traffic to see "what's allowed AND what's flowing".
type ViewMode = {
  policyDim: boolean; // color edges by firewall action
  traffic: boolean;   // overlay observed host-to-host flows
};

// Portal-based tooltip rendered in document.body. Tooltips inside SVG
// foreignObjects inherit opacity, transform, and clipping from their
// SVG parents, which made our edge tooltips look washed-out. Rendering
// in a portal sidesteps every one of those quirks: the tooltip is a
// plain absolutely-positioned div outside the React Flow tree.
function MapTooltip({
  visible,
  x,
  y,
  children,
}: {
  visible: boolean;
  x: number;
  y: number;
  children: React.ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted || !visible) return null;
  return createPortal(
    <div
      className="pointer-events-none fixed z-[60] rounded-lg border border-slate-700 px-3 py-2 text-[11px] text-slate-100 shadow-2xl"
      style={{
        left: x + 14,
        top: y + 14,
        backgroundColor: "#020617",
        maxWidth: 320,
      }}
    >
      {children}
    </div>,
    document.body,
  );
}

// Custom edge component with tooltip for firewall policy details. The
// label uses a pill background so it stays readable on the dark canvas
// and an action-aware leading icon (✓ allow / ✕ deny / ⚠ mixed) so the
// student can scan policy state without reading the label.
function PolicyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
  markerEnd,
}: EdgeProps) {
  const [tip, setTip] = useState({ visible: false, x: 0, y: 0 });

  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  // Position label closer to target (75% along the path) to avoid overlap
  const labelX = sourceX + (targetX - sourceX) * 0.75;
  const labelY = sourceY + (targetY - sourceY) * 0.75;

  const label = data?.label || "";
  const color = data?.color || "#64748b";
  const action = data?.action || "ALLOW";
  const details = data?.details || [];
  const dimmed = data?.dimmed === true;

  const actionIcon = action === "DENY" ? "\u2715" : action === "MIXED" ? "\u26A0" : "\u2713";
  const iconColor =
    action === "DENY" ? "#ef4444" : action === "MIXED" ? "#f59e0b" : "#22c55e";

  const labelWidth = Math.max(78, label.length * 6 + 28);
  const dimOpacity = dimmed ? 0.45 : 1;
  const pulse = data?.pulse === true;

  const zoneMeta = data?.zoneMeta as
    | {
        label: string;
        iface: string;
        role: string;
        subnet: string;
        fwIp: string;
        description: string;
      }
    | undefined;
  const hasTooltip = (details && details.length > 0) || !!zoneMeta;

  // Hover handler used by both the edge label and the wide invisible
  // hit-path so a student can hover anywhere on the line and get the
  // tooltip — not just the label pill.
  const showTip = (e: React.MouseEvent) => {
    if (hasTooltip) setTip({ visible: true, x: e.clientX, y: e.clientY });
  };
  const moveTip = (e: React.MouseEvent) => {
    if (tip.visible) setTip({ visible: true, x: e.clientX, y: e.clientY });
  };
  const hideTip = () => setTip((t) => ({ ...t, visible: false }));

  return (
    <>
      {/* Edge path + label group: dim with the policy view */}
      <g style={{ opacity: dimOpacity }}>
        <path
          id={id}
          style={style}
          className={`react-flow__edge-path ${pulse ? "tron-pulse" : ""}`}
          d={edgePath}
        />
        {/* Wide transparent hit-path so the tooltip triggers anywhere
            along the line, not only over the small label pill. */}
        <path
          d={edgePath}
          fill="none"
          stroke="transparent"
          strokeWidth={20}
          onMouseEnter={showTip}
          onMouseMove={moveTip}
          onMouseLeave={hideTip}
          style={{ cursor: hasTooltip ? "help" : "default" }}
        />
        {label && (
          <g
            onMouseEnter={showTip}
            onMouseMove={moveTip}
            onMouseLeave={hideTip}
            style={{ cursor: hasTooltip ? "help" : "default" }}
          >
            <rect
              x={labelX - labelWidth / 2}
              y={labelY - 11}
              width={labelWidth}
              height={22}
              rx={11}
              fill="#0f172a"
              stroke={color}
              strokeWidth={1.2}
              strokeOpacity={0.7}
            />
            <text
              x={labelX}
              y={labelY + 4}
              textAnchor="middle"
              fontSize={10}
              fontWeight={600}
              fill={color}
            >
              <tspan fill={iconColor}>{actionIcon}{"  "}</tspan>
              {label}
            </text>
          </g>
        )}
      </g>
      <MapTooltip visible={tip.visible} x={tip.x} y={tip.y}>
        {zoneMeta && (
          <>
            <div className="mb-1 text-[10px] font-bold uppercase tracking-wider text-sky-300">
              {zoneMeta.label} Interface
            </div>
            <div className="mb-1.5 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[10px]">
              <span className="text-slate-500">Iface</span>
              <span className="font-mono text-slate-200">
                {zoneMeta.iface} <span className="text-slate-500">({zoneMeta.role})</span>
              </span>
              <span className="text-slate-500">Subnet</span>
              <span className="font-mono text-slate-200">{zoneMeta.subnet}</span>
              <span className="text-slate-500">FW IP</span>
              <span className="font-mono text-slate-200">{zoneMeta.fwIp}</span>
            </div>
            <div className="mb-2 text-[10px] leading-snug text-slate-400">
              {zoneMeta.description}
            </div>
          </>
        )}
        {details.length > 0 && (
          <>
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
              Active Rules ({details.length})
            </div>
            <ul className="space-y-1">
              {details.slice(0, 8).map((detail: string, i: number) => {
                const isAllow = detail.startsWith("ALLOW");
                const isDeny = detail.startsWith("DENY");
                return (
                  <li key={i} className="flex items-start gap-1.5 leading-tight">
                    <span
                      className={`shrink-0 text-[10px] font-bold ${
                        isDeny ? "text-red-400" : isAllow ? "text-green-400" : "text-slate-400"
                      }`}
                    >
                      {isDeny ? "\u2715" : isAllow ? "\u2713" : "\u2022"}
                    </span>
                    <span className="text-slate-200">
                      {detail.replace(/^(ALLOW|DENY)\s*/, "")}
                    </span>
                  </li>
                );
              })}
              {details.length > 8 && (
                <li className="pl-4 italic text-slate-500">
                  +{details.length - 8} more rules...
                </li>
              )}
            </ul>
          </>
        )}
      </MapTooltip>
    </>
  );
}

// Traffic edge — observed peer-to-peer flow between two host nodes.
// Bezier curve from the source handle to the target handle. The
// handles sit on the visible edge of the node so the path connects
// directly to the icon outline. A triangle at the target gives
// unambiguous direction; no source-side dot because it was drifting
// away from the node and reading as a loose artifact.
function TrafficEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  style,
}: EdgeProps) {
  const [tip, setTip] = useState({ visible: false, x: 0, y: 0 });

  const [edgePath, centerX, centerY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    curvature: 0.4,
  });
  const labelX = centerX;
  const labelY = centerY;

  const label = data?.label || "";
  const status: "active" | "idle" | "down" = data?.status || "idle";
  const tooltipLines: string[] = data?.tooltipLines || [];

  const isActive = status === "active";
  const isDown = status === "down";
  const stroke = isActive ? "#22d3ee" : isDown ? "#64748b" : "#64748b";
  const labelColor = isActive ? "#67e8f9" : "#94a3b8";
  const labelWidth = Math.max(56, label.length * 6 + 16);

  // Arrow angle approximated from the source→target chord. It's not
  // the exact bezier tangent at the endpoint, but it's within a few
  // degrees and reads cleanly at the scales this lab uses.
  const dx = targetX - sourceX;
  const dy = targetY - sourceY;
  const arrowSize = 8;
  const arrowAngle = Math.atan2(dy, dx);
  const a1x = targetX - arrowSize * Math.cos(arrowAngle - Math.PI / 6);
  const a1y = targetY - arrowSize * Math.sin(arrowAngle - Math.PI / 6);
  const a2x = targetX - arrowSize * Math.cos(arrowAngle + Math.PI / 6);
  const a2y = targetY - arrowSize * Math.sin(arrowAngle + Math.PI / 6);

  return (
    <>
      <g
        onMouseEnter={(e) =>
          tooltipLines.length > 0 && setTip({ visible: true, x: e.clientX, y: e.clientY })
        }
        onMouseMove={(e) =>
          tip.visible && setTip({ visible: true, x: e.clientX, y: e.clientY })
        }
        onMouseLeave={() => setTip((t) => ({ ...t, visible: false }))}
        style={{ cursor: "help" }}
      >
        <path
          id={id}
          d={edgePath}
          className="react-flow__edge-path"
          fill="none"
          style={{
            stroke,
            strokeWidth: isActive ? 2 : 1.3,
            strokeDasharray: isActive ? "6 4" : "3 4",
            opacity: isDown ? 0.45 : isActive ? 0.95 : 0.65,
            ...style,
          }}
        />
        {/* Direction triangle at the target end */}
        <polygon
          points={`${targetX},${targetY} ${a1x},${a1y} ${a2x},${a2y}`}
          fill={stroke}
          opacity={isDown ? 0.5 : isActive ? 1 : 0.8}
        />
        <rect
          x={labelX - labelWidth / 2}
          y={labelY - 9}
          width={labelWidth}
          height={18}
          rx={9}
          fill="#0f172a"
          stroke={stroke}
          strokeOpacity={0.8}
          strokeWidth={1}
        />
        <text
          x={labelX}
          y={labelY + 4}
          textAnchor="middle"
          fontSize={9}
          fontWeight={600}
          fill={labelColor}
        >
          {label}
        </text>
      </g>
      <MapTooltip visible={tip.visible} x={tip.x} y={tip.y}>
        <div className="mb-1 flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-wider text-cyan-400">
          <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: stroke }} />
          Observed flow · {status}
        </div>
        {tooltipLines.map((l, i) => (
          <div key={i} className="text-slate-200">{l}</div>
        ))}
      </MapTooltip>
    </>
  );
}

// Edge types registration
const edgeTypes = {
  policyEdge: PolicyEdge,
  trafficEdge: TrafficEdge,
};

export function NetworkConsole() {
  const [inspectorNode, setInspectorNode] = useState<Node | null>(null);
  const [iframeUrl, setIframeUrl] = useState<string | null>(null);
  const [showTerminal, setShowTerminal] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [showTerminalModal, setShowTerminalModal] = useState(false);
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});
  const [viewMode, setViewMode] = useState<ViewMode>({
    policyDim: true,
    // Traffic edges default on — they're governed by the toggle
    // inside the Traffic Matrix drawer now, not a separate toolbar
    // button. The canvas still only renders them when policyDim is
    // on (same condition that mounts the drawer).
    traffic: true,
  });
  // Rook glow flag — flips true for ~1.4s whenever the active
  // firewall config changes, then auto-clears. Skips the initial
  // load so we don't pulse on first paint.
  const [rookGlow, setRookGlow] = useState(false);
  // Left drawer menu. The strip is mounted whenever Policy view is
  // on; the strip shows two stacked icons (Segmentation, Traffic
  // Matrix). Clicking either expands the drawer to 420px showing
  // that drawer's content. activeDrawer === null = strip only.
  // State is lifted so the canvas can re-fit the React Flow viewport
  // when the drawer changes width.
  type DrawerId = "segmentation" | "traffic";
  const [activeDrawer, setActiveDrawer] = useState<DrawerId | null>(null);
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance | null>(null);
  // Shared terminal state — persists across page navigations
  const { openTerminals: activeTerminals, open: openSharedTerminal } = useTerminals();

  const {
    data: graph,
    isLoading: graphLoading,
    isError: graphIsError,
    error: graphError
  } = useQuery({
    queryKey: ["workshop", "graph"],
    queryFn: getWorkshopGraph,
  });

  // Validate workshop environment on render
  const { data: workshopStatus } = useQuery({
    queryKey: ["workshop", "status"],
    queryFn: getWorkshopStatus,
    refetchInterval: 10000,
  });

  // Pulse Rook briefly whenever the firewall config changes. Initial
  // load is skipped via the ref so we don't pulse on first paint.
  const lastFirewallConfigRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    const cfg = workshopStatus?.firewall_config;
    if (cfg && lastFirewallConfigRef.current && cfg !== lastFirewallConfigRef.current) {
      setRookGlow(true);
      const t = setTimeout(() => setRookGlow(false), 1500);
      return () => clearTimeout(t);
    }
    lastFirewallConfigRef.current = cfg;
  }, [workshopStatus?.firewall_config]);

  // Fetch firewall rules for dynamic edge labels — poll every 5s so
  // topology updates promptly after a config change in the segmentation tab.
  const { data: firewallRulesData } = useQuery({
    queryKey: ["firewall-rules"],
    queryFn: getFirewallRules,
    refetchInterval: 5000,
    staleTime: 2000,
  });

  // Traffic generator state — only polled when the Traffic view is on
  // so we don't waste cycles in the default policy view.
  const { data: trafficStatus } = useQuery({
    queryKey: ["traffic-status"],
    queryFn: getTrafficStatus,
    refetchInterval: viewMode.traffic ? 3000 : false,
    enabled: viewMode.traffic,
  });

  const { nodes: layoutNodes, edges } = useStyledGraph(
    graph,
    firewallRulesData?.summaries,
    viewMode,
    workshopStatus?.device_comms,
    workshopStatus?.rtac_online,
    trafficStatus,
    workshopStatus?.firewall_online,
  );

  // Apply saved positions to nodes (for drag persistence)
  const nodes = useMemo(() => {
    return layoutNodes.map((node) => {
      const savedPos = nodePositions[node.id];
      return savedPos ? { ...node, position: savedPos } : node;
    });
  }, [layoutNodes, nodePositions]);

  // Compute the visible space inside the React Flow container that
  // isn't covered by floating UI (segmentation drawer on the left,
  // minimap on the bottom-right, controls on the top-right, view-mode
  // toolbar on the top-left), then call setViewport so all nodes fit
  // inside that available rectangle. Re-runs whenever the drawer
  // changes size or whenever policy view is toggled (which controls
  // drawer visibility entirely).
  const drawerVisible = viewMode.policyDim;
  const drawerExpanded = drawerVisible && activeDrawer !== null;

  // If Policy view is turned off the drawer is unmounted; reset the
  // active tab so re-enabling Policy starts fresh at the strip.
  useEffect(() => {
    if (!viewMode.policyDim && activeDrawer !== null) {
      setActiveDrawer(null);
    }
  }, [viewMode.policyDim, activeDrawer]);
  useEffect(() => {
    if (!rfInstance || nodes.length === 0) return;
    // Wait one frame so React Flow has measured the container.
    const handle = requestAnimationFrame(() => {
      const container = document.querySelector(".react-flow") as HTMLElement | null;
      const cw = container?.clientWidth || 1200;
      const ch = container?.clientHeight || 700;

      // Drawer minimized = 36px strip + gap. Expanded = 420px + gap.
      // Hidden = no left reservation.
      const leftPad = drawerExpanded ? 440 : drawerVisible ? 56 : 24;
      const rightPad = 200;  // minimap + a small gap
      const topPad = 56;     // view-mode toolbar
      const bottomPad = 24;

      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      for (const n of nodes) {
        const w = (n.width as number | undefined) || 140;
        const h = (n.height as number | undefined) || 100;
        minX = Math.min(minX, n.position.x);
        minY = Math.min(minY, n.position.y);
        maxX = Math.max(maxX, n.position.x + w);
        maxY = Math.max(maxY, n.position.y + h);
      }
      if (!Number.isFinite(minX)) return;

      const nodeW = maxX - minX;
      const nodeH = maxY - minY;
      const availW = Math.max(120, cw - leftPad - rightPad);
      const availH = Math.max(120, ch - topPad - bottomPad);

      const zoom = Math.max(0.3, Math.min(1.2, Math.min(availW / nodeW, availH / nodeH)));

      // Center the node bounding box inside the available rectangle.
      const targetCenterX = leftPad + availW / 2;
      const targetCenterY = topPad + availH / 2;
      const nodeCenterX = (minX + maxX) / 2;
      const nodeCenterY = (minY + maxY) / 2;

      rfInstance.setViewport(
        {
          x: targetCenterX - nodeCenterX * zoom,
          y: targetCenterY - nodeCenterY * zoom,
          zoom,
        },
        { duration: 350 },
      );
    });
    return () => cancelAnimationFrame(handle);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rfInstance, drawerVisible, drawerExpanded, nodes.length]);

  // Handle node changes (dragging)
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    changes.forEach((change) => {
      if (change.type === "position" && change.position) {
        setNodePositions((prev) => ({
          ...prev,
          [change.id]: change.position!,
        }));
      }
    });
  }, []);

  // Handle node click - preserve terminal sessions across node switches
  const handleNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    if (node.type === "zone") return;
    if (inspectorNode?.id === node.id) return;

    setInspectorNode(node);
    setIframeUrl(null);
    // If the new node already has an active terminal, show it; otherwise show placeholder
    setShowTerminal(activeTerminals.has(node.id));
  }, [inspectorNode?.id, activeTerminals]);

  const errors = useMemo(() => {
    const list: string[] = [];
    if (graphIsError && graphError)
      list.push(`Failed to load topology: ${graphError instanceof Error ? graphError.message : "Unknown error"}`);
    return list;
  }, [graphIsError, graphError]);

  return (
    <div className="flex h-[calc(100vh-3rem)] flex-col space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {/* Rook — operator/control symbol. Sits next to the title
              as a small clean logo and pulses briefly when the active
              firewall config flips (rule change beat). */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src="/rook-quarter-turn-wink-transparent-web.png"
            alt=""
            className={`h-12 w-12 shrink-0 ${rookGlow ? "rook-pulse" : ""}`}
          />
          <div>
            <p className="text-xs uppercase tracking-[0.3em] text-slate-400">Network Map</p>
            <h1 className="text-3xl font-semibold text-white">Substation Network Map</h1>
            <p className="text-sm text-slate-400">Distribution co-op feeder topology. Click a node to inspect, open UI, or terminal.</p>
          </div>
        </div>
        {workshopStatus && (
          <div className="flex items-center gap-3 text-[10px]">
            <StatusDot
              ok={workshopStatus.firewall_online}
              label="containd"
              tooltipTitle="Segmentation enforcement"
              tooltipBody="containd NGFW. Mediates every cross-zone flow in the lab. Down = no cross-zone path is enforced."
            />
            <StatusDot
              ok={workshopStatus.rtac_online}
              label="RTAC"
              tooltipTitle="Supervisory control"
              tooltipBody="Real-Time Automation Controller. The only host that polls field devices and brokers HMI/historian state. Down = no supervisory loop."
            />
            <StatusDot
              ok={workshopStatus.device_comms ? Object.values(workshopStatus.device_comms).every(Boolean) : false}
              label={`Devices ${workshopStatus.device_comms ? Object.values(workshopStatus.device_comms).filter(Boolean).length : 0}/${workshopStatus.device_comms ? Object.keys(workshopStatus.device_comms).length : 0}`}
              tooltipTitle="Field protection layer"
              tooltipBody="Relay, recloser, regulator. RTAC polls these via Modbus and DNP3. A drop here means a poll path is broken — device down, network issue, or firewall rule change."
            />
            <PolicyBadge hardened={workshopStatus.firewall_config === "improved"} />
          </div>
        )}
      </header>

      {errors.length > 0 && (
        <div className="rounded-xl border border-red-800 bg-red-950/40 px-4 py-3 text-sm text-red-100">
          {errors.map((msg, idx) => (
            <p key={idx}>{msg}</p>
          ))}
        </div>
      )}

      <div className="relative flex-1 overflow-hidden rounded-2xl border border-slate-800 bg-slate-950">
        <ViewModeToolbar viewMode={viewMode} onChange={setViewMode} />
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onNodeClick={handleNodeClick}
          onInit={setRfInstance}
          fitView
          nodesDraggable
          minZoom={0.3}
          maxZoom={2}
          defaultViewport={{ x: 0, y: 0, zoom: 0.8 }}
          proOptions={{ hideAttribution: true }}
        >
          {/* Subtle Tron grid — two layers: a fine cyan dot mesh for
              texture, then a wider line grid for the segmentation
              feel. Both are very low opacity so they read as
              "structured background" not "decoration". */}
          <Background
            id="tron-dots"
            variant={BackgroundVariant.Dots}
            gap={20}
            size={1}
            color="#0e7490"
            style={{ opacity: 0.25 }}
          />
          <Background
            id="tron-grid"
            variant={BackgroundVariant.Lines}
            gap={120}
            lineWidth={0.5}
            color="#0c4a6e"
            style={{ opacity: 0.35 }}
          />
          <MiniMap
            pannable
            zoomable
            position="bottom-right"
            nodeColor={(node) => {
              const zone = node.data?.zone;
              return zoneColors[zone]?.border || "#64748b";
            }}
            maskColor="rgba(15, 23, 42, 0.8)"
            style={{ backgroundColor: "#0f172a", border: "1px solid #334155" }}
          />
          <Controls
            className="!bg-slate-800 !border-slate-700"
            position="top-right"
          />
        </ReactFlow>
        {viewMode.policyDim && (
          <LeftDrawerMenu
            active={activeDrawer}
            onChange={setActiveDrawer}
            trafficEdgesOn={viewMode.traffic}
            onToggleTrafficEdges={() =>
              setViewMode((v) => ({ ...v, traffic: !v.traffic }))
            }
          />
        )}
        {graphLoading && (
          <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-3 bg-slate-950/70 text-slate-200">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/rook-quarter-turn-wink-transparent-web.png"
              alt=""
              className="h-32 w-32 animate-pulse"
            />
            <div className="text-sm font-medium tracking-wide text-slate-300">
              Loading console…
            </div>
          </div>
        )}
      </div>

      {inspectorNode && (
        <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-md border-l border-slate-800 bg-slate-900/95 px-5 py-6 shadow-2xl backdrop-blur">
          <div className="flex items-center justify-between">
            <div>
              <p
                className="text-xs font-semibold uppercase tracking-[0.25em]"
                style={{ color: zoneColors[inspectorNode.data?.zone]?.text || "#94a3b8" }}
              >
                {inspectorNode.data?.zone}
              </p>
              <h3 className="text-xl font-semibold text-white">{inspectorNode.data?.label}</h3>
              <p className="text-sm text-slate-400">{inspectorNode.data?.nodeType || inspectorNode.type}</p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => { setInspectorNode(null); setIframeUrl(null); setShowTerminal(false); setShowTerminalModal(false); }}>
              Close
            </Button>
          </div>

          <div className="mt-4 space-y-3 text-sm text-slate-200">
            <InfoRow label="Type" value={inspectorNode.data?.nodeType || inspectorNode.type || "unknown"} />
            <InfoRow label="Status" value={inspectorNode.data?.status || "unknown"} />

            {/* Networks as chips */}
            <div className="flex items-start justify-between rounded-lg border border-slate-800 bg-slate-900 px-3 py-2">
              <span className="text-slate-400 shrink-0 mr-3">Networks</span>
              <div className="flex flex-wrap gap-1.5 justify-end">
                {(inspectorNode.data?.networks || []).length > 0 ? (
                  (inspectorNode.data?.networks || []).map((net: string) => (
                    <span
                      key={net}
                      className="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
                      style={{
                        backgroundColor: `${zoneColors[net]?.border || "#64748b"}20`,
                        color: zoneColors[net]?.text || "#94a3b8",
                        border: `1px solid ${zoneColors[net]?.border || "#64748b"}40`,
                      }}
                    >
                      {net.replace(/_net$/, "").replace(/_/g, " ")}
                    </span>
                  ))
                ) : (
                  <span className="text-slate-500">N/A</span>
                )}
              </div>
            </div>

            {/* IPs as chips with network tooltips */}
            <div className="flex items-start justify-between rounded-lg border border-slate-800 bg-slate-900 px-3 py-2">
              <span className="text-slate-400 shrink-0 mr-3">IPs</span>
              <div className="flex flex-wrap gap-1.5 justify-end">
                {inspectorNode.data?.interface_ips && typeof inspectorNode.data.interface_ips === 'object' && Object.keys(inspectorNode.data.interface_ips).length > 0 ? (
                  Object.entries(inspectorNode.data.interface_ips).map(([net, ip]) => (
                    <span
                      key={net}
                      className="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium cursor-help"
                      style={{
                        backgroundColor: `${zoneColors[net]?.border || "#64748b"}20`,
                        color: zoneColors[net]?.text || "#94a3b8",
                        border: `1px solid ${zoneColors[net]?.border || "#64748b"}40`,
                      }}
                      title={net.replace(/_net$/, "").replace(/_/g, " ")}
                    >
                      {String(ip)}
                    </span>
                  ))
                ) : inspectorNode.data?.ip ? (
                  <span
                    className="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium"
                    style={{
                      backgroundColor: `${zoneColors[inspectorNode.data?.zone || ""]?.border || "#64748b"}20`,
                      color: zoneColors[inspectorNode.data?.zone || ""]?.text || "#94a3b8",
                      border: `1px solid ${zoneColors[inspectorNode.data?.zone || ""]?.border || "#64748b"}40`,
                    }}
                  >
                    {inspectorNode.data.ip}
                  </span>
                ) : (
                  <span className="text-slate-500">N/A</span>
                )}
              </div>
            </div>

            <ActiveFlowsPanel
              nodeId={inspectorNode.id}
              nodeZone={inspectorNode.data?.zone}
              ruleSummaries={firewallRulesData?.summaries}
              multiHomedZones={inspectorNode.data?.multiHomedZones || []}
            />
          </div>

          <div className="mt-6 flex gap-3">
            <Button
              className="flex-1"
              variant={!showTerminal ? "default" : "outline"}
              disabled={!inspectorNode.data?.ui_path && !inspectorNode.data?.external_ui_url}
              onClick={() => {
                if (inspectorNode.data?.ui_path || inspectorNode.data?.external_ui_url) {
                  setIframeUrl(inspectorNode.data.ui_path || inspectorNode.data.external_ui_url);
                  setShowTerminal(false);
                }
              }}
            >
              {inspectorNode.data?.ui_path || inspectorNode.data?.external_ui_url ? "UI" : "No UI"}
            </Button>
            <Button
              className="flex-1"
              variant={showTerminal ? "default" : "outline"}
              onClick={() => {
                setShowTerminal(true);
                setIframeUrl(null);
                openSharedTerminal(inspectorNode.id);
              }}
            >
              Terminal
            </Button>
            {inspectorNode.data?.external_ui_url && (
              <Button
                variant="outline"
                className="px-3"
                onClick={() => window.open(inspectorNode.data.external_ui_url, "_blank")}
                title="Open in new tab"
              >
                <ExternalLink className="h-4 w-4" />
              </Button>
            )}
          </div>

          <div className="mt-6 h-[320px] overflow-hidden rounded-xl border border-slate-800 bg-black relative">
            {/* Shared terminals — state tracked across pages */}
            {showTerminal && (
              <SharedTerminalPanel
                nodes={[inspectorNode.id]}
                activeNode={inspectorNode.id}
                height={320}
              />
            )}
            {showTerminal && activeTerminals.has(inspectorNode.id) && (
              <Button
                variant="ghost"
                size="sm"
                className="absolute top-2 right-10 bg-slate-900/80 hover:bg-slate-800 z-20"
                onClick={() => setShowTerminalModal(true)}
                title="Expand terminal"
              >
                <Maximize2 className="h-4 w-4" />
              </Button>
            )}
            {!showTerminal && iframeUrl ? (
              <div className="relative h-full w-full overflow-hidden">
                {/* Scaled minimap view - iframe is rendered at 2x size and scaled down */}
                <div
                  className="absolute origin-top-left"
                  style={{
                    width: '200%',
                    height: '200%',
                    transform: 'scale(0.5)',
                  }}
                >
                  <iframe
                    title="Embedded UI"
                    src={iframeUrl}
                    className="border-0"
                    style={{ width: '100%', height: '100%' }}
                  />
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  className="absolute top-2 right-2 bg-slate-900/80 hover:bg-slate-800 z-20"
                  onClick={() => setShowModal(true)}
                  title="Expand to full size"
                >
                  <Maximize2 className="h-4 w-4" />
                </Button>
              </div>
            ) : !showTerminal && !iframeUrl ? (
              <div className="flex h-full items-center justify-center text-sm text-slate-500">
                Select UI or Terminal to view here.
              </div>
            ) : null}
          </div>
        </aside>
      )}

      {/* Expanded UI Modal */}
      {showModal && iframeUrl && (
        <div className="fixed inset-0 z-50 bg-black/90 flex flex-col">
          <div className="flex items-center justify-between px-4 py-3 bg-slate-900 border-b border-slate-800">
            <div className="flex items-center gap-3">
              <h3 className="text-white font-medium">{inspectorNode?.data?.label || "UI"}</h3>
              {inspectorNode?.data?.external_ui_url && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => window.open(inspectorNode.data.external_ui_url, "_blank")}
                  className="gap-1 text-slate-400 hover:text-white"
                >
                  <ExternalLink className="h-3 w-3" />
                  Open in new tab
                </Button>
              )}
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowModal(false)}
              className="text-slate-400 hover:text-white"
            >
              <X className="h-5 w-5" />
            </Button>
          </div>
          <div className="flex-1 overflow-hidden">
            <iframe title="Expanded UI" src={iframeUrl} className="h-full w-full border-0" />
          </div>
        </div>
      )}

      {/* Expanded Terminal Modal */}
      {showTerminalModal && inspectorNode && (
        <div className="fixed inset-0 z-50 bg-black/95 flex flex-col">
          <div className="flex items-center justify-between px-4 py-3 bg-slate-900 border-b border-slate-800">
            <div className="flex items-center gap-3">
              <h3 className="text-white font-medium">{inspectorNode?.data?.label || "Terminal"}</h3>
              <span className="text-xs text-slate-500">
                {inspectorNode?.data?.ip || inspectorNode?.id}
              </span>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowTerminalModal(false)}
              className="text-slate-400 hover:text-white"
            >
              <X className="h-5 w-5" />
            </Button>
          </div>
          <div className="flex-1 overflow-hidden p-4">
            <div className="h-full w-full rounded-lg border border-slate-700 overflow-hidden bg-slate-950">
              <NodeTerminal
                nodeId={inspectorNode.id}
                labId="workshop"
                expanded={true}
                hideHeader={true}
              />
            </div>
          </div>
        </div>
      )}

    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-slate-800 bg-slate-900 px-3 py-2">
      <span className="text-slate-400">{label}</span>
      <span className="text-slate-100">{value}</span>
    </div>
  );
}

function StatusDot({
  ok,
  label,
  tooltipTitle,
  tooltipBody,
}: {
  ok: boolean;
  label: string;
  tooltipTitle?: string;
  tooltipBody?: string;
}) {
  const dot = (
    <span className="flex cursor-help items-center gap-1.5 text-slate-400">
      <span className={`h-2 w-2 rounded-full ${ok ? "bg-green-500" : "bg-red-500"}`} />
      {label}
    </span>
  );
  if (!tooltipTitle && !tooltipBody) return dot;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{dot}</TooltipTrigger>
      <TooltipContent className="max-w-[260px]">
        {tooltipTitle && (
          <div className="mb-0.5 text-[10px] font-bold uppercase tracking-wider text-sky-300">
            {tooltipTitle}
          </div>
        )}
        {tooltipBody && <div className="text-[11px] leading-snug">{tooltipBody}</div>}
      </TooltipContent>
    </Tooltip>
  );
}

// Prominent policy state pill so the student can't miss whether they're
// looking at the weak baseline or the hardened post-remediation config.
// In the weak state we render the aggressive raccoon as a small visual
// alarm — it pairs with the red coloring to push the "this is dangerous"
// signal harder than text alone.
function PolicyBadge({ hardened }: { hardened: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[11px] font-bold uppercase tracking-wider ${
        hardened
          ? "border-green-600/60 bg-green-950/40 text-green-300"
          : "border-red-600/60 bg-red-950/40 text-red-300"
      }`}
      title={
        hardened
          ? "Hardened policy: only RTAC→field, vendor restricted to HMI HTTPS"
          : "Weak baseline: enterprise→OT and vendor→field allowed"
      }
    >
      {hardened ? (
        <Shield className="h-3 w-3" />
      ) : (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src="/rook-forward-aggressive-transparent-web.png"
          alt=""
          className="h-4 w-4"
        />
      )}
      Firewall · {hardened ? "Hardened" : "Weak baseline"}
    </span>
  );
}

// Left drawer menu attached to the left edge of the React Flow
// canvas. The strip is always 36px wide and shows a stack of icon
// buttons — one per registered drawer. Clicking any icon opens that
// drawer's content in a 420px panel that sits next to the strip.
// Clicking the header chevron collapses back to strip-only.
//
// Drawers mount whenever Policy view is on; both icons are always
// visible. The Traffic Matrix drawer owns the canvas "traffic edges"
// toggle as an internal header control, so the top toolbar no longer
// has a separate "Traffic view" button.
type DrawerKey = "segmentation" | "traffic";
const DRAWER_META: Record<
  DrawerKey,
  { label: string; Icon: typeof Shield; iconColor: string }
> = {
  segmentation: {
    label: "Segmentation Policy",
    Icon: Shield,
    iconColor: "text-amber-400",
  },
  traffic: {
    label: "Traffic Matrix",
    Icon: Activity,
    iconColor: "text-cyan-400",
  },
};
const DRAWER_ORDER: DrawerKey[] = ["segmentation", "traffic"];

function LeftDrawerMenu({
  active,
  onChange,
  trafficEdgesOn,
  onToggleTrafficEdges,
}: {
  active: DrawerKey | null;
  onChange: (id: DrawerKey | null) => void;
  trafficEdgesOn: boolean;
  onToggleTrafficEdges: () => void;
}) {
  const activeMeta = active ? DRAWER_META[active] : null;

  const renderContent = () => {
    if (active === "segmentation") return <SegmentationView />;
    if (active === "traffic") {
      return (
        <TrafficMatrixView
          canvasEdgesOn={trafficEdgesOn}
          onToggleCanvasEdges={onToggleTrafficEdges}
        />
      );
    }
    return null;
  };

  return (
    <>
      {/* Always-visible icon strip on the left edge */}
      <div className="absolute left-0 top-0 z-30 flex h-full w-9 flex-col items-center gap-1 border-r border-slate-700 bg-slate-900/95 py-3 backdrop-blur">
        {DRAWER_ORDER.map((id) => {
          const { label, Icon, iconColor } = DRAWER_META[id];
          const isActive = active === id;
          return (
            <Tooltip key={id}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => onChange(isActive ? null : id)}
                  aria-label={label}
                  className={`flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
                    isActive
                      ? "border-slate-600 bg-slate-800"
                      : "border-transparent hover:border-slate-700 hover:bg-slate-800"
                  }`}
                >
                  <Icon className={`h-4 w-4 ${iconColor}`} />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">{label}</TooltipContent>
            </Tooltip>
          );
        })}
      </div>

      {/* Expanded drawer panel sits flush against the right edge of
          the strip. Same dimensions and chrome regardless of which
          drawer is active. */}
      {activeMeta && (
        <aside className="absolute left-9 top-0 z-30 flex h-full w-[420px] flex-col border-r border-slate-700 bg-slate-950/95 shadow-2xl backdrop-blur">
          <div className="flex items-center justify-between border-b border-slate-800 px-3 py-2.5">
            <div className="flex items-center gap-2">
              <activeMeta.Icon className={`h-4 w-4 ${activeMeta.iconColor}`} />
              <span className="text-[11px] font-bold uppercase tracking-wider text-slate-200">
                {activeMeta.label}
              </span>
            </div>
            <button
              onClick={() => onChange(null)}
              title="Minimize"
              className="rounded p-1 text-slate-400 transition-colors hover:bg-slate-800 hover:text-white"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-3">{renderContent()}</div>
        </aside>
      )}
    </>
  );
}

// Floating toolbar above the React Flow canvas. Each toggle is an
// icon-only button (matching the exercise page header style) so the
// strip stays compact even when the segmentation drawer is open.
// Position is shifted past the drawer's collapsed strip so the "VIEW"
// label is never covered.
function ViewModeToolbar({
  viewMode,
  onChange,
}: {
  viewMode: ViewMode;
  onChange: (m: ViewMode) => void;
}) {
  const IconToggle = ({
    on,
    onClick,
    label,
    Icon,
    activeBorder,
    activeBg,
    activeText,
  }: {
    on: boolean;
    onClick: () => void;
    label: string;
    Icon: React.ComponentType<{ className?: string }>;
    activeBorder: string;
    activeBg: string;
    activeText: string;
  }) => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={onClick}
          aria-label={label}
          className={`inline-flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
            on
              ? `${activeBorder} ${activeBg} ${activeText}`
              : "border-slate-800 bg-slate-900/70 text-slate-500 hover:border-slate-700 hover:text-slate-300"
          }`}
        >
          <Icon className="h-3.5 w-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );

  return (
    <div className="absolute left-14 top-3 z-20 flex items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/85 px-2.5 py-1.5 backdrop-blur">
      <span className="pl-0.5 pr-1 text-[9px] font-semibold uppercase tracking-[0.2em] text-slate-500">
        View
      </span>
      <IconToggle
        on={viewMode.policyDim}
        onClick={() => onChange({ ...viewMode, policyDim: !viewMode.policyDim })}
        label="Policy"
        Icon={ScrollText}
        activeBorder="border-green-700/60"
        activeBg="bg-green-950/30"
        activeText="text-green-400"
      />
    </div>
  );
}

// Map containd's internal zone names back to a human-friendly label.
// Used by the inspector so the student doesn't see "lan1 → lan2" but
// "OT Ops → Field Devices".
const ZONE_HUMAN_NAME: Record<string, string> = {
  enterprise_net: "Enterprise",
  vendor_net: "Vendor / DMZ",
  ot_ops_net: "OT Ops",
  field_net: "Field Devices",
  wan: "Enterprise",
  dmz: "Vendor / DMZ",
  lan1: "OT Ops",
  lan2: "Field Devices",
  any: "any zone",
};

function humanZoneName(z: string): string {
  return ZONE_HUMAN_NAME[z] || z;
}

// Right-panel section showing what flows the selected node participates
// in. Two distinct sections so the student can see the teaching point
// at a glance:
//
//   • Intra-zone (firewall-blind): hard-coded peer flows like HMI→RTAC.
//     Important context that the firewall view is incomplete.
//
//   • Cross-zone (firewall-governed): rules from containd, framed as
//     "outbound from this zone" or "inbound to this zone" so the
//     student can correlate the rule list to the selected node.
//
// Note: containd policy is enforced at the zone level, not per host
// IP. We say so explicitly so students don't think a per-node ACL is
// being shown.
function ActiveFlowsPanel({
  nodeId,
  nodeZone,
  ruleSummaries,
  multiHomedZones,
}: {
  nodeId: string;
  nodeZone?: string;
  ruleSummaries?: ZoneRuleSummary[];
  multiHomedZones: string[];
}) {
  // Cross-zone rules where this node's zone is either source or dest.
  // We split into outbound (this zone is the source) and inbound (this
  // zone is the destination) so the user sees the rule from the
  // selected node's perspective.
  const { outbound, inbound } = useMemo(() => {
    if (!ruleSummaries || !nodeZone) return { outbound: [], inbound: [] };
    const containdZones = Object.entries(containdZoneToNetwork)
      .filter(([_, networks]) => networks.includes(nodeZone))
      .map(([cz]) => cz);
    const out: ZoneRuleSummary[] = [];
    const inb: ZoneRuleSummary[] = [];
    ruleSummaries.forEach((r) => {
      if (r.source_zone === r.dest_zone) return;
      const isSrc = containdZones.includes(r.source_zone);
      const isDst = containdZones.includes(r.dest_zone);
      if (isSrc && !isDst) out.push(r);
      else if (isDst && !isSrc) inb.push(r);
    });
    return { outbound: out, inbound: inb };
  }, [ruleSummaries, nodeZone]);

  // Intra-zone flows that name this node as source or target.
  const intraFlows = useMemo(() => {
    return INTRA_ZONE_FLOWS.filter((f) => f.source === nodeId || f.target === nodeId);
  }, [nodeId]);

  const hasContent =
    outbound.length > 0 ||
    inbound.length > 0 ||
    intraFlows.length > 0 ||
    multiHomedZones.length > 0;
  if (!hasContent) return null;

  const renderRule = (
    r: ZoneRuleSummary,
    direction: "out" | "in",
    key: number,
  ) => {
    const peerZone = direction === "out" ? r.dest_zone : r.source_zone;
    const protos = humanizeSummary(r.summary);
    const protoLabel =
      protos.length === 0
        ? r.summary || "all"
        : protos.length <= 3
        ? protos.join(" / ")
        : `${protos.length} flows`;
    const actionColor =
      r.action === "DENY"
        ? "text-red-400"
        : r.action === "MIXED"
        ? "text-amber-400"
        : "text-green-400";
    const arrow = direction === "out" ? "→" : "←";
    return (
      <li key={key} className="leading-tight">
        <div className="flex items-baseline gap-1.5 text-[11px]">
          <span className={`font-bold ${actionColor}`}>
            {r.action === "DENY" ? "✕" : r.action === "MIXED" ? "⚠" : "✓"}
          </span>
          <span className="font-mono text-slate-500">{arrow}</span>
          <span className="text-slate-200">{humanZoneName(peerZone)}</span>
          <span className="text-slate-500">·</span>
          <span className="text-slate-300">{protoLabel}</span>
        </div>
      </li>
    );
  };

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 px-3 py-2.5">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
        Communications
      </div>

      {multiHomedZones.length > 0 && (
        <div className="mb-2 rounded border border-amber-700/40 bg-amber-950/20 px-2 py-1.5">
          <div className="text-[9px] font-bold uppercase tracking-wider text-amber-400">
            Multi-homed
          </div>
          <div className="mt-0.5 text-[11px] text-slate-200">
            Also has interface in{" "}
            {multiHomedZones.map((z) => (
              <span
                key={z}
                className="font-mono"
                style={{ color: zoneColors[z]?.text || "#94a3b8" }}
              >
                {z.replace(/_net$/, "")}{" "}
              </span>
            ))}
          </div>
          <div className="mt-1 text-[10px] text-slate-400">
            Cross-zone traffic still routes through the firewall.
          </div>
        </div>
      )}

      {intraFlows.length > 0 && (
        <div className="mb-3">
          <div className="mb-1 flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-wider text-amber-400">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
            Same zone · firewall-blind
          </div>
          <ul className="space-y-1">
            {intraFlows.map((f, i) => {
              const isSource = f.source === nodeId;
              const peer = isSource ? f.target : f.source;
              return (
                <li key={i} className="text-[11px] leading-tight">
                  <span className="font-mono text-slate-500">
                    {isSource ? "→" : "←"}
                  </span>{" "}
                  <span className="text-slate-200">{peer}</span>{" "}
                  <span className="text-slate-500">· {f.protocol}</span>
                </li>
              );
            })}
          </ul>
          <div className="mt-1 text-[10px] text-slate-500 italic">
            These flows never reach the firewall. Protect with host or
            network controls inside the zone.
          </div>
        </div>
      )}

      {(outbound.length > 0 || inbound.length > 0) && (
        <div>
          <div className="mb-1 flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-wider text-sky-400">
            <span className="h-1.5 w-1.5 rounded-full bg-sky-400" />
            Cross-zone · firewall-governed
          </div>
          {outbound.length > 0 && (
            <div className="mb-1.5">
              <div className="mb-0.5 text-[9px] uppercase tracking-wider text-slate-500">
                Outbound from {nodeZone ? humanZoneName(nodeZone) : "this zone"}
              </div>
              <ul className="space-y-1">
                {outbound.slice(0, 6).map((r, i) => renderRule(r, "out", i))}
              </ul>
            </div>
          )}
          {inbound.length > 0 && (
            <div>
              <div className="mb-0.5 text-[9px] uppercase tracking-wider text-slate-500">
                Inbound to {nodeZone ? humanZoneName(nodeZone) : "this zone"}
              </div>
              <ul className="space-y-1">
                {inbound.slice(0, 6).map((r, i) => renderRule(r, "in", i))}
              </ul>
            </div>
          )}
          <div className="mt-1.5 text-[10px] text-slate-500 italic">
            Policy is enforced per zone, not per host. This list shows
            the rules that govern any flow between {nodeZone ? humanZoneName(nodeZone) : "this zone"} and another zone.
          </div>
        </div>
      )}
    </div>
  );
}

// Map containd zone names to our network names
const containdZoneToNetwork: Record<string, string[]> = {
  wan: ["enterprise_net"],
  dmz: ["vendor_net"],
  lan1: ["ot_ops_net"],
  lan2: ["field_net"],
};

// Per-zone interface metadata for the firewall→zone tooltip. Pulled
// from the lab's docker-compose layout: containd binds one interface
// per zone, and each interface is the default gateway for its subnet.
// Surfacing this in the tooltip lets students answer "which side of
// the firewall am I looking at?" without leaving the network map.
const ZONE_INTERFACE_META: Record<
  string,
  {
    label: string;       // human zone name
    iface: string;       // containd interface name (ethN)
    role: string;        // containd zone label (wan/dmz/lan1/lan2)
    subnet: string;      // CIDR
    fwIp: string;        // firewall's interface IP in this zone
    description: string; // one-line teaching hint
  }
> = {
  enterprise_net: {
    label: "Enterprise",
    iface: "eth0",
    role: "wan",
    subnet: "10.10.10.0/24",
    fwIp: "10.10.10.2",
    description: "Corporate IT zone. Hosts: corp-ws, kali (attacker).",
  },
  vendor_net: {
    label: "Vendor / DMZ",
    iface: "eth1",
    role: "dmz",
    subnet: "10.20.20.0/24",
    fwIp: "10.20.20.2",
    description: "Vendor remote-access DMZ. Hosts: vendor-jump, eng-ws.",
  },
  ot_ops_net: {
    label: "OT Operations",
    iface: "eth2",
    role: "lan1",
    subnet: "10.30.30.0/24",
    fwIp: "10.30.30.2",
    description: "Supervisory control plane. Hosts: rtac, fuxa-hmi, openplc, historian, gps.",
  },
  field_net: {
    label: "Field Devices",
    iface: "eth3",
    role: "lan2",
    subnet: "10.40.40.0/24",
    fwIp: "10.40.40.2",
    description: "Protective devices. Hosts: relay, recloser, regulator, capbank.",
  },
};

// Build policy info for a zone from the live containd rules. The label
// favors protocol names over raw port numbers — students don't need to
// know that 502 is Modbus, they need to know "Modbus is allowed."
type Permissiveness = "allowed" | "over" | "blocked" | "unknown";

function getZonePolicyInfo(
  zone: string,
  ruleSummaries?: ZoneRuleSummary[],
): {
  label: string;
  details: string[];
  action: string;
  permissiveness: Permissiveness;
} {
  if (!ruleSummaries || ruleSummaries.length === 0) {
    return {
      label: "No policy data",
      details: [],
      action: "ALLOW",
      permissiveness: "unknown",
    };
  }

  // Find rules that apply to this zone (as destination from firewall)
  const containdZones = Object.entries(containdZoneToNetwork)
    .filter(([_, networks]) => networks.includes(zone))
    .map(([cz]) => cz);

  const relevantRules = ruleSummaries.filter(
    (r) => containdZones.includes(r.dest_zone) || r.dest_zone === "any",
  );

  if (relevantRules.length === 0) {
    return {
      label: "No matching rules",
      details: [],
      action: "ALLOW",
      permissiveness: "unknown",
    };
  }

  // Aggregate action
  const hasAllow = relevantRules.some((r) => r.action === "ALLOW");
  const hasDeny = relevantRules.some((r) => r.action === "DENY");
  const action = hasAllow && hasDeny ? "MIXED" : hasDeny ? "DENY" : "ALLOW";

  // Detect over-permissive: any allow rule whose detail starts with
  // "WEAK:" — that's the lab definition's convention for "this rule
  // should be tightened in the improved policy". When the active
  // config is the weak baseline, those rules are still in effect and
  // should read visually as warnings.
  const overPermissive = relevantRules.some(
    (r) =>
      r.action !== "DENY" &&
      r.rule_details.some((d) => d.trim().toUpperCase().startsWith("WEAK:")),
  );
  const permissiveness: Permissiveness =
    action === "DENY" ? "blocked" : overPermissive ? "over" : "allowed";

  // Structured details for tooltip
  const details = relevantRules.map((r) => {
    const src = r.source_zone || "any";
    const dst = r.dest_zone || "any";
    const desc = r.rule_details.length > 0 ? r.rule_details[0] : r.summary;
    return `${r.action} ${src}→${dst}: ${desc}`;
  });

  // Collect unique protocol names across all relevant ALLOW rules.
  const protoSet = new Set<string>();
  relevantRules
    .filter((r) => r.action === "ALLOW")
    .forEach((r) => humanizeSummary(r.summary).forEach((p) => protoSet.add(p)));
  const protos = Array.from(protoSet);

  // Build a label tuned to teach the student what kind of conduit this
  // is. Compact and unambiguous — never "+1" / "+3".
  let label: string;
  if (action === "DENY") {
    label = "BLOCKED";
  } else if (action === "MIXED") {
    label = protos.length > 0 ? `MIXED · ${protos.slice(0, 2).join(" / ")}` : "MIXED";
  } else if (protos.length === 0) {
    label = "ALLOW";
  } else if (protos.length === 1) {
    label = protos[0];
  } else if (protos.length <= 3) {
    label = protos.join(" / ");
  } else {
    // 4+ protocols — say so explicitly instead of dumping a +N tail.
    label = `Multiple flows (${protos.length})`;
  }

  return { label, details, action, permissiveness };
}

// Map a workshop graph node id to the runtime telemetry source that
// drives its status dot. Nodes not in the map don't get a dot — we
// only render dots for nodes whose health we actually probe.
function deriveNodeHealth(
  nodeId: string,
  workshopOnline: { firewall?: boolean; rtac?: boolean; deviceComms?: Record<string, boolean> },
): { health?: "ok" | "down"; healthSource?: string } {
  if (nodeId === "fw-1") {
    return {
      health: workshopOnline.firewall ? "ok" : "down",
      healthSource: "containd /api/v1/health",
    };
  }
  if (nodeId === "rtac-1") {
    return {
      health: workshopOnline.rtac ? "ok" : "down",
      healthSource: "RTAC /api/state",
    };
  }
  const deviceMap: Record<string, string> = {
    "relay-1": "relay",
    "recloser-1": "recloser",
    "regulator-1": "regulator",
  };
  const dev = deviceMap[nodeId];
  if (dev) {
    const ok = workshopOnline.deviceComms?.[dev];
    return {
      health: ok ? "ok" : "down",
      healthSource: `RTAC device_comms[${dev}]`,
    };
  }
  return {};
}

function useStyledGraph(
  graph?: LabGraph,
  ruleSummaries?: ZoneRuleSummary[],
  viewMode: ViewMode = { policyDim: true, traffic: false },
  deviceComms?: Record<string, boolean>,
  rtacOnline?: boolean,
  trafficStatus?: TrafficStatus,
  firewallOnline?: boolean,
) {
  return useMemo(() => {
    if (!graph) return { nodes: [] as Node[], edges: [] as Edge[] };

    const workshopOnline = {
      firewall: firewallOnline,
      rtac: rtacOnline,
      deviceComms,
    };

    // Layout configuration
    const ZONE_SPACING_X = 240;
    const NODE_SPACING_Y = 130;
    const FIREWALL_Y = 50;
    const ZONE_START_Y = 220;

    // Find the firewall node (containd_ngfw or opnsense_external)
    const firewallNode = graph.nodes.find((n) =>
      n.type === "containd_ngfw" || n.type === "opnsense_external"
    );

    // Group non-firewall, non-zone nodes by zone (host nodes only)
    const nodesByZone: Record<string, ApiGraphNode[]> = {};
    graph.nodes.forEach((n) => {
      if (n.type === "containd_ngfw" || n.type === "opnsense_external" || n.type === "zone") {
        return;
      }
      const zone = n.data.zone || "unknown";
      if (!nodesByZone[zone]) nodesByZone[zone] = [];
      nodesByZone[zone].push(n);
    });

    // Define preferred zone order for layout
    const zoneOrder = [
      "enterprise_net", "wan", "it_net",
      "vendor_net", "dmz", "dmz_net",
      "ot_ops_net", "lan1", "ot_control", "ot_control_net",
      "field_net", "lan2", "ot_safety", "ot_safety_net",
    ];
    const activeZones = Object.keys(nodesByZone)
      .filter(z => zones.includes(z as typeof zones[number]))
      .sort((a, b) => {
        const aIdx = zoneOrder.indexOf(a);
        const bIdx = zoneOrder.indexOf(b);
        return (aIdx === -1 ? 999 : aIdx) - (bIdx === -1 ? 999 : bIdx);
      });

    // Calculate layout dimensions
    const numZones = activeZones.length;
    const totalWidth = (numZones - 1) * ZONE_SPACING_X;
    const startX = -totalWidth / 2;
    const centerX = 0;

    const styledNodes: Node[] = [];

    // Add firewall node at top center
    if (firewallNode) {
      const fwHealth = deriveNodeHealth(firewallNode.id, workshopOnline);
      styledNodes.push({
        id: firewallNode.id,
        type: "firewall",
        position: { x: centerX, y: FIREWALL_Y },
        data: {
          label: firewallNode.data.label || "containd NGFW",
          nodeType: firewallNode.type,
          zone: "enterprise_net",
          status: firewallNode.data.status || "running",
          ip: firewallNode.data.ip,
          interface_ips: firewallNode.data.interface_ips,
          networks: firewallNode.data.networks,
          ui_path: firewallNode.data.ui_path,
          external_ui_url: firewallNode.data.external_ui_url,
          health: fwHealth.health,
          healthSource: fwHealth.healthSource,
        },
        draggable: true,
      });
    }

    // Zone display labels and subnets
    const zoneLabels: Record<string, string> = {
      enterprise_net: "Enterprise Zone",
      vendor_net: "Vendor / Engineering",
      ot_ops_net: "OT Operations",
      field_net: "Field Devices",
      wan: "Enterprise Zone",
      dmz: "Vendor / Engineering",
      ot_control: "OT Operations",
      ot_safety: "Field Devices",
      it_workstations: "IT Workstations",
      it_net: "Enterprise Zone",
      dmz_net: "Vendor / Engineering",
      ot_control_net: "OT Operations",
      ot_safety_net: "Field Devices",
    };

    const zoneSubnets: Record<string, string> = {
      enterprise_net: "10.10.10.0/24",
      vendor_net: "10.20.20.0/24",
      ot_ops_net: "10.30.30.0/24",
      field_net: "10.40.40.0/24",
      wan: "10.10.10.0/24",
      dmz: "10.20.20.0/24",
      ot_control: "10.30.30.0/24",
      ot_safety: "10.40.40.0/24",
      it_net: "10.10.10.0/24",
      dmz_net: "10.20.20.0/24",
      ot_control_net: "10.30.30.0/24",
      ot_safety_net: "10.40.40.0/24",
    };

    // Boundary box geometry shared across all zones. The boundary
    // wraps a zone column with enough room to clear the icons + the
    // host labels below them.
    const BOUNDARY_HALF_WIDTH = 100;
    const BOUNDARY_TOP_PAD = 36;
    const BOUNDARY_BOTTOM_PAD = 70;
    const BOUNDARY_TOP_Y = ZONE_START_Y - BOUNDARY_TOP_PAD;

    // Add zone BOUNDARY nodes first so they render BEHIND the hosts.
    // Each boundary is a translucent rounded panel that visually
    // groups the hosts inside the zone — the segmentation grouping
    // becomes immediately legible without any heavy borders.
    activeZones.forEach((zone, zoneIdx) => {
      const zoneX = startX + zoneIdx * ZONE_SPACING_X;
      const zoneNodes = nodesByZone[zone] || [];
      const lastNodeY = ZONE_START_Y + 100 + Math.max(0, zoneNodes.length - 1) * NODE_SPACING_Y;
      const boundaryHeight = lastNodeY + BOUNDARY_BOTTOM_PAD - BOUNDARY_TOP_Y;
      styledNodes.push({
        id: `zone-boundary-${zone}`,
        type: "zoneBoundary",
        position: {
          x: zoneX - BOUNDARY_HALF_WIDTH,
          y: BOUNDARY_TOP_Y,
        },
        data: {
          zone,
          label: zoneLabels[zone] || zone.toUpperCase(),
          subnet: zoneSubnets[zone],
          width: BOUNDARY_HALF_WIDTH * 2,
          height: boundaryHeight,
        },
        selectable: false,
        draggable: false,
        zIndex: 0,
      });
    });

    // Add zone nodes and host nodes
    activeZones.forEach((zone, zoneIdx) => {
      const zoneX = startX + zoneIdx * ZONE_SPACING_X;
      const zoneNodes = nodesByZone[zone] || [];

      // Tiny invisible anchor node so firewall→zone edges still have
      // a defined target inside the zone column. We keep using the
      // existing "zone-<name>" id for the edge target so the rest of
      // the wiring stays unchanged. Renders as an empty 1x1 div.
      styledNodes.push({
        id: `zone-${zone}`,
        type: "zone",
        position: { x: zoneX, y: ZONE_START_Y + 24 },
        data: {
          label: "",
          zone,
          subnet: undefined,
        },
        selectable: false,
        draggable: false,
        zIndex: 1,
      });

      // Add host nodes below the zone label
      zoneNodes.forEach((n, nodeIdx) => {
        // Detect multi-homing: a node with interfaces in more than one
        // zone gets a "MULTI" badge and a list of additional zones for
        // the inspector. The primary zone is the one we placed it in.
        const allInterfaceZones = n.data.interface_ips
          ? Object.keys(n.data.interface_ips).filter((z) => zones.includes(z as typeof zones[number]))
          : (n.data.networks || []);
        const additionalZones = allInterfaceZones.filter((z) => z !== zone);

        const nodeHealth = deriveNodeHealth(n.id, workshopOnline);

        styledNodes.push({
          id: n.id,
          type: "host",
          position: {
            x: zoneX,
            y: ZONE_START_Y + 100 + nodeIdx * NODE_SPACING_Y,
          },
          data: {
            label: n.data.label || n.id,
            nodeType: n.type,
            zone: zone,
            status: n.data.status || "running",
            ip: n.data.ip,
            interface_ips: n.data.interface_ips,
            networks: n.data.networks,
            ui_path: n.data.ui_path,
            external_ui_url: n.data.external_ui_url,
            multiHomedZones: additionalZones,
            health: nodeHealth.health,
            healthSource: nodeHealth.healthSource,
          },
          draggable: true,
        });
      });
    });

    // Create edges
    const styledEdges: Edge[] = [];

    // Edges from firewall to zones — segmentation boundaries.
    //
    // Policy view ON: edges are colored by action (zone color for
    // ALLOW, red for DENY, amber for MIXED), allowed paths are
    // animated, denied paths are dashed and dimmed. The student can
    // scan policy state at a glance.
    //
    // Policy view OFF: every edge looks the same — neutral grey, no
    // animation, no action color, no protocol label. The map becomes
    // a pure topology view ("what exists"), independent of policy
    // ("what's allowed"). This makes the toggle visibly do something
    // even when the current policy has no DENY rules.
    if (firewallNode) {
      activeZones.forEach((zone) => {
        const zoneStroke = zoneColors[zone]?.border || "#64748b";
        const policyInfo = getZonePolicyInfo(zone, ruleSummaries);
        const policyOn = viewMode.policyDim;

        // State encoding driven by `permissiveness`:
        //   allowed → steady cyan/zone glow
        //   over    → thicker amber/orange + soft pulse (warning)
        //   blocked → dim dashed red
        //   unknown → faint grey
        // Policy view OFF flattens everything to neutral grey.
        const state = policyOn ? policyInfo.permissiveness : "topology";
        const styleByState: Record<
          string,
          { stroke: string; width: number; dash?: string; opacity: number; glow: number }
        > = {
          allowed: { stroke: zoneStroke, width: 2.4, opacity: 1, glow: 5 },
          over: { stroke: "#f59e0b", width: 3.4, opacity: 1, glow: 8 },
          blocked: { stroke: "#ef4444", width: 1.6, dash: "6 4", opacity: 0.55, glow: 3 },
          unknown: { stroke: "#64748b", width: 1.4, opacity: 0.7, glow: 0 },
          topology: { stroke: "#475569", width: 1.4, opacity: 0.85, glow: 0 },
        };
        const cfg = styleByState[state];

        styledEdges.push({
          id: `fw-to-${zone}`,
          source: firewallNode.id,
          target: `zone-${zone}`,
          type: "policyEdge",
          style: {
            stroke: cfg.stroke,
            strokeWidth: cfg.width,
            strokeDasharray: cfg.dash,
            opacity: cfg.opacity,
            filter: cfg.glow ? `drop-shadow(0 0 ${cfg.glow}px ${cfg.stroke})` : undefined,
          },
          // Allowed paths animate (energized conduit). Over-permissive
          // edges get the CSS pulse class via data.pulse so a student
          // catches the warning at a glance.
          animated: state === "allowed",
          data: {
            label: policyOn ? policyInfo.label : "",
            details: policyInfo.details,
            action: policyInfo.action,
            permissiveness: policyInfo.permissiveness,
            color: cfg.stroke,
            dimmed: state === "blocked",
            pulse: state === "over",
            zoneMeta: ZONE_INTERFACE_META[zone],
          },
        });
      });
    }

    // Edges from zones to their host nodes — these are pure topology
    // (which host belongs to which zone), so they stay neutral grey
    // with no policy semantics.
    activeZones.forEach((zone) => {
      const zoneNodes = nodesByZone[zone] || [];
      const color = zoneColors[zone]?.border || "#64748b";

      zoneNodes.forEach((n, idx) => {
        const sourceId = idx === 0 ? `zone-${zone}` : zoneNodes[idx - 1].id;
        styledEdges.push({
          id: `${sourceId}-to-${n.id}`,
          source: sourceId,
          target: n.id,
          style: { stroke: color, strokeWidth: 1.2, opacity: 0.55 },
        });
      });
    });

    // Traffic view: render observed host-to-host flows when the toggle
    // is on. We aggregate by source-target pair so a single line
    // represents one logical conversation between two nodes — even if
    // the flow uses multiple protocols (RTAC↔relay carries both
    // Modbus and DNP3, for example). Aggregation is the difference
    // between 14 sprawling lines and ~9 readable ones.
    if (viewMode.traffic) {
      const presentNodeIds = new Set(styledNodes.map((n) => n.id));
      const nodeIndex = new Map(styledNodes.map((n) => [n.id, n]));

      // Group flows by `${source}→${target}` so each pair gets one
      // edge with a combined protocol label.
      const groups = new Map<string, ObservedFlow[]>();
      for (const flow of OBSERVED_FLOWS) {
        if (!presentNodeIds.has(flow.source) || !presentNodeIds.has(flow.target)) continue;
        const key = `${flow.source}->${flow.target}`;
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key)!.push(flow);
      }

      Array.from(groups.entries()).forEach(([key, flows]) => {
        const first = flows[0];
        // A pair is "active" if any of its constituent flows is active.
        const statuses = flows.map((f) =>
          resolveLiveness(f, deviceComms, rtacOnline ?? false, trafficStatus),
        );
        const status: "active" | "idle" | "down" = statuses.includes("active")
          ? "active"
          : statuses.every((s) => s === "down")
          ? "down"
          : "idle";

        // Combined protocol label: dedup, preserve order, max 3.
        const protocols: string[] = [];
        for (const f of flows) {
          if (!protocols.includes(f.protocol)) protocols.push(f.protocol);
        }
        const label =
          protocols.length <= 3
            ? protocols.join(" + ")
            : `${protocols.slice(0, 2).join(" + ")} +${protocols.length - 2}`;

        // Pick handles based on the relative x of source vs target so
        // the edge always exits the side closest to the target node.
        const srcNode = nodeIndex.get(first.source);
        const dstNode = nodeIndex.get(first.target);
        const srcX = srcNode?.position.x ?? 0;
        const dstX = dstNode?.position.x ?? 0;
        const srcOnLeft = srcX <= dstX;
        const sourceHandle = srcOnLeft ? "right-source" : "left-source";
        const targetHandle = srcOnLeft ? "left-target" : "right-target";

        const tooltipLines = [
          `${first.source} → ${first.target}`,
          ...flows.map((f) => `${f.protocol} on tcp/${f.port} · ${f.cadence}`),
          `category: ${first.category}`,
        ];

        styledEdges.push({
          id: `traffic-${key}`,
          source: first.source,
          target: first.target,
          sourceHandle,
          targetHandle,
          type: "trafficEdge",
          animated: status === "active",
          data: {
            label,
            status,
            tooltipLines,
          },
        });
      });
    }

    return { nodes: styledNodes, edges: styledEdges };
  }, [
    graph,
    ruleSummaries,
    viewMode.policyDim,
    viewMode.traffic,
    deviceComms,
    rtacOnline,
    trafficStatus,
    firewallOnline,
  ]);
}
