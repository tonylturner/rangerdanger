import { describe, expect, it } from "vitest";
import type { LabGraph } from "./api";
import { buildStyledGraph } from "./network-graph";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import { aggregateFlows, createFlowContext } from "./observed-flows";

describe("buildStyledGraph", () => {
  it.each([euFixture, usFixture])("uses package-authored Purdue labels and optional targets for $networks.0.name", (fixture) => {
    const graph = structuredClone(fixture) as LabGraph;
    // Neither labels nor targets can be inferred from familiar zone IDs.
    graph.networks = graph.networks.map((network, i) => ({
      ...network, purdue: `Custom tier ${i}`, name: `Current zone ${i}`,
      target_sl: i % 2 ? 4 : undefined,
    }));
    const result = buildStyledGraph(graph, [], { traffic: false, policyDim: true, iec62443: true });
    const bands = result.nodes.filter((n) => n.type === "zoneBoundary");
    expect(bands).toHaveLength(graph.networks.length);
    for (const network of graph.networks) {
      const band = bands.find((n) => n.id === `zone-boundary-${network.id}`)!;
      expect(band.data.label).toBe(`${network.purdue} · ${network.name}${network.target_sl === undefined ? "" : ` · Target SL-${network.target_sl}`}`);
      expect(band.data.securityLevel).toBe(network.target_sl);
    }
    expect(result.nodes.find((n) => n.type === "firewall")?.data.targetSL).toBe(4);
  });

  it.each([euFixture, usFixture])("shows the declared package mapping in the existing zone order for $networks.0.name", (fixture) => {
    const graph = fixture as LabGraph;
    const result = buildStyledGraph(graph, [], { traffic: false, policyDim: true, iec62443: true });
    const eu = graph.networks.some((n) => n.id === "station_net");
    expect(result.nodes.filter((n) => n.type === "zoneBoundary").map((n) => [n.data.zone, n.data.label])).toEqual(eu ? [
      ["enterprise_net", "Level 4 · Enterprise Zone"],
      ["vendor_net", "Level 3.5 · Vendor / Engineering Zone"],
      ["ops_dmz_net", "Level 3.5 · Operations DMZ"],
      ["control_centre_net", "Level 3 · Control Centre Zone"],
      ["station_net", "Level 1–2 · Station Zone"],
      ["field_net", "Level 1 · Field Device Zone"],
    ] : [
      ["enterprise_net", "Level 4 · Enterprise Zone · Target SL-1"],
      ["vendor_net", "Level 3.5 · Vendor / Engineering Zone · Target SL-2"],
      ["ot_ops_net", "Level 3 · OT Operations Zone · Target SL-3"],
      ["field_net", "Level 1 · Field Device Zone · Target SL-2"],
    ]);
    expect(result.nodes.find((n) => n.type === "firewall")?.data.targetSL).toBe(eu ? undefined : 3);
  });

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
    delete graph.nodes.find((n) => n.type === "containd_ngfw")!.data.interface_ips;
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
      expect(result.nodes.filter((n) => n.type === "host").map((n) => n.id).sort()).toEqual(graph.nodes.filter((n) => n.id !== "fw-1" && n.type !== "zone").map((n) => n.id).sort());
      expect(result.edges.filter((e) => e.type === "trafficEdge").map((e) => `${e.source}->${e.target}`)).toEqual(
        aggregateFlows(context, filter).map((r) => `${r.source}->${r.target}`),
      );
      if (iec62443) {
        expect(result.nodes.map((n) => n.data.label).join(" ")).toContain("Level 1–2 · Station Zone");
        expect(result.nodes.map((n) => n.data.label).join(" ")).not.toMatch(/SL-\d/);
        expect(result.nodes.every((n) => n.data.securityLevel === undefined)).toBe(true);
      }
    }
    const result = buildStyledGraph(graph, [], { traffic: true, policyDim: true, iec62443 }, undefined, undefined, context);
    expect(result.edges.find((e) => e.target === "gps-1")?.data.status).toBe("unknown");
    expect(result.edges.filter((e) => e.type === "trafficEdge")).toHaveLength(13);
    expect(result.edges.find((e) => e.source === "rtu-1" && e.target === "f2-breaker")).toBeDefined();
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


describe("increment-4 map inventory", () => {
  function graph(): LabGraph {
    return structuredClone(euFixture) as LabGraph;
  }
  it.each([false, true])("renders lan5 and every new node, Purdue=%s", (iec62443) => {
    const g = graph();
    const result = buildStyledGraph(g, [], { policyDim: true, traffic: false, iec62443 });
    for (const id of ["rmu-1", "rmu-2", "oltc-1", "plc-1", "historian-1"]) expect(result.nodes.find((n) => n.id === id)?.data.nodeType).toBe(g.nodes.find((n) => n.id === id)!.type);
    expect(result.nodes.find((n) => n.id === "zone-boundary-ops_dmz_net")?.data.label).toBe(iec62443 ? "Level 3.5 · Operations DMZ" : "Operations DMZ");
    expect(result.edges.find((e) => e.id === "fw-to-ops_dmz_net")?.data.zoneMeta.role).toBe("lan5");
    const dmz = result.nodes.find((n) => n.id === "zone-boundary-ops_dmz_net")!;
    const cc = result.nodes.find((n) => n.id === "zone-boundary-control_centre_net")!;
    expect(iec62443 ? dmz.position.y : dmz.position.x).toBeLessThan(iec62443 ? cc.position.y : cc.position.x);
  });
});
