"use client";

import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  Node,
  Edge,
} from "reactflow";
import "reactflow/dist/style.css";

const zoneY: Record<string, number> = {
  enterprise: 0,
  vendor: 0,
  ot_ops: 0,
  field: 0,
  physics: 0,
};

const zoneX: Record<string, number> = {
  enterprise: 0,
  vendor: 280,
  ot_ops: 560,
  field: 840,
  physics: 1120,
};

const zoneColors: Record<string, string> = {
  enterprise: "#38bdf8",
  vendor: "#a855f7",
  ot_ops: "#f97316",
  field: "#22c55e",
  physics: "#64748b",
};

const nodes: Node[] = [
  // Zone labels
  { id: "z-enterprise", position: { x: 0, y: -30 }, data: { label: "Enterprise (10.10.10.0/24)" }, type: "input", style: { background: "#0c1324", border: "1px solid #38bdf8", color: "#38bdf8", fontSize: 10, padding: "4px 8px", borderRadius: 6, width: 200 } },
  { id: "z-vendor", position: { x: 280, y: -30 }, data: { label: "Vendor (10.20.20.0/24)" }, type: "input", style: { background: "#0c1324", border: "1px solid #a855f7", color: "#a855f7", fontSize: 10, padding: "4px 8px", borderRadius: 6, width: 200 } },
  { id: "z-otops", position: { x: 560, y: -30 }, data: { label: "OT Ops (10.30.30.0/24)" }, type: "input", style: { background: "#0c1324", border: "1px solid #f97316", color: "#f97316", fontSize: 10, padding: "4px 8px", borderRadius: 6, width: 200 } },
  { id: "z-field", position: { x: 840, y: -30 }, data: { label: "Field (10.40.40.0/24)" }, type: "input", style: { background: "#0c1324", border: "1px solid #22c55e", color: "#22c55e", fontSize: 10, padding: "4px 8px", borderRadius: 6, width: 200 } },
  { id: "z-physics", position: { x: 1120, y: -30 }, data: { label: "Physics (10.50.50.0/24)" }, type: "input", style: { background: "#0c1324", border: "1px solid #64748b", color: "#64748b", fontSize: 10, padding: "4px 8px", borderRadius: 6, width: 180 } },

  // Firewall (spanning zones)
  { id: "fw", position: { x: 420, y: 60 }, data: { label: "containd NGFW" }, style: { background: "#1e1b4b", border: "2px solid #f59e0b", color: "#fbbf24", fontSize: 11, fontWeight: 700, padding: "6px 12px", borderRadius: 8, width: 160 } },

  // Enterprise zone
  { id: "corp-ws", position: { x: 0, y: 140 }, data: { label: "Corp WS\n.10" }, style: nodeStyle("#38bdf8") },
  { id: "kali", position: { x: 0, y: 240 }, data: { label: "Kali\n.50" }, style: nodeStyle("#ef4444") },

  // Vendor zone
  { id: "vendor-jump", position: { x: 280, y: 140 }, data: { label: "Vendor Jump\n.10" }, style: nodeStyle("#a855f7") },
  { id: "eng-ws", position: { x: 280, y: 240 }, data: { label: "Eng WS\n.20" }, style: nodeStyle("#a855f7") },

  // OT Ops zone
  { id: "hmi", position: { x: 560, y: 140 }, data: { label: "FUXA HMI\n.10" }, style: nodeStyle("#f97316") },
  { id: "rtac", position: { x: 560, y: 240 }, data: { label: "RTAC\n.20" }, style: nodeStyle("#f97316") },
  { id: "openplc", position: { x: 560, y: 340 }, data: { label: "OpenPLC\n.30" }, style: nodeStyle("#f97316") },

  // Field zone
  { id: "relay", position: { x: 840, y: 140 }, data: { label: "Relay 52\n.20" }, style: nodeStyle("#22c55e") },
  { id: "recloser", position: { x: 840, y: 240 }, data: { label: "Recloser 79\n.21" }, style: nodeStyle("#22c55e") },
  { id: "regulator", position: { x: 840, y: 340 }, data: { label: "Regulator 90\n.22" }, style: nodeStyle("#22c55e") },

  // Physics zone
  { id: "opendss", position: { x: 1120, y: 240 }, data: { label: "OpenDSS\n.20" }, style: nodeStyle("#64748b") },
];

const edges: Edge[] = [
  // Firewall connections to zones
  { id: "fw-e", source: "fw", target: "z-enterprise", animated: true, style: { stroke: "#38bdf8", strokeWidth: 1 } },
  { id: "fw-v", source: "fw", target: "z-vendor", animated: true, style: { stroke: "#a855f7", strokeWidth: 1 } },
  { id: "fw-o", source: "fw", target: "z-otops", animated: true, style: { stroke: "#f97316", strokeWidth: 1 } },
  { id: "fw-f", source: "fw", target: "z-field", animated: true, style: { stroke: "#22c55e", strokeWidth: 1 } },

  // RTAC multi-homed (crosses into field + physics)
  { id: "rtac-relay", source: "rtac", target: "relay", animated: true, style: { stroke: "#22c55e", strokeWidth: 2 }, label: "Modbus" },
  { id: "rtac-recloser", source: "rtac", target: "recloser", animated: true, style: { stroke: "#22c55e", strokeWidth: 2 } },
  { id: "rtac-regulator", source: "rtac", target: "regulator", animated: true, style: { stroke: "#22c55e", strokeWidth: 2 } },
  { id: "rtac-physics", source: "rtac", target: "opendss", style: { stroke: "#64748b", strokeWidth: 1.5 } },
];

function nodeStyle(color: string): React.CSSProperties {
  return {
    background: "#0f172a",
    border: `1px solid ${color}`,
    color: "#e2e8f0",
    fontSize: 10,
    padding: "4px 8px",
    borderRadius: 6,
    width: 120,
    textAlign: "center" as const,
    whiteSpace: "pre-line" as const,
    lineHeight: "1.3",
  };
}

export function TopologyPreview() {
  return (
    <div className="rounded-2xl border border-slate-800 bg-slate-950" style={{ height: 440 }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} color="#1e293b" gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
