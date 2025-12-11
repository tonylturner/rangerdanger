"use client";

import { useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  Edge,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  Connection,
  MiniMap,
  Node,
  NodeChange,
  EdgeChange
} from "reactflow";
import "reactflow/dist/style.css";
import { useMutation } from "@tanstack/react-query";

import { saveLabTemplate } from "../lib/api";
import { Button } from "./ui/button";

const palette = [
  { type: "ews", label: "Engineering WS", zone: "it_net", description: "noVNC desktop & PLC IDE" },
  { type: "jump_host", label: "Jump Host", zone: "it_net", description: "Kali-lite pentest box" },
  { type: "hmi_scada", label: "HMI / SCADA", zone: "dmz_net", description: "FUXA HMI server" },
  { type: "historian", label: "Historian", zone: "dmz_net", description: "InfluxDB + Grafana" },
  { type: "ot_ids", label: "Suricata IDS", zone: "dmz_net", description: "Sensor with ICS rules" },
  { type: "plc_trainer", label: "Process PLC", zone: "ot_control_net", description: "OpenPLC runtime" },
  { type: "sis_plc", label: "Safety PLC", zone: "ot_safety_net", description: "SIS logic" }
];

const zoneX: Record<string, number> = {
  it_net: 0,
  dmz_net: 250,
  ot_control_net: 500,
  ot_safety_net: 750
};

const zoneColors: Record<string, string> = {
  it_net: "#38bdf8",
  dmz_net: "#a855f7",
  ot_control_net: "#f97316",
  ot_safety_net: "#22c55e"
};

const generateNodeId = () => {
  const globalCrypto = typeof globalThis !== "undefined" ? globalThis.crypto : undefined;
  if (globalCrypto?.randomUUID) {
    return globalCrypto.randomUUID();
  }
  return `node-${Math.random().toString(36).slice(2, 9)}`;
};

export function TopologyBuilder() {
  const [templateName, setTemplateName] = useState("Custom OT Lab");
  const [description, setDescription] = useState("Visual builder for quick lab templates.");
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [zoneCounts, setZoneCounts] = useState<Record<string, number>>({});

  const onNodesChange = (changes: NodeChange[]) => setNodes((nds) => applyNodeChanges(changes, nds));
  const onEdgesChange = (changes: EdgeChange[]) => setEdges((eds) => applyEdgeChanges(changes, eds));
  const onConnect = (connection: Connection) => setEdges((eds) => addEdge(connection, eds));

  const createNode = (type: string, zone: string, label: string) => {
    const id = generateNodeId();
    const count = zoneCounts[zone] ?? 0;
    const updatedCounts = { ...zoneCounts, [zone]: count + 1 };
    setZoneCounts(updatedCounts);

    const position = {
      x: zoneX[zone] ?? 0,
      y: 80 + count * 140
    };

    const newNode: Node = {
      id,
      position,
      data: { label, type, zone },
      style: {
        border: `2px solid ${zoneColors[zone] ?? "#94a3b8"}`,
        background: "#0f172a",
        color: "#f8fafc",
        padding: 12,
        borderRadius: 12,
        width: 200
      }
    };
    setNodes((prev) => [...prev, newNode]);
  };

  const saveMutation = useMutation({
    mutationFn: () =>
      saveLabTemplate({
        name: templateName,
        description,
        topology: {
          networks: [
            { name: "it_net", cidr: "10.10.10.0/24" },
            { name: "dmz_net", cidr: "10.20.20.0/24" },
            { name: "ot_control_net", cidr: "10.30.30.0/24" },
            { name: "ot_safety_net", cidr: "10.40.40.0/24" }
          ],
          nodes: nodes.map((node) => ({
            id: node.id,
            name: node.data?.label ?? node.id,
            type: node.data?.type ?? "node",
            networks: [node.data?.zone ?? "it_net"],
            position: node.position
          })),
          edges: edges.map((edge) => ({ id: edge.id, source: edge.source, target: edge.target }))
        }
      }),
    onSuccess: () => {
      setNodes([]);
      setEdges([]);
      setZoneCounts({});
    }
  });

  return (
    <div className="space-y-6">
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

      <div className="grid gap-6 md:grid-cols-[260px,1fr]">
        <aside className="space-y-4 rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
          <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-300">Node Palette</h3>
          <div className="space-y-3">
            {palette.map((item) => (
              <div key={item.type} className="rounded-xl border border-slate-800 bg-slate-950 p-3 text-sm text-slate-300">
                <p className="font-semibold text-white">{item.label}</p>
                <p className="text-xs text-slate-400">{item.description}</p>
                <Button className="mt-2 w-full" size="sm" variant="outline" onClick={() => createNode(item.type, item.zone, item.label)}>
                  Add to {item.zone.replace("_", " ")}
                </Button>
              </div>
            ))}
          </div>
          <Button
            className="w-full"
            disabled={nodes.length === 0 || saveMutation.isLoading}
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isLoading ? "Saving..." : "Save Template"}
          </Button>
        </aside>

        <div className="h-[520px] overflow-hidden rounded-2xl border border-slate-800 bg-slate-950">
          <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange} onConnect={onConnect} fitView>
            <Background variant="dots" gap={16} size={1} />
            <MiniMap pannable zoomable />
            <Controls />
          </ReactFlow>
        </div>
      </div>
    </div>
  );
}
