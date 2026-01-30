"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  Edge,
  MiniMap,
  Node,
  NodeChange,
  applyNodeChanges,
} from "reactflow";
import "reactflow/dist/style.css";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createLabInstance,
  getLabGraph,
  listLabInstances,
  listLabTemplates,
  seedTemplates,
  LabGraph,
  LabInstance,
  GraphNode as ApiGraphNode,
} from "../lib/api";
import { Button } from "./ui/button";
import { NodeTerminal } from "./node-terminal";
import { nodeTypes, zoneColors } from "./topology-nodes";

// All supported zone names (new and legacy)
const zones = [
  "wan", "dmz", "ot_control", "ot_safety", "it_workstations",
  // Legacy zone names
  "it_net", "dmz_net", "ot_control_net", "ot_safety_net"
] as const;

export function NetworkConsole() {
  const {
    data: labsData,
    isLoading: labsLoading,
    isError: labsIsError,
    error: labsError
  } = useQuery({ queryKey: ["lab-instances"], queryFn: listLabInstances });
  const {
    data: templatesData,
    isLoading: templatesLoading,
    isError: templatesIsError,
    error: templatesError
  } = useQuery({ queryKey: ["lab-templates"], queryFn: listLabTemplates });
  const [selectedLab, setSelectedLab] = useState<LabInstance | undefined>(undefined);
  const [inspectorNode, setInspectorNode] = useState<Node | null>(null);
  const [iframeUrl, setIframeUrl] = useState<string | null>(null);
  const [showTerminal, setShowTerminal] = useState(false);
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>({});
  const queryClient = useQueryClient();

  const launchLab = useMutation({
    mutationFn: async () => {
      let tmpl = templatesData?.templates?.[0];
      if (!tmpl) {
        await seedTemplates();
        const refreshed = await listLabTemplates();
        tmpl = refreshed.templates[0];
      }
      if (!tmpl) throw new Error("No templates available");
      const name = `Console Lab ${new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
      return createLabInstance(tmpl.id, name);
    },
    onSuccess: (lab) => {
      queryClient.invalidateQueries({ queryKey: ["lab-instances"] });
      queryClient.invalidateQueries({ queryKey: ["lab-templates"] });
      setSelectedLab(lab);
      setInspectorNode(null);
      setIframeUrl(null);
    }
  });

  useEffect(() => {
    if (!labsData?.instances) return;
    if (labsData.instances.length === 0) {
      setSelectedLab(undefined);
      setInspectorNode(null);
      setIframeUrl(null);
      setShowTerminal(false);
      return;
    }

    const current = labsData.instances.find((lab) => lab.id === selectedLab?.id);
    if (current) {
      setSelectedLab((prev) => (prev?.id === current.id ? prev : current));
      return;
    }

    setSelectedLab(labsData.instances[0]);
    setInspectorNode(null);
    setIframeUrl(null);
    setShowTerminal(false);
  }, [labsData, selectedLab?.id]);

  const {
    data: graph,
    isLoading: graphLoading,
    isError: graphIsError,
    error: graphError
  } = useQuery({
    queryKey: ["lab", selectedLab?.id, "graph"],
    queryFn: () => (selectedLab ? getLabGraph(selectedLab.id) : Promise.resolve(undefined)),
    enabled: Boolean(selectedLab?.id)
  });

  const { nodes: layoutNodes, edges } = useStyledGraph(graph);

  // Apply saved positions to nodes (for drag persistence)
  const nodes = useMemo(() => {
    return layoutNodes.map((node) => {
      const savedPos = nodePositions[node.id];
      return savedPos ? { ...node, position: savedPos } : node;
    });
  }, [layoutNodes, nodePositions]);

  // Handle node changes (dragging)
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    // Update positions for dragged nodes
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

    // If clicking the same node, don't change anything
    if (inspectorNode?.id === node.id) return;

    setInspectorNode(node);
    setIframeUrl(null);
    setShowTerminal(false);
  }, [inspectorNode?.id]);

  const errors = useMemo(() => {
    const list: string[] = [];
    if (labsIsError && labsError) list.push(`Failed to load labs: ${labsError instanceof Error ? labsError.message : "Unknown error"}`);
    if (templatesIsError && templatesError)
      list.push(
        `Failed to load templates: ${templatesError instanceof Error ? templatesError.message : "Unknown error"}`
      );
    if (launchLab.isError && launchLab.error)
      list.push(`Failed to start lab: ${launchLab.error instanceof Error ? launchLab.error.message : "Unknown error"}`);
    if (graphIsError && graphError)
      list.push(`Failed to load topology: ${graphError instanceof Error ? graphError.message : "Unknown error"}`);
    return list;
  }, [labsIsError, labsError, templatesIsError, templatesError, launchLab.isError, launchLab.error, graphIsError, graphError]);

  return (
    <div className="flex h-[calc(100vh-3rem)] flex-col space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[0.3em] text-slate-400">Main Console</p>
          <h1 className="text-3xl font-semibold text-white">OT Network Desktop</h1>
          <p className="text-sm text-slate-400">Pan/zoom the map. Click a node to open its UI or terminal.</p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <span className="text-slate-300">Lab:</span>
          {labsData?.instances?.length ? (
            <select
              className="rounded-md border border-slate-700 bg-slate-900 px-3 py-2 text-white"
              value={selectedLab?.id ?? ""}
              onChange={(e) => {
                const lab = labsData?.instances.find((l) => l.id === e.target.value);
                setSelectedLab(lab);
                setInspectorNode(null);
              }}
            >
              {(labsData?.instances ?? []).map((lab) => (
                <option key={lab.id} value={lab.id}>
                  {lab.name || lab.id}
                </option>
              ))}
            </select>
          ) : (
            <Button
              size="sm"
              variant="outline"
              disabled={launchLab.isPending || labsLoading || templatesLoading}
              onClick={() => launchLab.mutate()}
            >
              {launchLab.isPending ? "Launching..." : "Start default lab"}
            </Button>
          )}
        </div>
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
        {(graphLoading || labsLoading) && (
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
            <Button variant="ghost" size="sm" onClick={() => { setInspectorNode(null); setIframeUrl(null); setShowTerminal(false); }}>
              Close
            </Button>
          </div>

          <div className="mt-4 space-y-3 text-sm text-slate-200">
            <InfoRow label="Type" value={inspectorNode.data?.nodeType || inspectorNode.type || "unknown"} />
            <InfoRow label="Networks" value={(inspectorNode.data?.networks || []).join(", ") || "N/A"} />
            <InfoRow label="Status" value={inspectorNode.data?.status || "unknown"} />
            <InfoRow label="IP" value={inspectorNode.data?.ip || "N/A"} />
          </div>

          <div className="mt-6 flex gap-3">
            <Button
              className="flex-1"
              variant={!showTerminal ? "default" : "outline"}
              disabled={!inspectorNode.data?.ui_path}
              onClick={() => {
                if (inspectorNode.data?.ui_path) {
                  setIframeUrl(inspectorNode.data.ui_path);
                  setShowTerminal(false);
                }
              }}
            >
              {inspectorNode.data?.ui_path ? "UI" : "No UI"}
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
          </div>

          <div className="mt-6 h-[320px] overflow-hidden rounded-xl border border-slate-800 bg-black">
            {showTerminal && selectedLab ? (
              <NodeTerminal
                nodeId={inspectorNode.id}
                labId={selectedLab.id}
                onClose={() => setShowTerminal(false)}
              />
            ) : iframeUrl ? (
              <iframe title="Embedded UI" src={iframeUrl} className="h-full w-full border-0" />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-slate-500">
                Select UI or Terminal to view here.
              </div>
            )}
          </div>
        </aside>
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

function useStyledGraph(graph?: LabGraph) {
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
      // Skip firewall nodes and zone nodes (zones are rendered separately)
      if (n.type === "containd_ngfw" || n.type === "opnsense_external" || n.type === "zone") {
        return;
      }
      const zone = n.data.zone || "unknown";
      if (!nodesByZone[zone]) nodesByZone[zone] = [];
      nodesByZone[zone].push(n);
    });

    // Define preferred zone order for layout
    const zoneOrder = [
      "wan", "it_net", "it_workstations", "dmz", "dmz_net",
      "ot_control", "ot_control_net", "ot_safety", "ot_safety_net"
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
          zone: "wan",
          status: firewallNode.data.status || "running",
          ip: firewallNode.data.ip,
          networks: firewallNode.data.networks,
          ui_path: firewallNode.data.ui_path,
        },
        draggable: true,
      });
    }

    // Zone display labels and subnets
    const zoneLabels: Record<string, string> = {
      wan: "WAN",
      dmz: "DMZ",
      ot_control: "OT Control",
      ot_safety: "OT Safety",
      it_workstations: "IT Workstations",
      it_net: "IT Network",
      dmz_net: "DMZ",
      ot_control_net: "OT Control",
      ot_safety_net: "OT Safety",
    };

    const zoneSubnets: Record<string, string> = {
      wan: "192.168.240.0/24",
      dmz: "192.168.241.0/24",
      ot_control: "192.168.242.0/24",
      ot_safety: "192.168.243.0/24",
      it_workstations: "192.168.244.0/24",
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
            networks: n.data.networks,
            ui_path: n.data.ui_path,
          },
          draggable: true,
        });
      });
    });

    // Create edges
    const styledEdges: Edge[] = [];

    // Edges from firewall to zones
    if (firewallNode) {
      activeZones.forEach((zone) => {
        const color = zoneColors[zone]?.border || "#64748b";
        styledEdges.push({
          id: `fw-to-${zone}`,
          source: firewallNode.id,
          target: `zone-${zone}`,
          style: { stroke: color, strokeWidth: 2 },
          animated: true,
        });
      });
    }

    // Edges from zones to their host nodes
    activeZones.forEach((zone) => {
      const zoneNodes = nodesByZone[zone] || [];
      const color = zoneColors[zone]?.border || "#64748b";

      zoneNodes.forEach((n, idx) => {
        // Connect zone to first host, then chain hosts
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
  }, [graph]);
}
