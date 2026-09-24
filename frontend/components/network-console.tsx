"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactFlow, {
  Background,
  BackgroundVariant,
  ControlButton,
  Controls,
  EdgeLabelRenderer,
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
} from "../lib/api";
import { Button } from "./ui/button";
import { NodeTerminal } from "./node-terminal";
import { SharedTerminalPanel, useTerminals } from "./terminal-context";
import { nodeTypes } from "./topology-nodes";
import { zoneColors } from "../lib/zone-colors";
import { nodeZone } from "../lib/observed-flows";
import { ViewMode, humanZoneName } from "../lib/network-console-data";
import { edgeTypes } from "./network-edges";
import {
  ActiveFlowsPanel,
  InfoRow,
  LeftDrawerMenu,
  PolicyBadge,
  StatusDot,
  ViewModeToolbar,
} from "./network-drawers";
import { useStyledGraph } from "./use-styled-graph";
import { ExternalLink, Map as MapIcon, Maximize2, X } from "lucide-react";

export function NetworkConsole() {
  const [inspectorNode, setInspectorNode] = useState<Node | null>(null);
  const [iframeUrl, setIframeUrl] = useState<string | null>(null);
  const [showTerminal, setShowTerminal] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const [showTerminalModal, setShowTerminalModal] = useState(false);
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});
  const [viewMode, setViewMode] = useState<ViewMode>({
    policyDim: true,
    // Traffic edges default on - they're governed by the toggle
    // inside the Traffic Matrix drawer now, not a separate toolbar
    // button. The canvas still only renders them when policyDim is
    // on (same condition that mounts the drawer).
    traffic: true,
    iec62443: false,
  });
  // Rook glow flag - flips true for ~1.4s whenever the active
  // firewall config changes, then auto-clears. Skips the initial
  // load so we don't pulse on first paint.
  const [rookGlow, setRookGlow] = useState(false);
  // Minimap visibility - toggled from a custom button inside the
  // React Flow Controls panel. Default on.
  const [showMinimap, setShowMinimap] = useState(true);

  // Traffic matrix filter + highlight - lifted from the drawer so
  // the canvas edges can mirror whatever the student selects.
  const [trafficFilter, setTrafficFilter] = useState({
    zone: "all",
    crossZoneOnly: false,
  });
  const [highlightedTrafficPair, setHighlightedTrafficPair] = useState<string | null>(null);
  // Left drawer menu. The strip is mounted whenever Policy view is
  // on; the strip shows two stacked icons (Segmentation, Traffic
  // Matrix). Clicking either expands the drawer to 420px showing
  // that drawer's content. activeDrawer === null = strip only.
  // State is lifted so the canvas can re-fit the React Flow viewport
  // when the drawer changes width.
  type DrawerId = "segmentation" | "traffic";
  const [activeDrawer, setActiveDrawer] = useState<DrawerId | null>(null);
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance | null>(null);
  // Shared terminal state - persists across page navigations
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

  // Fetch firewall rules for dynamic edge labels - poll every 5s so
  // topology updates promptly after a config change in the segmentation tab.
  const { data: firewallRulesData } = useQuery({
    queryKey: ["firewall-rules"],
    queryFn: getFirewallRules,
    refetchInterval: 5000,
    staleTime: 2000,
  });

  // Traffic generator state - only polled when the Traffic view is on
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
    trafficFilter,
    highlightedTrafficPair,
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

      // Drawer minimized = 36px strip + gap. Expanded = 36px strip
      // + 420px panel + small breathing buffer so the leftmost zone
      // boundary clears the drawer's right edge.
      // Hidden = no left reservation.
      const leftPad = drawerExpanded ? 488 : drawerVisible ? 56 : 24;
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
  }, [rfInstance, drawerVisible, drawerExpanded, nodes.length, viewMode.iec62443]);

  // Handle node changes (dragging) - positions persist during session only
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    changes.forEach((change) => {
      if (change.type === "position" && change.position) {
        setNodePositions((prev) => ({ ...prev, [change.id]: change.position! }));
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
          {/* Rook - operator/control symbol. Sits next to the title
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
              tooltipBody="Relay, recloser, regulator. RTAC polls these via Modbus and DNP3. A drop here means a poll path is broken - device down, network issue, or firewall rule change."
            />
            <PolicyBadge activeConfig={workshopStatus.firewall_config} />
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
          {/* Subtle Tron grid - two layers: a fine cyan dot mesh for
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
          {showMinimap && (
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
          )}
          <Controls
            className="!bg-slate-800 !border-slate-700"
            position="top-right"
            showInteractive={false}
          >
            <ControlButton
              onClick={() => setShowMinimap((v) => !v)}
              title={showMinimap ? "Hide minimap" : "Show minimap"}
              className={showMinimap ? "minimap-toggle-on" : ""}
            >
              <MapIcon size={14} />
            </ControlButton>
          </Controls>
        </ReactFlow>
        {viewMode.policyDim && (
          <LeftDrawerMenu
            active={activeDrawer}
            onChange={setActiveDrawer}
            trafficEdgesOn={viewMode.traffic}
            onToggleTrafficEdges={() =>
              setViewMode((v) => ({ ...v, traffic: !v.traffic }))
            }
            trafficFilter={trafficFilter}
            onTrafficFilterChange={setTrafficFilter}
            highlightedTrafficPair={highlightedTrafficPair}
            onHighlightTrafficPair={setHighlightedTrafficPair}
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
            {/* Shared terminals - state tracked across pages */}
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
