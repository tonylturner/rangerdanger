import { describe, expect, it } from "vitest";
import type { LabGraph } from "./api";
import { buildStyledGraph } from "./network-graph";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import { aggregateFlows, createFlowContext } from "./observed-flows";

describe("buildStyledGraph", () => {
  it("builds the current default layout for one firewall zone", () => {
    const graph: LabGraph = {
      nodes: [
        {
          id: "fw-1",
          type: "containd_ngfw",
          position: { x: 0, y: 0 },
          data: { label: "Firewall", zone: "enterprise_net", networks: [] },
        },
        {
          id: "relay-1",
          type: "relay_sim",
          position: { x: 0, y: 0 },
          data: { label: "Relay", zone: "field_net", networks: ["field_net"] },
        },
      ],
      edges: [], flows: [], liveness: {}, networks: [], operations: { traffic_generation: false },
    };

    const result = buildStyledGraph(
      graph,
      undefined,
      { policyDim: true, traffic: false, iec62443: false },
    );

    expect(result.nodes).toHaveLength(4);
    expect(result.edges).toHaveLength(1);
    expect(result.edges[0].id).toBe("fw-to-field_net");
  });

  it.each([false, true])("draws every EU node and the same filtered pairs in layout Purdue=%s", (iec62443) => {
    const graph = euFixture as LabGraph;
    const context = createFlowContext(graph, {});
    for (const filter of [
      { zone: "all", crossZoneOnly: false },
      { zone: "all", crossZoneOnly: true },
      { zone: "station_net", crossZoneOnly: false },
      { zone: "control_centre_net", crossZoneOnly: true },
    ]) {
      const result = buildStyledGraph(graph, [], { traffic: true, policyDim: true, iec62443 }, undefined, undefined, context, true, filter);
      expect(result.nodes.filter((n) => n.type === "host").map((n) => n.id).sort()).toEqual(graph.nodes.filter((n) => n.id !== "fw-1").map((n) => n.id).sort());
      expect(result.edges.filter((e) => e.type === "trafficEdge").map((e) => `${e.source}->${e.target}`)).toEqual(
        aggregateFlows(context, filter).map((r) => `${r.source}->${r.target}`),
      );
      if (iec62443) {
        expect(result.nodes.map((n) => n.data.label).join(" ")).not.toMatch(/Level \d|SL-\d/);
        expect(result.nodes.every((n) => n.data.securityLevel === undefined)).toBe(true);
      }
    }
    const result = buildStyledGraph(graph, [], { traffic: true, policyDim: true, iec62443 }, undefined, undefined, context);
    expect(result.edges.find((e) => e.target === "gps-1")?.data.status).toBe("unknown");
    expect(result.edges.filter((e) => e.type === "trafficEdge")).toHaveLength(4);
  });

  it.each([false, true])("keeps the US pairs, labels and node health in layout Purdue=%s", (iec62443) => {
    const graph = usFixture as LabGraph;
    const deviceComms = { relay: true, recloser: true, regulator: true, capbank: true };
    const context = createFlowContext(graph, {
      workshop: { workshop_id: "us", workshop_name: "US", firewall_online: true, firewall_config: "weak", scenario_count: 7, rtac_online: true, device_comms: deviceComms },
    });
    const result = buildStyledGraph(graph, [], { traffic: true, policyDim: true, iec62443 }, deviceComms, true, context, true);
    const traffic = result.edges.filter((e) => e.type === "trafficEdge");
    expect(traffic).toHaveLength(19);
    expect(traffic.find((e) => e.source === "rtac-1" && e.target === "relay-1")?.data).toMatchObject({ status: "active", label: "Modbus + DNP3" });
    expect(result.nodes.find((n) => n.id === "relay-1")?.data.health).toBe("ok");
    expect(result.nodes.find((n) => n.id === "rtac-1")?.data.health).toBe("ok");
    expect(result.nodes.find((n) => n.id === "fw-1")?.data.health).toBe("ok");
  });
});
