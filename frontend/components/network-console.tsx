"use client";

import React, { useMemo, useState } from "react";
import ReactFlow, { Background, BackgroundVariant, Controls, Edge, MiniMap, Node } from "reactflow";
import "reactflow/dist/style.css";
import { useQuery } from "@tanstack/react-query";
import {
  createLabInstance,
  getLabTopology,
  listLabInstances,
  listLabTemplates,
  LabInstance,
  LabTopology
} from "../lib/api";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "./ui/button";

const zones = ["it_net", "dmz_net", "ot_control_net", "ot_safety_net"] as const;
const zoneColors: Record<string, string> = {
  it_net: "#0ea5e9",
  dmz_net: "#a855f7",
  ot_control_net: "#f97316",
  ot_safety_net: "#22c55e"
};

const proxyPaths: Record<string, string> = {
  hmi_scada: "/apps/hmi/",
  plc_trainer: "/apps/plc/",
  sis_plc: "/apps/sis/",
  grafana: "/apps/grafana/",
  jump_host: "/apps/jump/",
  ot_ids: "/apps/ids/"
};

const typeLabels: Record<string, string> = {
  ews: "EWS",
  jump_host: "Jump",
  hmi_scada: "HMI",
  plc_trainer: "PLC",
  sis_plc: "SIS",
  ot_ids: "IDS",
  historian: "Historian",
  grafana: "Grafana",
  opnsense_external: "Firewall"
};

type InspectorState = {
  id: string;
  name: string;
  type: string;
  zone: string;
  networks: string[];
};

export function NetworkConsole() {
  const { data: labsData } = useQuery({ queryKey: ["lab-instances"], queryFn: listLabInstances });
  const { data: templatesData } = useQuery({ queryKey: ["lab-templates"], queryFn: listLabTemplates });
  const initialLab = labsData?.instances?.[0];
  const [selectedLab, setSelectedLab] = useState<LabInstance | undefined>(initialLab);
  const queryClient = useQueryClient();
  const launchLab = useMutation({
    mutationFn: async () => {
      const tmpl = templatesData?.templates?.[0];
      if (!tmpl) throw new Error("No templates available");
      const name = `Console Lab ${new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
      return createLabInstance(tmpl.id, name);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["lab-instances"] });
    }
  });

  const { data: topology } = useQuery({
    queryKey: ["lab", selectedLab?.id, "topology"],
    queryFn: () => (selectedLab ? getLabTopology(selectedLab.id) : Promise.resolve(undefined)),
    enabled: Boolean(selectedLab?.id)
  });

  const [inspector, setInspector] = useState<InspectorState | null>(null);
  const [iframeUrl, setIframeUrl] = useState<string | null>(null);

  const { nodes, edges } = useTopologyGraph(topology);

  return (
    <div className="space-y-4">
      <header className="flex items-center justify-between">
        <div>
          <p className="text-xs uppercase tracking-[0.3em] text-slate-400">Live Console</p>
          <h1 className="text-3xl font-semibold text-white">Interactive OT Map</h1>
          <p className="text-sm text-slate-400">Click nodes to inspect; open UIs inlined via the proxy.</p>
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
                setInspector(null);
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
              disabled={launchLab.isPending || !templatesData?.templates?.length}
              onClick={() => launchLab.mutate()}
            >
              {launchLab.isPending ? "Launching..." : "Start default lab"}
            </Button>
          )}
        </div>
      </header>

      <div className="grid gap-4 md:grid-cols-[2fr,1fr]">
        <div className="h-[600px] overflow-hidden rounded-2xl border border-slate-800 bg-slate-950">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            fitView
            nodesDraggable
            onNodeClick={(_, node) => {
              const zone = node.data?.zone ?? "";
              const networks = (node.data?.networks as string[]) ?? [];
              setInspector({ id: node.id, name: node.data?.label ?? node.id, type: node.data?.type, zone, networks });
            }}
          >
            <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
            <MiniMap pannable zoomable />
            <Controls />
          </ReactFlow>
        </div>

        <div className="rounded-2xl border border-slate-800 bg-slate-900/70 p-4">
          <h2 className="text-lg font-semibold text-white">Inspector</h2>
          {inspector ? (
            <div className="mt-3 space-y-2 text-sm text-slate-200">
              <div className="flex justify-between"><span className="text-slate-400">Name</span><span>{inspector.name}</span></div>
              <div className="flex justify-between"><span className="text-slate-400">Type</span><span className="uppercase">{inspector.type}</span></div>
              <div className="flex justify-between"><span className="text-slate-400">Zone</span><span>{inspector.zone}</span></div>
              <div className="flex justify-between"><span className="text-slate-400">Networks</span><span>{inspector.networks.join(", ")}</span></div>
              <Button
                className="w-full mt-3"
                variant="outline"
                disabled={!proxyPaths[inspector.type]}
                onClick={() => {
                  const path = proxyPaths[inspector.type];
                  if (path) setIframeUrl(path);
                }}
              >
                {proxyPaths[inspector.type] ? "Open Embedded UI" : "No UI Available"}
              </Button>
            </div>
          ) : (
            <p className="mt-3 text-slate-400">Select a node to view details.</p>
          )}
        </div>
      </div>

      {iframeUrl && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/80 backdrop-blur">
          <div className="w-11/12 max-w-5xl overflow-hidden rounded-2xl border border-slate-800 bg-slate-950 shadow-2xl">
            <div className="flex items-center justify-between border-b border-slate-800 px-4 py-3 text-sm text-slate-200">
              <span>Embedded UI: {iframeUrl}</span>
              <Button size="sm" variant="ghost" onClick={() => setIframeUrl(null)}>
                Close
              </Button>
            </div>
            <div className="h-[70vh] bg-black">
              <iframe title="Embedded App" src={iframeUrl} className="h-full w-full border-0" allowFullScreen />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function useTopologyGraph(topology?: LabTopology) {
  return useMemo(() => {
    if (!topology || !topology.nodes) {
      return { nodes: [] as Node[], edges: [] as Edge[] };
    }

    const graphNodes: Node[] = [];
    const graphEdges: Edge[] = [];
    const zoneCounts: Record<string, number> = {};

    zones.forEach((zone, index) => {
      graphNodes.push({
        id: `zone-${zone}`,
        position: { x: index * 240, y: 0 },
        data: { label: zone.toUpperCase(), zone },
        type: "input",
        style: {
          border: `1px solid ${zoneColors[zone]}`,
          color: zoneColors[zone],
          background: "#020617",
          padding: 8,
          borderRadius: 10
        }
      });
    });

    topology.nodes.forEach((node) => {
      const zone = node.networks?.[0] ?? "it_net";
      const columnIndex = zones.indexOf(zone as typeof zones[number]);
      const columnX = columnIndex >= 0 ? columnIndex * 240 : 0;
      const count = zoneCounts[zone] ?? 0;
      zoneCounts[zone] = count + 1;

      const label = `${typeLabels[node.type] ?? node.type} - ${node.name}`;

      graphNodes.push({
        id: node.id,
        position: { x: columnX, y: 140 + count * 140 },
        data: { label, type: node.type, zone, networks: node.networks },
        style: {
          border: `2px solid ${zoneColors[zone] ?? "#94a3b8"}`,
          background: "linear-gradient(135deg, #0f172a 0%, #0b1220 100%)",
          color: "#e2e8f0",
          borderRadius: 14,
          padding: 12,
          width: 220,
          boxShadow: "0 10px 30px rgba(0,0,0,0.35)"
        }
      });

      if (zones.includes(zone as typeof zones[number])) {
        graphEdges.push({
          id: `edge-${zone}-${node.id}`,
          source: `zone-${zone}`,
          target: node.id,
          animated: zone === "it_net",
          style: { stroke: zoneColors[zone] ?? "#94a3b8" }
        });
      }
    });

    return { nodes: graphNodes, edges: graphEdges };
  }, [topology]);
}
