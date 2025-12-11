"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import ReactFlow, { Background, BackgroundVariant, Controls, Edge, MiniMap, Node } from "reactflow";
import "reactflow/dist/style.css";

import { getLabInstance, getLabTopology, LabTopology, startLabInstance, stopLabInstance } from "../lib/api";
import { Button } from "./ui/button";
import { ScenarioList } from "./scenario-list";
import { MetricsOverview } from "./metrics-overview";

const zones = ["it_net", "dmz_net", "ot_control_net", "ot_safety_net"] as const;
const zoneColors: Record<string, string> = {
  it_net: "#38bdf8",
  dmz_net: "#a855f7",
  ot_control_net: "#f97316",
  ot_safety_net: "#22c55e"
};

type LabDetailProps = {
  labId: string;
};

export function LabDetail({ labId }: LabDetailProps) {
  const [activeTab, setActiveTab] = useState<"topology" | "scenarios" | "metrics" | "nodes">("topology");
  const queryClient = useQueryClient();
  const { data: lab, isLoading } = useQuery({ queryKey: ["lab", labId], queryFn: () => getLabInstance(labId) });
  const { data: topology } = useQuery({ queryKey: ["lab", labId, "topology"], queryFn: () => getLabTopology(labId) });
  const startMutation = useMutation({
    mutationFn: () => startLabInstance(labId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["lab", labId] });
      queryClient.invalidateQueries({ queryKey: ["lab-instances"] });
    }
  });

  const stopMutation = useMutation({
    mutationFn: () => stopLabInstance(labId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["lab", labId] });
      queryClient.invalidateQueries({ queryKey: ["lab-instances"] });
    }
  });

  const { nodes, edges } = useLabTopologyGraph(topology);

  if (isLoading || !lab) {
    return <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-8">Loading lab...</div>;
  }

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between rounded-2xl border border-slate-800 bg-slate-900/70 p-6">
        <div>
          <p className="text-sm uppercase tracking-wide text-slate-500">Lab Instance</p>
          <h1 className="text-3xl font-semibold text-white">{lab.name || lab.id}</h1>
          <p className="text-sm text-slate-400">Template: {lab.template_id}</p>
        </div>
        <div className="flex items-center gap-3">
          <span className="rounded-full border border-slate-700 px-3 py-1 text-xs uppercase tracking-wide text-slate-200">
            {lab.status}
          </span>
          <Button
            variant="outline"
            disabled={lab.status === "running" || startMutation.isPending}
            onClick={() => startMutation.mutate()}
          >
            {startMutation.isPending ? "Starting..." : "Start"}
          </Button>
          <Button variant="outline" disabled={lab.status !== "running" || stopMutation.isPending} onClick={() => stopMutation.mutate()}>
            {stopMutation.isPending ? "Stopping..." : "Stop"}
          </Button>
        </div>
      </header>

      <nav className="flex gap-4 text-sm">
        {(
          [
            { id: "topology", label: "Topology" },
            { id: "scenarios", label: "Scenarios" },
            { id: "metrics", label: "Metrics" },
            { id: "nodes", label: "Nodes" }
          ] as const
        ).map((tab) => (
          <button
            key={tab.id}
            className={`rounded-full px-4 py-2 ${activeTab === tab.id ? "bg-slate-100 text-slate-900" : "bg-slate-800 text-slate-300"}`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </nav>

      {activeTab === "topology" && (
        <div className="h-[500px] overflow-hidden rounded-2xl border border-slate-800 bg-slate-950">
          <ReactFlow nodes={nodes} edges={edges} fitView nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}>
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
            <MiniMap pannable zoomable />
            <Controls />
          </ReactFlow>
        </div>
      )}

      {activeTab === "scenarios" && <ScenarioList templateId={lab.template_id} />}

      {activeTab === "metrics" && <MetricsOverview />}

      {activeTab === "nodes" && (
        <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-950">
          <table className="w-full text-sm">
            <thead className="bg-slate-900 text-slate-400">
              <tr>
                <th className="px-4 py-3 text-left font-semibold">Name</th>
                <th className="px-4 py-3 text-left font-semibold">Type</th>
                <th className="px-4 py-3 text-left font-semibold">IP</th>
                <th className="px-4 py-3 text-left font-semibold">Status</th>
              </tr>
            </thead>
            <tbody>
              {lab.nodes?.map((node) => (
                <tr key={node.id} className="border-t border-slate-900 text-slate-200">
                  <td className="px-4 py-3">{node.name}</td>
                  <td className="px-4 py-3 uppercase text-slate-400">{node.type}</td>
                  <td className="px-4 py-3 text-slate-300">{node.ip}</td>
                  <td className="px-4 py-3">
                    <span className="rounded-full bg-slate-800 px-3 py-1 text-xs uppercase tracking-wide text-slate-200">
                      {node.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function useLabTopologyGraph(topology?: LabTopology) {
  return useMemo(() => {
    if (!topology || !topology.nodes) {
      return { nodes: [] as Node[], edges: [] as Edge[] };
    }
    const graphNodes: Node[] = [];
    const graphEdges: Edge[] = [];
    const zoneCounts: Record<string, number> = {};

    zones.forEach((zone, index) => {
      graphNodes.push({
        id: `net-${zone}`,
        position: { x: index * 220, y: 0 },
        data: { label: zone.replace("_", " ").toUpperCase() },
        type: "input",
        style: {
          border: `1px solid ${zoneColors[zone]}`,
          color: zoneColors[zone],
          background: "#020617"
        }
      });
    });

    topology.nodes.forEach((node) => {
      const zone = node.networks?.[0] ?? "it_net";
      const columnIndex = zones.indexOf(zone as typeof zones[number]);
      const columnX = columnIndex >= 0 ? columnIndex * 220 : 0;
      const count = zoneCounts[zone] ?? 0;
      zoneCounts[zone] = count + 1;

      graphNodes.push({
        id: node.id,
        position: { x: columnX, y: 140 + count * 120 },
        data: { label: `${node.name}\n${node.type.toUpperCase()}` },
        style: {
          border: `2px solid ${zoneColors[zone] ?? "#94a3b8"}`,
          background: "#0f172a",
          color: "#f8fafc",
          borderRadius: 12,
          padding: 12,
          width: 180,
          whiteSpace: "pre-line"
        }
      });

      if (zones.includes(zone as typeof zones[number])) {
        graphEdges.push({
          id: `edge-${zone}-${node.id}`,
          source: `net-${zone}`,
          target: node.id,
          animated: zone === "it_net"
        });
      }
    });

    return { nodes: graphNodes, edges: graphEdges };
  }, [topology]);
}
