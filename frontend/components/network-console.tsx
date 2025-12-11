"use client";

import React, { useEffect, useMemo, useState } from "react";
import ReactFlow, { Background, BackgroundVariant, Controls, Edge, MiniMap, Node } from "reactflow";
import "reactflow/dist/style.css";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createLabInstance,
  getLabGraph,
  listLabInstances,
  listLabTemplates,
  seedTemplates,
  LabGraph,
  LabInstance
} from "../lib/api";
import { Button } from "./ui/button";

const zones = ["it_net", "dmz_net", "ot_control_net", "ot_safety_net"] as const;
const zoneColors: Record<string, string> = {
  it_net: "#22d3ee",
  dmz_net: "#a855f7",
  ot_control_net: "#fb923c",
  ot_safety_net: "#22c55e"
};

const typeIcons: Record<string, string> = {
  ews: "🖥️",
  jump_host: "🛠️",
  hmi_scada: "📊",
  plc_trainer: "🤖",
  sis_plc: "🛡️",
  ot_ids: "🛰️",
  historian: "🗄️",
  grafana: "📈",
  opnsense_external: "🧱"
};

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

  const { nodes, edges } = useStyledGraph(graph);

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
          fitView
          nodesDraggable
          onNodeClick={(_, node) => {
            if (node.id.startsWith("zone-")) return;
            setInspectorNode(node);
            setIframeUrl(null);
          }}
        >
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
          <MiniMap pannable zoomable />
          <Controls />
        </ReactFlow>
        {(graphLoading || labsLoading) && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-slate-950/60 text-slate-200">
            Loading console…
          </div>
        )}
      </div>

      {inspectorNode && (
        <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-md border-l border-slate-800 bg-slate-900/90 px-5 py-6 shadow-2xl backdrop-blur">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-xs uppercase tracking-[0.25em] text-slate-400">{inspectorNode.data?.zone}</p>
              <h3 className="text-xl font-semibold text-white">{inspectorNode.data?.label}</h3>
              <p className="text-sm text-slate-400">{inspectorNode.type}</p>
            </div>
            <Button variant="ghost" size="sm" onClick={() => { setInspectorNode(null); setIframeUrl(null); }}>
              Close
            </Button>
          </div>

          <div className="mt-4 space-y-3 text-sm text-slate-200">
            <InfoRow label="Networks" value={(inspectorNode.data?.networks || []).join(", ")} />
            <InfoRow label="Status" value={inspectorNode.data?.status || "unknown"} />
            <InfoRow label="IP" value={inspectorNode.data?.ip || "N/A"} />
          </div>

          <div className="mt-6 space-y-3">
            <Button
              className="w-full"
              variant="outline"
              disabled={!inspectorNode.data?.ui_path}
              onClick={() => {
                if (inspectorNode.data?.ui_path) {
                  setIframeUrl(inspectorNode.data.ui_path);
                }
              }}
            >
              {inspectorNode.data?.ui_path ? "Open UI" : "No UI available"}
            </Button>
            <Button className="w-full" variant="outline" disabled>
              Open Terminal (coming soon)
            </Button>
          </div>

          <div className="mt-6 h-[320px] overflow-hidden rounded-xl border border-slate-800 bg-black">
            {iframeUrl ? (
              <iframe title="Embedded UI" src={iframeUrl} className="h-full w-full border-0" />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-slate-500">
                Launch a UI to view it here.
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

    const styledNodes: Node[] = graph.nodes.map((n) => {
      const color = zoneColors[n.data.zone] ?? "#94a3b8";
      const icon = typeIcons[n.type] ?? "🛰️";
      return {
        id: n.id,
        type: "default",
        position: n.position,
        data: {
          ...n.data,
          label: `${icon} ${n.data.label || n.id}`
        },
        style: {
          border: `2px solid ${color}`,
          background: "linear-gradient(135deg, #0f172a 0%, #0b1220 100%)",
          color: "#e2e8f0",
          borderRadius: 14,
          padding: 12,
          width: 230,
          boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
          fontSize: 14
        }
      };
    });

    const styledEdges: Edge[] = graph.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      label: e.label,
      style: { stroke: zoneColors[e.label ?? ""] ?? "#64748b" }
    }));

    return { nodes: styledNodes, edges: styledEdges };
  }, [graph]);
}
