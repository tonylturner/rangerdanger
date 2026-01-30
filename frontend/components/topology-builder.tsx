"use client";

import { useCallback, useState } from "react";
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  Edge,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  Connection,
  MiniMap,
  Node,
  NodeChange,
  EdgeChange,
} from "reactflow";
import "reactflow/dist/style.css";
import { useMutation } from "@tanstack/react-query";
import { Plus, Trash2, Network, X } from "lucide-react";

import { saveLabTemplate } from "../lib/api";
import { Button } from "./ui/button";
import { nodeTypes, zoneColors } from "./topology-nodes";

// All available node types
const nodeTypeOptions = [
  { type: "containd_ngfw", label: "containd NGFW", description: "ICS-aware firewall", rfType: "firewall" },
  { type: "ews", label: "Engineering WS", description: "noVNC desktop & PLC IDE", rfType: "host" },
  { type: "jump_host", label: "Jump Host", description: "Kali-lite pentest box", rfType: "host" },
  { type: "hmi_scada", label: "HMI / SCADA", description: "FUXA HMI server", rfType: "host" },
  { type: "historian", label: "Historian", description: "InfluxDB time-series DB", rfType: "host" },
  { type: "grafana", label: "Grafana", description: "Metrics visualization", rfType: "host" },
  { type: "ot_ids", label: "Suricata IDS", description: "Sensor with ICS rules", rfType: "host" },
  { type: "plc_trainer", label: "Process PLC", description: "OpenPLC runtime", rfType: "host" },
  { type: "sis_plc", label: "Safety PLC", description: "SIS logic", rfType: "host" },
];

// Default network colors
const defaultNetworkColors = [
  "#f59e0b", // amber
  "#22d3ee", // cyan
  "#a855f7", // purple
  "#f97316", // orange
  "#22c55e", // green
  "#ec4899", // pink
  "#3b82f6", // blue
];

type NetworkDef = {
  id: string;
  name: string;
  cidr: string;
  color: string;
};

const generateId = () => {
  const globalCrypto = typeof globalThis !== "undefined" ? globalThis.crypto : undefined;
  if (globalCrypto?.randomUUID) {
    return globalCrypto.randomUUID();
  }
  return `id-${Math.random().toString(36).slice(2, 9)}`;
};

export function TopologyBuilder() {
  const [templateName, setTemplateName] = useState("Custom OT Lab");
  const [description, setDescription] = useState("Custom lab topology.");
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [networks, setNetworks] = useState<NetworkDef[]>([
    { id: "wan", name: "WAN", cidr: "192.168.240.0/24", color: "#f59e0b" },
    { id: "it_net", name: "IT Network", cidr: "10.10.10.0/24", color: "#22d3ee" },
    { id: "dmz_net", name: "DMZ", cidr: "10.20.20.0/24", color: "#a855f7" },
    { id: "ot_control_net", name: "OT Control", cidr: "10.30.30.0/24", color: "#f97316" },
    { id: "ot_safety_net", name: "OT Safety", cidr: "10.40.40.0/24", color: "#22c55e" },
  ]);

  // Selection state for adding nodes
  const [selectedNodeType, setSelectedNodeType] = useState<typeof nodeTypeOptions[0] | null>(null);
  const [selectedNetwork, setSelectedNetwork] = useState<string>("it_net");

  // New network form
  const [showNetworkForm, setShowNetworkForm] = useState(false);
  const [newNetworkName, setNewNetworkName] = useState("");
  const [newNetworkCidr, setNewNetworkCidr] = useState("");

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => setNodes((nds) => applyNodeChanges(changes, nds)),
    []
  );
  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => setEdges((eds) => applyEdgeChanges(changes, eds)),
    []
  );
  const onConnect = useCallback(
    (connection: Connection) => {
      const sourceNode = nodes.find((n) => n.id === connection.source);
      const network = networks.find((net) => net.id === sourceNode?.data?.zone);
      const color = network?.color || "#64748b";
      setEdges((eds) =>
        addEdge(
          {
            ...connection,
            style: { stroke: color, strokeWidth: 2 },
            animated: true,
          },
          eds
        )
      );
    },
    [nodes, networks]
  );

  // Handle canvas click to place a node
  const onPaneClick = useCallback(
    (event: React.MouseEvent) => {
      if (!selectedNodeType) return;

      // Get click position relative to the React Flow pane
      const reactFlowBounds = (event.target as HTMLElement).closest('.react-flow')?.getBoundingClientRect();
      if (!reactFlowBounds) return;

      const position = {
        x: event.clientX - reactFlowBounds.left - 60,
        y: event.clientY - reactFlowBounds.top - 40,
      };

      const id = generateId();
      const network = networks.find((n) => n.id === selectedNetwork);

      const newNode: Node = {
        id,
        type: selectedNodeType.rfType,
        position,
        data: {
          label: selectedNodeType.label,
          nodeType: selectedNodeType.type,
          zone: selectedNetwork,
          status: "pending",
        },
        draggable: true,
      };

      setNodes((prev) => [...prev, newNode]);
    },
    [selectedNodeType, selectedNetwork, networks]
  );

  const addNetwork = () => {
    if (!newNetworkName.trim()) return;

    const id = newNetworkName.toLowerCase().replace(/\s+/g, "_");
    const colorIdx = networks.length % defaultNetworkColors.length;

    setNetworks((prev) => [
      ...prev,
      {
        id,
        name: newNetworkName,
        cidr: newNetworkCidr || "10.0.0.0/24",
        color: defaultNetworkColors[colorIdx],
      },
    ]);

    setNewNetworkName("");
    setNewNetworkCidr("");
    setShowNetworkForm(false);
  };

  const deleteNetwork = (networkId: string) => {
    // Remove network and all nodes in that network
    setNetworks((prev) => prev.filter((n) => n.id !== networkId));
    setNodes((prev) => prev.filter((n) => n.data?.zone !== networkId));
    if (selectedNetwork === networkId) {
      setSelectedNetwork(networks[0]?.id || "");
    }
  };

  const deleteNode = (nodeId: string) => {
    setNodes((prev) => prev.filter((n) => n.id !== nodeId));
    setEdges((prev) => prev.filter((e) => e.source !== nodeId && e.target !== nodeId));
  };

  const clearAll = () => {
    setNodes([]);
    setEdges([]);
  };

  const saveMutation = useMutation({
    mutationFn: () =>
      saveLabTemplate({
        name: templateName,
        description,
        topology: {
          networks: networks.map((n) => ({ name: n.id, cidr: n.cidr })),
          nodes: nodes.map((node) => ({
            id: node.id,
            name: node.data?.label ?? node.id,
            type: node.data?.nodeType ?? "node",
            networks: [node.data?.zone ?? "it_net"],
            position: node.position,
          })),
          edges: edges.map((edge) => ({ id: edge.id, source: edge.source, target: edge.target })),
        },
      }),
    onSuccess: () => {
      alert("Template saved successfully!");
    },
  });

  // Build dynamic zone colors for minimap
  const dynamicZoneColors = networks.reduce(
    (acc, net) => {
      acc[net.id] = { border: net.color, bg: `${net.color}20`, text: net.color };
      return acc;
    },
    {} as Record<string, { border: string; bg: string; text: string }>
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
        <div className="grid gap-4 md:grid-cols-2">
          <label className="space-y-1 text-sm">
            <span className="text-slate-400">Template Name</span>
            <input
              className="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-white"
              value={templateName}
              onChange={(e) => setTemplateName(e.target.value)}
            />
          </label>
          <label className="space-y-1 text-sm">
            <span className="text-slate-400">Description</span>
            <input
              className="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-white"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[320px,1fr]">
        {/* Sidebar */}
        <aside className="space-y-4 rounded-2xl border border-slate-800 bg-slate-900/70 p-4 max-h-[700px] overflow-y-auto">
          {/* Networks Section */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-300">Networks</h3>
              <button
                className="text-xs text-slate-400 hover:text-white flex items-center gap-1"
                onClick={() => setShowNetworkForm(true)}
              >
                <Plus size={14} /> Add
              </button>
            </div>

            {showNetworkForm && (
              <div className="mb-3 p-3 rounded-lg border border-slate-700 bg-slate-950 space-y-2">
                <input
                  className="w-full rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-white"
                  placeholder="Network name"
                  value={newNetworkName}
                  onChange={(e) => setNewNetworkName(e.target.value)}
                />
                <input
                  className="w-full rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-white"
                  placeholder="CIDR (e.g., 10.0.0.0/24)"
                  value={newNetworkCidr}
                  onChange={(e) => setNewNetworkCidr(e.target.value)}
                />
                <div className="flex gap-2">
                  <Button size="sm" className="flex-1" onClick={addNetwork}>
                    Add
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setShowNetworkForm(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            )}

            <div className="space-y-1">
              {networks.map((net) => (
                <div
                  key={net.id}
                  className={`flex items-center justify-between rounded-lg px-3 py-2 cursor-pointer transition-colors ${
                    selectedNetwork === net.id ? "bg-slate-800" : "hover:bg-slate-800/50"
                  }`}
                  onClick={() => setSelectedNetwork(net.id)}
                >
                  <div className="flex items-center gap-2">
                    <div
                      className="w-3 h-3 rounded-full"
                      style={{ backgroundColor: net.color }}
                    />
                    <div>
                      <p className="text-sm font-medium text-white">{net.name}</p>
                      <p className="text-xs text-slate-500">{net.cidr}</p>
                    </div>
                  </div>
                  <button
                    className="text-slate-500 hover:text-red-400 p-1"
                    onClick={(e) => {
                      e.stopPropagation();
                      deleteNetwork(net.id);
                    }}
                  >
                    <X size={14} />
                  </button>
                </div>
              ))}
            </div>
          </div>

          {/* Node Types Section */}
          <div className="border-t border-slate-800 pt-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-300 mb-2">
              Node Types
            </h3>
            <p className="text-xs text-slate-500 mb-3">
              Select a node type, then click on the canvas to place it in the selected network.
            </p>

            <div className="space-y-1">
              {nodeTypeOptions.map((opt) => (
                <button
                  key={opt.type}
                  className={`w-full rounded-lg px-3 py-2 text-left transition-colors ${
                    selectedNodeType?.type === opt.type
                      ? "bg-brand/20 border border-brand"
                      : "hover:bg-slate-800 border border-transparent"
                  }`}
                  onClick={() =>
                    setSelectedNodeType(selectedNodeType?.type === opt.type ? null : opt)
                  }
                >
                  <p className="text-sm font-medium text-white">{opt.label}</p>
                  <p className="text-xs text-slate-500">{opt.description}</p>
                </button>
              ))}
            </div>
          </div>

          {/* Actions */}
          <div className="border-t border-slate-800 pt-4 space-y-2">
            <Button className="w-full" variant="outline" onClick={clearAll} disabled={nodes.length === 0}>
              Clear All Nodes
            </Button>
            <Button
              className="w-full"
              disabled={nodes.length === 0 || saveMutation.isPending}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? "Saving..." : "Save as Template"}
            </Button>
          </div>
        </aside>

        {/* Canvas */}
        <div className="h-[700px] overflow-hidden rounded-2xl border border-slate-800 bg-slate-950 relative">
          {selectedNodeType && (
            <div className="absolute top-3 left-3 z-10 bg-slate-900/90 border border-slate-700 rounded-lg px-3 py-2 text-sm">
              <span className="text-slate-400">Placing: </span>
              <span className="text-white font-medium">{selectedNodeType.label}</span>
              <span className="text-slate-400"> in </span>
              <span style={{ color: networks.find((n) => n.id === selectedNetwork)?.color }}>
                {networks.find((n) => n.id === selectedNetwork)?.name}
              </span>
              <button
                className="ml-3 text-slate-500 hover:text-white"
                onClick={() => setSelectedNodeType(null)}
              >
                <X size={14} />
              </button>
            </div>
          )}

          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onPaneClick={onPaneClick}
            fitView
            nodesDraggable
            deleteKeyCode="Delete"
            style={{ cursor: selectedNodeType ? "crosshair" : "default" }}
          >
            <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="#1e293b" />
            <MiniMap
              pannable
              zoomable
              nodeColor={(node) => dynamicZoneColors[node.data?.zone]?.border || "#64748b"}
              maskColor="rgba(15, 23, 42, 0.8)"
              style={{ backgroundColor: "#0f172a", border: "1px solid #334155" }}
            />
            <Controls className="!bg-slate-800 !border-slate-700" />
          </ReactFlow>
        </div>
      </div>

      <p className="text-xs text-slate-500">
        1. Add networks using the + button. 2. Select a network to place nodes in. 3. Select a node type,
        then click on the canvas to place. 4. Drag nodes to reposition. 5. Connect nodes by dragging handles.
        6. Press Delete to remove selected elements.
      </p>
    </div>
  );
}
