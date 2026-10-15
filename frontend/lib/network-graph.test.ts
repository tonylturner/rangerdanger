import { describe, expect, it } from "vitest";
import type { LabGraph } from "./api";
import { buildStyledGraph } from "./network-graph";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import { aggregateFlows, createFlowContext } from "./observed-flows";

describe("buildStyledGraph", () => {
  it.each([euFixture, usFixture])("keeps label lanes stable across traffic filters for $networks.0.name", (fixture) => {
    const graph = fixture as LabGraph;
    const context = createFlowContext(graph, {});
    for (const iec62443 of [false, true]) {
      const view = { traffic: true, policyDim: true, iec62443 };
      const all = buildStyledGraph(graph, [], view, undefined, undefined, context);
      for (const filter of [
        { zone: "all", crossZoneOnly: true },
        { zone: graph.nodes.find((n) => n.id === graph.flows[0].source)!.data.zone, crossZoneOnly: false },
      ]) {
        const filtered = buildStyledGraph(graph, [], view, undefined, undefined, context, undefined, filter);
        expect(filtered.nodes).toEqual(all.nodes);
        for (const edge of filtered.edges.filter((e) => e.type === "trafficEdge")) {
          expect(edge.data.labelOffset).toEqual(all.edges.find((e) => e.id === edge.id)!.data.labelOffset);
        }
      }
    }
  });

  it.each([euFixture, usFixture])("derives tooltip metadata only from the graph for $networks.0.name", (fixture) => {
    const graph: LabGraph = structuredClone(fixture) as LabGraph;
    const firewall = graph.nodes.find((n) => n.type === "containd_ngfw")!;
    // Deliberately differ from the package defaults to catch stale tables.
    firewall.data.interface_ips = Object.fromEntries(graph.networks.map((n, i) => [n.id, `192.0.2.${i + 1}`]));
    graph.networks = graph.networks.map((n) => ({
      ...n, name: `Current ${n.name}`, subnet: "192.0.2.0/24", zone: `role-${n.id}`, description: `Topology description for ${n.id}`,
    }));
    for (const iec62443 of [false, true]) {
      const result = buildStyledGraph(graph, [], { traffic: false, policyDim: true, iec62443 });
      for (const edge of result.edges) {
        const network = graph.networks.find((n) => edge.id === `fw-to-${n.id}`)!;
        expect(edge.data.zoneMeta).toEqual({
          label: network.name, role: network.zone, subnet: network.subnet,
          fwIp: firewall.data.interface_ips[network.id], description: network.description,
        });
      }
    }
  });

  it("does not invent missing interface metadata", () => {
    const graph = structuredClone(euFixture) as LabGraph;
    const network = graph.networks.find((n) => n.id === "station_net")!;
    delete network.subnet;
    network.cidr = "192.0.2.0/24";
    delete network.description;
    delete network.zone;
    expect(buildStyledGraph(graph).edges.find((e) => e.id === "fw-to-station_net")?.data.zoneMeta).toEqual({
      label: network.name, role: "—", subnet: network.cidr, fwIp: "—", description: "",
    });
  });

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
    expect(result.edges[0].data.zoneMeta).toBeUndefined();
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
    expect(result.edges.find((e) => e.target === "gps-1")?.data.sameZone).toBe(true);
    expect(result.edges.find((e) => e.source === "cc-1" && e.target === "rtu-1")?.data.sameZone).toBe(false);
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
    expect(traffic.find((e) => e.source === "rtac-1" && e.target === "relay-1")?.data.sameZone).toBe(false);
    expect(traffic.find((e) => e.source === "historian-1" && e.target === "rtac-1")?.data.sameZone).toBe(true);
    expect(result.nodes.find((n) => n.id === "relay-1")?.data.health).toBe("ok");
    expect(result.nodes.find((n) => n.id === "rtac-1")?.data.health).toBe("ok");
    expect(result.nodes.find((n) => n.id === "fw-1")?.data.health).toBe("ok");
  });
});
