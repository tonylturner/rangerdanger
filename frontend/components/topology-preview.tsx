"use client";

import ReactFlow, { Background, BackgroundVariant, Controls, MiniMap, Node, Edge } from "reactflow";
import "reactflow/dist/style.css";

const initialNodes: Node[] = [
  { id: "ews", position: { x: 0, y: 100 }, data: { label: "EWS" }, type: "input" },
  { id: "hmi", position: { x: 200, y: 0 }, data: { label: "HMI" } },
  { id: "plc", position: { x: 400, y: 100 }, data: { label: "PLC" } },
  { id: "sis", position: { x: 400, y: 250 }, data: { label: "SIS" } }
];

const initialEdges: Edge[] = [
  { id: "e1", source: "ews", target: "hmi", animated: true },
  { id: "e2", source: "hmi", target: "plc" },
  { id: "e3", source: "plc", target: "sis" }
];

export function TopologyPreview() {
  return (
    <div className="h-80 rounded-2xl border border-slate-800 bg-slate-950">
      <ReactFlow nodes={initialNodes} edges={initialEdges} fitView nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}>
        <Background variant={BackgroundVariant.Dots} />
        <MiniMap pannable zoomable />
        <Controls />
      </ReactFlow>
    </div>
  );
}
