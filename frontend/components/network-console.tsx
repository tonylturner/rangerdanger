"use client";

import React, { useCallback, useMemo, useState } from "react";
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
} from "reactflow";
import "reactflow/dist/style.css";
import { useQuery } from "@tanstack/react-query";
import {
  getWorkshopGraph,
  getWorkshopStatus,
  getFirewallRules,
  LabGraph,
  GraphNode as ApiGraphNode,
  ZoneRuleSummary,
  WorkshopStatus,
} from "../lib/api";
import { Button } from "./ui/button";
import { NodeTerminal } from "./node-terminal";
import { nodeTypes, zoneColors } from "./topology-nodes";
import { ExternalLink, Maximize2, X } from "lucide-react";

// All supported zone names
const zones = [
  "enterprise_net", "vendor_net", "ot_ops_net", "field_net",
  // Legacy zone names
  "wan", "dmz", "ot_control", "ot_safety", "it_workstations",
  "it_net", "dmz_net", "ot_control_net", "ot_safety_net"
] as const;

// Custom edge component with tooltip for firewall policy details
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
  const [showTooltip, setShowTooltip] = useState(false);
  const [tooltipPos, setTooltipPos] = useState({ x: 0, y: 0 });

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

  const handleMouseEnter = (e: React.MouseEvent) => {
    if (data?.details && data.details.length > 0) {
      setTooltipPos({ x: e.clientX, y: e.clientY });
      setShowTooltip(true);
    }
  };

  const handleMouseLeave = () => {
    setShowTooltip(false);
  };

  const label = data?.label || "";
  const color = data?.color || "#64748b";
  const action = data?.action || "ALLOW";
  const details = data?.details || [];

  // Action indicator icon
  const actionIcon = action === "DENY" ? "\u2715" : action === "MIXED" ? "\u26A0" : "";

  // Calculate label width based on content
  const labelWidth = Math.max(70, label.length * 6 + 20);

  return (
    <>
      <path
        id={id}
        style={style}
        className="react-flow__edge-path"
        d={edgePath}
        markerEnd={markerEnd}
      />
      {label && (
        <g
          onMouseEnter={handleMouseEnter}
          onMouseLeave={handleMouseLeave}
          style={{ cursor: details.length > 0 ? "help" : "default" }}
        >
          <rect
            x={labelX - labelWidth / 2}
            y={labelY - 10}
            width={labelWidth}
            height={20}
            rx={4}
            fill="#0f172a"
            fillOpacity={0.95}
            stroke={color}
            strokeWidth={1}
            strokeOpacity={0.3}
          />
          <text
            x={labelX}
            y={labelY + 4}
            textAnchor="middle"
            fontSize={10}
            fontWeight={500}
            fill={color}
          >
            {actionIcon && (
              <tspan fill={action === "DENY" ? "#ef4444" : "#f59e0b"}>
                {actionIcon}{" "}
              </tspan>
            )}
            {label}
          </text>
        </g>
      )}
      {showTooltip && details.length > 0 && (
        <foreignObject
          x={labelX - 120}
          y={labelY + 15}
          width={240}
          height={Math.min(details.length * 24 + 32, 200)}
          style={{ overflow: "visible" }}
        >
          <div
            className="rounded-lg border border-slate-700 bg-slate-900 px-3 py-2 shadow-xl"
            style={{ fontSize: 11 }}
          >
            <div className="mb-1.5 text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Firewall Rules
            </div>
            <ul className="space-y-1">
              {details.slice(0, 6).map((detail: string, i: number) => (
                <li key={i} className="text-slate-200 leading-tight">
                  {detail}
                </li>
              ))}
              {details.length > 6 && (
                <li className="text-slate-500 italic">
                  +{details.length - 6} more rules...
                </li>
              )}
            </ul>
          </div>
        </foreignObject>
      )}
    </>
  );
}

// Edge types registration
const edgeTypes = {
  policyEdge: PolicyEdge,
};

export function NetworkConsole() {
  const [inspectorNode, setInspectorNode] = useState<Node | null>(null);
  const [iframeUrl, setIframeUrl] = useState<string | null>(null);
  const [showTerminal, setShowTerminal] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [showTerminalModal, setShowTerminalModal] = useState(false);
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});

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

  // Fetch firewall rules for dynamic edge labels
  const { data: firewallRulesData } = useQuery({
    queryKey: ["firewall-rules"],
    queryFn: getFirewallRules,
    refetchInterval: 30000,
    staleTime: 10000,
  });

  const { nodes: layoutNodes, edges } = useStyledGraph(graph, firewallRulesData?.summaries);

  // Apply saved positions to nodes (for drag persistence)
  const nodes = useMemo(() => {
    return layoutNodes.map((node) => {
      const savedPos = nodePositions[node.id];
      return savedPos ? { ...node, position: savedPos } : node;
    });
  }, [layoutNodes, nodePositions]);

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

  // Handle node click - don't close terminal if clicking same node
  const handleNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    if (node.type === "zone") return;
    if (inspectorNode?.id === node.id) return;

    setInspectorNode(node);
    setIframeUrl(null);
    setShowTerminal(false);
  }, [inspectorNode?.id]);

  const errors = useMemo(() => {
    const list: string[] = [];
    if (graphIsError && graphError)
      list.push(`Failed to load topology: ${graphError instanceof Error ? graphError.message : "Unknown error"}`);
    return list;
  }, [graphIsError, graphError]);

  return (
    <div className="flex h-[calc(100vh-3rem)] flex-col space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.3em] text-slate-400">Network Map</p>
          <h1 className="text-3xl font-semibold text-white">Substation Network Map</h1>
          <p className="text-sm text-slate-400">Distribution co-op feeder topology. Click a node to inspect, open UI, or terminal.</p>
        </div>
        {workshopStatus && (
          <div className="flex items-center gap-3 text-[10px]">
            <StatusDot ok={workshopStatus.firewall_online} label="containd" />
            <StatusDot ok={workshopStatus.rtac_online} label="RTAC" />
            <StatusDot
              ok={workshopStatus.device_comms ? Object.values(workshopStatus.device_comms).every(Boolean) : false}
              label={`Devices ${workshopStatus.device_comms ? Object.values(workshopStatus.device_comms).filter(Boolean).length : 0}/${workshopStatus.device_comms ? Object.keys(workshopStatus.device_comms).length : 0}`}
            />
            <span className={`rounded border px-2 py-0.5 font-bold ${
              workshopStatus.firewall_config === "improved"
                ? "border-green-800/60 text-green-400"
                : "border-red-800/60 text-red-400"
            }`}>
              {workshopStatus.firewall_config === "improved" ? "Hardened" : "Weak"}
            </span>
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
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onNodeClick={handleNodeClick}
          fitView
          nodesDraggable
          minZoom={0.3}
          maxZoom={2}
          defaultViewport={{ x: 0, y: 0, zoom: 0.8 }}
        >
          <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="#1e293b" />
          <MiniMap
            pannable
            zoomable
            nodeColor={(node) => {
              const zone = node.data?.zone;
              return zoneColors[zone]?.border || "#64748b";
            }}
            maskColor="rgba(15, 23, 42, 0.8)"
            style={{ backgroundColor: "#0f172a", border: "1px solid #334155" }}
          />
          <Controls className="!bg-slate-800 !border-slate-700" />
        </ReactFlow>
        {graphLoading && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-slate-950/60 text-slate-200">
            Loading console…
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
            {showTerminal ? (
              <>
                <NodeTerminal
                  nodeId={inspectorNode.id}
                  labId="workshop"
                  onClose={() => setShowTerminal(false)}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  className="absolute top-2 right-10 bg-slate-900/80 hover:bg-slate-800 z-20"
                  onClick={() => setShowTerminalModal(true)}
                  title="Expand terminal"
                >
                  <Maximize2 className="h-4 w-4" />
                </Button>
              </>
            ) : iframeUrl ? (
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
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-slate-500">
                Select UI or Terminal to view here.
              </div>
            )}
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

function StatusDot({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className="flex items-center gap-1.5 text-slate-400">
      <span className={`h-2 w-2 rounded-full ${ok ? "bg-green-500" : "bg-red-500"}`} />
      {label}
    </span>
  );
}

// Map containd zone names to our network names
const containdZoneToNetwork: Record<string, string[]> = {
  wan: ["enterprise_net"],
  dmz: ["vendor_net"],
  lan1: ["ot_ops_net"],
  lan2: ["field_net"],
};

// Get policy info for a zone from firewall rules
function getZonePolicyInfo(
  zone: string,
  ruleSummaries?: ZoneRuleSummary[]
): { label: string; details: string[]; action: string } {
  // Default/fallback labels - concise single labels per zone
  const fallbackLabels: Record<string, { label: string; action: string }> = {
    enterprise_net: { label: "IT Access", action: "ALLOW" },
    vendor_net: { label: "DMZ Access", action: "ALLOW" },
    ot_ops_net: { label: "Modbus R/W", action: "ALLOW" },
    field_net: { label: "Read Only", action: "DENY" },
  };

  if (!ruleSummaries || ruleSummaries.length === 0) {
    const fallback = fallbackLabels[zone] || { label: "", action: "ALLOW" };
    return { label: fallback.label, details: [], action: fallback.action };
  }

  // Find rules that apply to this zone (as destination from firewall)
  const containdZones = Object.entries(containdZoneToNetwork)
    .filter(([_, networks]) => networks.includes(zone))
    .map(([cz]) => cz);

  const relevantRules = ruleSummaries.filter(
    (r) => containdZones.includes(r.dest_zone) || r.dest_zone === "any"
  );

  if (relevantRules.length === 0) {
    const fallback = fallbackLabels[zone] || { label: "", action: "ALLOW" };
    return { label: fallback.label, details: [], action: fallback.action };
  }

  // Collect all rule details for tooltip
  const details = relevantRules.flatMap((r) => r.rule_details);
  const hasAllow = relevantRules.some((r) => r.action === "ALLOW");
  const hasDeny = relevantRules.some((r) => r.action === "DENY");
  const action = hasAllow && hasDeny ? "MIXED" : hasDeny ? "DENY" : "ALLOW";

  // Use a single concise label - prioritize the most relevant rule's summary
  const primaryRule = relevantRules.find((r) => r.action === (hasDeny ? "DENY" : "ALLOW"));
  let label = primaryRule?.summary || relevantRules[0]?.summary || "";

  // Truncate long labels and show count if multiple rules
  if (label.length > 12) {
    label = label.substring(0, 10) + "...";
  }
  if (relevantRules.length > 1) {
    label = `${label} +${relevantRules.length - 1}`;
  }

  // If label is still empty or too generic, use fallback
  if (!label || label === "ALLOW" || label === "DENY") {
    label = fallbackLabels[zone]?.label || action;
  }

  return { label, details, action };
}

function useStyledGraph(graph?: LabGraph, ruleSummaries?: ZoneRuleSummary[]) {
  return useMemo(() => {
    if (!graph) return { nodes: [] as Node[], edges: [] as Edge[] };

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

    // Add zone nodes and host nodes
    activeZones.forEach((zone, zoneIdx) => {
      const zoneX = startX + zoneIdx * ZONE_SPACING_X;
      const zoneNodes = nodesByZone[zone] || [];

      // Add zone label node
      styledNodes.push({
        id: `zone-${zone}`,
        type: "zone",
        position: { x: zoneX, y: ZONE_START_Y },
        data: {
          label: zoneLabels[zone] || zone.toUpperCase(),
          zone: zone,
          subnet: zoneSubnets[zone],
        },
        selectable: false,
        draggable: false,
      });

      // Add host nodes below the zone label
      zoneNodes.forEach((n, nodeIdx) => {
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
          },
          draggable: true,
        });
      });
    });

    // Create edges
    const styledEdges: Edge[] = [];

    // Edges from firewall to zones (with dynamic policy labels)
    if (firewallNode) {
      activeZones.forEach((zone) => {
        const color = zoneColors[zone]?.border || "#64748b";
        const policyInfo = getZonePolicyInfo(zone, ruleSummaries);

        // Color the label based on action
        const labelColor =
          policyInfo.action === "DENY"
            ? "#ef4444"
            : policyInfo.action === "MIXED"
            ? "#f59e0b"
            : color;

        styledEdges.push({
          id: `fw-to-${zone}`,
          source: firewallNode.id,
          target: `zone-${zone}`,
          type: "policyEdge",
          style: { stroke: color, strokeWidth: 2 },
          animated: true,
          data: {
            label: policyInfo.label,
            details: policyInfo.details,
            action: policyInfo.action,
            color: labelColor,
          },
        });
      });
    }

    // Edges from zones to their host nodes
    activeZones.forEach((zone) => {
      const zoneNodes = nodesByZone[zone] || [];
      const color = zoneColors[zone]?.border || "#64748b";

      zoneNodes.forEach((n, idx) => {
        const sourceId = idx === 0 ? `zone-${zone}` : zoneNodes[idx - 1].id;
        styledEdges.push({
          id: `${sourceId}-to-${n.id}`,
          source: sourceId,
          target: n.id,
          style: { stroke: color, strokeWidth: 1.5 },
        });
      });
    });

    return { nodes: styledNodes, edges: styledEdges };
  }, [graph, ruleSummaries]);
}
