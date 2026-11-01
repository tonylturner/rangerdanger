// Package-owned flow inventory, normalized evidence and shared pair aggregation.
// Neither presentation interprets binding keys or substitutes a default inventory.
import type { LabGraph, TopologyFlow, TrafficStatus, WorkshopStatus } from "./api";
import type { FirewallSessionList, OperatorState } from "./operator-api";

export type ObservedFlow = TopologyFlow;
export type FlowCategory = ObservedFlow["category"];
export type FlowStatus = "active" | "idle" | "down" | "unknown";
export type FlowObservation = {
  status: FlowStatus; source: string; reason: string; observedAt?: number; evidence: string;
};
export type FlowContext = { graph: LabGraph; observations: Record<string, FlowObservation>; traffic?: TrafficStatus };
export type FlowInputs = {
  workshop?: WorkshopStatus; traffic?: TrafficStatus; operator?: OperatorState;
  sessions?: FirewallSessionList; observedAt?: number;
};

export function nodeZone(nodeId: string, graph: LabGraph): string {
  return graph.nodes.find((n) => n.id === nodeId)?.data.zone || "unknown";
}
export function nodeIp(nodeId: string, graph: LabGraph): string | undefined {
  const n = graph.nodes.find((n) => n.id === nodeId);
  return n?.data.interface_ips?.[n.data.zone] ?? n?.data.ip;
}
export function isCrossZone(source: string, target: string, graph: LabGraph): boolean {
  const a = nodeZone(source, graph), b = nodeZone(target, graph);
  return a !== b && a !== "unknown" && b !== "unknown";
}

export function createFlowContext(graph: LabGraph, inputs: FlowInputs, now = Date.now()): FlowContext {
  const observations: Record<string, FlowObservation> = {};
  for (const flow of graph.flows) {
    const b = graph.liveness[flow.liveness_key];
    let status: FlowStatus = "unknown", reason = "Not observed", evidence = "unobserved";
    switch (b.provider) {
      case "workshop-supervisor":
        status = inputs.workshop?.rtac_online ? "active" : "down";
        reason = "Supervisory online state"; evidence = "workshop status";
        break;
      case "workshop-device":
        status = inputs.workshop?.rtac_online && inputs.workshop.device_comms?.[b.device] ? "active" : "down";
        reason = "Device comms gated by supervisor"; evidence = "workshop device comms";
        break;
      case "traffic-generator": {
        const started = Date.parse(inputs.traffic?.started_at ?? "");
        status = inputs.traffic?.generating || (Number.isFinite(started) && now - started < 60_000) ? "active" : "idle";
        reason = "Generator state (60 s started-at tail)"; evidence = "generator status";
        break;
      }
      case "operator-link":
        if (inputs.operator) {
          status = inputs.operator.link.state === "started" ? "active" : inputs.operator.link.state === "disconnected" ? "down" : "idle";
          reason = `IEC104 ${inputs.operator.link.state}; GI ${inputs.operator.link.gi.state} (connect / 15 min / operator); spontaneous status, cyclic/deadband measurands`;
          evidence = "control-centre link";
        }
        break;
      case "operator-device": {
        // Quiet status does not age out; float reporting has an upstream watchdog.
        // Only a current upstream observation can support downstream inference.
        if (!inputs.operator || inputs.operator.link.state !== "started") {
          reason = "Downstream unknown without a current started CC link";
          break;
        }
        const points = b.points.map((id) => inputs.operator!.points.find((p) => p.id === id));
        if (points.some((p) => p?.stale_reason === "device_unreachable")) {
          status = "down"; reason = "Device unreachable reported by RTU";
        } else if (points.every((p) => p && p.received_at && p.value !== null && !p.quality.iv && !p.quality.nt && p.stale_reason === null)) {
          status = "active"; reason = "Downstream polling inferred from received status points";
        } else {
          reason = "Missing or invalid status points do not establish transport failure";
        }
        evidence = "inferred from CC status point quality";
        break;
      }
      case "session-presence": {
        if (!inputs.sessions) break;
        const src = nodeIp(flow.source, graph), dst = nodeIp(flow.target, graph);
        if (!src || !dst) { reason = "Missing endpoint address"; break; }
        const session = inputs.sessions.sessions.find((s) => s.proto.toLowerCase() === b.transport && s.src === src && s.dst === dst && s.dport === flow.port);
        if (session) {
          status = "unknown"; reason = `Conntrack entry present (${session.state ?? "state unknown"}); traffic may already be blocked; not application success or policy authorization`;
        } else if (inputs.sessions.sessions.some((s) => s.proto.toLowerCase() === b.transport && s.src === src && s.dst === dst && s.dport === null)) {
          reason = "Incomplete session tuple; destination port unknown";
        } else if (inputs.sessions.count >= 5000) {
          reason = "Session list may be capped; absence is inconclusive";
        } else {
          status = "idle"; reason = "No matching original-direction session; not proof of failure";
        }
        evidence = "conntrack transport presence";
        break;
      }
      case "unobserved": break;
    }
    observations[flow.id] = { status, source: b.provider, reason, observedAt: b.provider === "operator-link" || b.provider === "operator-device" ? inputs.observedAt : undefined, evidence };
  }
  return { graph, observations, traffic: inputs.traffic };
}

export type FlowPair = {
  source: string; target: string; protocols: ObservedFlow[]; status: FlowStatus;
  category: FlowCategory; crossZone: boolean;
};
export type FlowFilter = { zone: string; crossZoneOnly: boolean };
export function aggregateFlows(context: FlowContext, filter?: FlowFilter): FlowPair[] {
  const { graph, observations } = context;
  const groups = new Map<string, FlowPair>();
  const nodes = new Set(graph.nodes.map((n) => n.id));
  for (const f of graph.flows) {
    if (!nodes.has(f.source) || !nodes.has(f.target)) continue;
    const crossZone = isCrossZone(f.source, f.target, graph);
    if (filter?.crossZoneOnly && !crossZone) continue;
    if (filter && filter.zone !== "all" && nodeZone(f.source, graph) !== filter.zone && nodeZone(f.target, graph) !== filter.zone) continue;
    const key = `${f.source}->${f.target}`;
    if (!groups.has(key)) groups.set(key, { source: f.source, target: f.target, protocols: [], status: "unknown", category: f.category, crossZone });
    groups.get(key)!.protocols.push(f);
  }
  for (const row of groups.values()) {
    const statuses = row.protocols.map((f) => observations[f.id]?.status ?? "unknown");
    row.status = statuses.includes("active") ? "active" : statuses.every((s) => s === "down") ? "down" : statuses.includes("unknown") ? "unknown" : "idle";
  }
  const order: Record<FlowStatus, number> = { active: 0, idle: 1, down: 2, unknown: 3 };
  return [...groups.values()].sort((a, b) => order[a.status] - order[b.status] || Number(b.crossZone) - Number(a.crossZone) || a.source.localeCompare(b.source));
}

// Cached CC observations from a failed read, old generation or stale poll must
// never turn into an affirmative downstream-health claim.
export function currentOperatorObservation(data: OperatorState | undefined, failed: boolean, updatedAt: number,
  identity: { package: string; generation: number; phase: string } | undefined, now: number): OperatorState | undefined {
  return !failed && identity?.phase === "ready" && now - updatedAt < 15_000 &&
    data?.package_id === identity.package && data?.range_generation === identity.generation ? data : undefined;
}
