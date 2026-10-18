import { describe, expect, it } from "vitest";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import type { LabGraph, TrafficStatus, WorkshopStatus } from "./api";
import type { OperatorState, StaleReason } from "./operator-api";
import { aggregateFlows, createFlowContext, currentOperatorObservation, nodeIp, nodeZone } from "./observed-flows";

const eu = euFixture as LabGraph;
const us = usFixture as LabGraph;
const now = Date.parse("2026-10-15T12:00:00Z");
const workshop: WorkshopStatus = {
  workshop_id: "us", workshop_name: "US", firewall_online: true, firewall_config: "weak", scenario_count: 7,
  rtac_online: true, device_comms: { relay: true, regulator: false, recloser: true, capbank: true },
};
function operator(): OperatorState {
  return {
    schema: 3, snapshot_seq: 1, rendered_at: new Date(now).toISOString(), cache_epoch: "cc-boot", package_id: "eu-iec104-substation", range_generation: 7,
    link: { state: "started", peer: "10.30.30.20:2404", since: null, last_rx_at: null, connection_seq: 1,
      gi: { state: "complete", started_at: null, trigger: "connect", last_complete_at: new Date(now).toISOString(), next_due_at: new Date(now + 900000).toISOString() } },
    points: ["feeder-breaker-position", "feeder-protection-trip"].map((id) => ({
      id, device: "feeder-breaker", limits: null, ioa: 1, kind: "single-point", reporting: null, asdu: "M_SP_TB_1", value: false, quality: { iv: false, nt: false, sb: false, bl: false, ov: null },
      stale_reason: null, source_time: null, source_time_invalid: null,
      received_at: "2020-01-01T00:00:00Z", cot: 3,
    })),
  };
}

describe("package flow liveness", () => {
  it("keeps all 27 US rows, supervisor gating and device comms", () => {
    const context = createFlowContext(us, { workshop }, now);
    expect(us.flows).toHaveLength(27);
    expect(context.observations["rtac-1-relay-1-modbus"].status).toBe("active");
    expect(context.observations["rtac-1-regulator-1-dnp3"].status).toBe("down");
    expect(context.observations["gps-1-relay-1-ntp"].status).toBe("active");
    const down = createFlowContext(us, { workshop: { ...workshop, rtac_online: false } }, now);
    expect(us.flows.filter((f) => f.category === "baseline").map((f) => down.observations[f.id].status)).toEqual(Array(14).fill("down"));
    expect(nodeZone("gps-1", us)).toBe("ot_ops_net");
    expect(nodeIp("rtac-1", us)).toBe("10.30.30.20");
  });

  it("preserves the generator's 60 second started-at tail exactly", () => {
    const id = "eng-ws-1-relay-1-modbus";
    const status = (age: number, generating = false) => createFlowContext(us, {
      traffic: { generating, started_at: new Date(now - age).toISOString() } as TrafficStatus,
    }, now).observations[id].status;
    expect(status(59_999)).toBe("active");
    expect(status(60_000)).toBe("idle");
    expect(status(100_000, true)).toBe("active");
    expect(createFlowContext(us, {}, now).observations[id].status).toBe("idle");
  });

  it("uses opaque binding keys rather than recognizing node names", () => {
    const graph = structuredClone(us);
    graph.liveness.opaque = graph.liveness["rtac-relay"];
    graph.flows[0].liveness_key = "opaque";
    expect(createFlowContext(graph, { workshop }).observations[graph.flows[0].id].status).toBe("active");
  });

  it("shows every EU flow with honest unknowns for unobserved or missing telemetry", () => {
    const context = createFlowContext(eu, { operator: operator(), sessions: { sessions: [], count: 0 } });
    expect(Object.fromEntries(Object.entries(context.observations).map(([id, o]) => [id, o.status]))).toEqual({
      "cc-rtu": "active",
      "rtu-breaker": "active",
      "station-clock": "unknown",
      "attacker-rtu": "idle",
      "rtu-rmu-1": "unknown",
      "rtu-rmu-2": "unknown",
      "rtu-oltc-1": "unknown",
      "rtu-plc-1": "unknown",
      "plc-oltc": "unknown",
      "cc-historian": "unknown",
      "enterprise-historian": "unknown",
      "engineering-plc": "unknown",
    });
    expect(context.observations["rtu-breaker"].evidence).toContain("inferred");
    expect(nodeZone("gps-1", eu)).toBe("station_net");
    expect(aggregateFlows(context).find((r) => r.target === "gps-1")?.crossZone).toBe(false);
  });

  it.each(["disconnected", "connecting"] as const)("does not call downstream transport down when CC is %s", (state) => {
    const op = operator(); op.link.state = state;
    const context = createFlowContext(eu, { operator: op });
    expect(context.observations["cc-rtu"].status).toBe(state === "disconnected" ? "down" : "idle");
    expect(context.observations["rtu-breaker"].status).toBe("unknown");
  });

  it.each([
    ["device_unreachable", "down"], ["measurement_invalid", "unknown"],
    ["never_received", "unknown"], ["link_lost", "unknown"],
  ] as const)("interprets %s without overstating downstream evidence", (reason, expected) => {
    const op = operator();
    op.points[0].stale_reason = reason as StaleReason;
    op.points[0].quality.iv = true;
    expect(createFlowContext(eu, { operator: op }).observations["rtu-breaker"].status).toBe(expected);
  });

  it("does not require recent point timestamps or complete GI, but needs received status points", () => {
    const op = operator(); op.link.gi.state = "running";
    expect(createFlowContext(eu, { operator: op }).observations["rtu-breaker"].status).toBe("active");
    op.points[0].received_at = null;
    expect(createFlowContext(eu, { operator: op }).observations["rtu-breaker"].status).toBe("unknown");
    op.points = [];
    expect(createFlowContext(eu, { operator: op }).observations["rtu-breaker"].status).toBe("unknown");
    expect(createFlowContext(eu, {}).observations["cc-rtu"].status).toBe("unknown");
  });

  it("describes the reporting conduit without treating a periodic GI as loss of integrity", () => {
    const op = operator();
    op.link.gi.state = "running";
    op.link.gi.trigger = "periodic";
    const observation = createFlowContext(eu, { operator: op }).observations["cc-rtu"];
    expect(observation.status).toBe("active");
    expect(observation.reason).toBe("IEC104 started; GI running (connect / 15 min / operator); spontaneous status, cyclic/deadband measurands");
    expect(op.link.gi.last_complete_at).not.toBeNull();
  });

  it("does not confuse invalid measurements or old time tags with failed Modbus or GPS transport", () => {
    const op = operator();
    op.points[0].source_time_invalid = true;
    op.points.push({ ...op.points[0], id: "feeder-current", stale_reason: "measurement_invalid", value: null,
      quality: { ...op.points[0].quality, iv: true } });
    const context = createFlowContext(eu, { operator: op });
    expect(context.observations["rtu-breaker"].status).toBe("active");
    expect(context.observations["station-clock"].status).toBe("unknown");
  });

  it("expires failed/stale polls and rejects old package/generation evidence", () => {
    const op = operator();
    const identity = { package: op.package_id, generation: 7, phase: "ready" };
    expect(currentOperatorObservation(op, false, now, identity, now)).toBe(op);
    expect(currentOperatorObservation(op, true, now, identity, now)).toBeUndefined();
    expect(currentOperatorObservation(op, false, now - 15_000, identity, now)).toBeUndefined();
    expect(currentOperatorObservation(op, false, now, { ...identity, generation: 8 }, now)).toBeUndefined();
    expect(currentOperatorObservation(op, false, now, { ...identity, package: "us" }, now)).toBeUndefined();
    expect(currentOperatorObservation(op, false, now, { ...identity, phase: "stopping" }, now)).toBeUndefined();
  });

  it("matches session transport and the exact original tuple, never CC by port alone", () => {
    const session = { proto: "tcp", src: "10.60.60.20", dst: "10.30.30.20", sport: 40000, dport: 2404, state: "ESTABLISHED", timeout: 60 };
    const status = (sessions: typeof session[]) => createFlowContext(eu, { sessions: { sessions, count: sessions.length } }).observations["attacker-rtu"];
    expect(status([session]).status).toBe("idle");
    expect(status([{ ...session, src: "10.10.10.50" }]).status).toBe("active");
    expect(status([{ ...session, src: "10.10.10.50", proto: "udp" }]).status).toBe("idle");
    expect(status([{ ...session, src: "10.10.10.50", dst: "10.30.30.50" }]).status).toBe("idle");
    expect(status([{ ...session, src: "10.10.10.50", dport: 502 }]).status).toBe("idle");
    const partial = { ...session, src: "10.10.10.50", dport: null, state: null };
    expect(createFlowContext(eu, { sessions: { sessions: [partial], count: 1 } }).observations["attacker-rtu"].status).toBe("unknown");
    expect(createFlowContext(eu, { sessions: { sessions: [{ ...partial, dport: 2404 }], count: 1 } }).observations["attacker-rtu"].status).toBe("active");
    expect(createFlowContext(eu, { sessions: { sessions: Array(5000).fill(session), count: 5000 } }).observations["attacker-rtu"].status).toBe("unknown");
    expect(createFlowContext(eu, {}).observations["attacker-rtu"].status).toBe("unknown");
  });

  it("shares filtering and pair status precedence, with no fallback on empty inventory", () => {
    const context = createFlowContext(eu, {});
    expect(aggregateFlows(context, { zone: "all", crossZoneOnly: true })).toHaveLength(10);
    expect(aggregateFlows(context, { zone: "control_centre_net", crossZoneOnly: false })).toHaveLength(2);
    expect(aggregateFlows(createFlowContext({ ...eu, flows: [] }, {}))).toEqual([]);
    const f = { ...eu.flows[0], id: "second", port: 502 };
    const multi = createFlowContext({ ...eu, flows: [eu.flows[0], f] }, {});
    multi.observations[f.id].status = "idle";
    expect(aggregateFlows(multi)[0].status).toBe("unknown");
    multi.observations[f.id].status = "down";
    expect(aggregateFlows(multi)[0].status).toBe("unknown");
    multi.observations[eu.flows[0].id].status = "down";
    expect(aggregateFlows(multi)[0].status).toBe("down");
    multi.observations[f.id].status = "active";
    expect(aggregateFlows(multi)[0].status).toBe("active");
  });
});
