import { describe, expect, it } from "vitest";
import { getBezierPath, Position, type Node } from "reactflow";
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
        const source = result.nodes.find((n) => n.id === edge.source)!;
        const zone = iec62443 ? source.data.zone : result.nodes.find((n) => n.id === edge.target)!.data.zone;
        const network = graph.networks.find((n) => n.id === zone)!;
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

describe("Purdue rows", () => {
  const view = { traffic: false, policyDim: true, iec62443: true };

  function expectHostsInside(nodes: Node[]) {
    for (const host of nodes.filter((n) => n.type === "host")) {
      const box = nodes.find((n) => n.id === `zone-boundary-${host.data.zone}`)!;
      expect(host.position.x, host.id).toBeGreaterThanOrEqual(box.position.x + 30);
      expect(host.position.x + 120, host.id).toBeLessThanOrEqual(box.position.x + box.data.width - 30);
      expect(host.position.y, host.id).toBeGreaterThanOrEqual(box.position.y + 50);
      expect(host.position.y + 130, host.id).toBeLessThanOrEqual(box.position.y + box.data.height);
    }
  }

  it("shares only the EU Level 3.5 row and sizes its boxes for their hosts", () => {
    const { nodes } = buildStyledGraph(euFixture as LabGraph, [], view);
    const boxes = nodes.filter((n) => n.type === "zoneBoundary");
    const vendor = boxes.find((n) => n.data.zone === "vendor_net")!;
    const dmz = boxes.find((n) => n.data.zone === "ops_dmz_net")!;
    const enterprise = boxes[0];
    expect(boxes.map((n) => n.position.y)).toEqual([0, 230, 230, 460, 690, 920]);
    expect(vendor.position.x).toBe(enterprise.position.x);
    expect(dmz.position.x).toBe(vendor.position.x + vendor.data.width + 30);
    expect(dmz.position.x + dmz.data.width).toBe(enterprise.position.x + enterprise.data.width);
    expect(vendor.data.width).toBeGreaterThan(dmz.data.width);
    for (const box of boxes.filter((n) => n !== vendor && n !== dmz)) {
      expect(box.position.x).toBe(enterprise.position.x);
      expect(box.data.width).toBe(enterprise.data.width);
    }
    expectHostsInside(nodes);
    expect(nodes.find((n) => n.type === "firewall")?.position).toEqual({
      x: enterprise.position.x + enterprise.data.width + 120, y: (920 + 200) / 2 - 50,
    });
  });

  it("does not merge non-adjacent equal levels", () => {
    const graph = structuredClone(euFixture) as LabGraph;
    graph.networks.find((n) => n.id === "control_centre_net")!.purdue = "Level 4";
    const boxes = buildStyledGraph(graph, [], view).nodes.filter((n) => n.type === "zoneBoundary");
    expect(boxes.map((n) => n.position.y)).toEqual([0, 230, 230, 460, 690, 920]);
    expect(boxes[0].data.label).toContain("Level 4");
    expect(boxes[3].data.label).toContain("Level 4");
  });

  it("widens shared rows rather than clipping hosts or their reserved lanes", () => {
    const graph = structuredClone(euFixture) as LabGraph;
    graph.networks = graph.networks.map((n) => ({ ...n, purdue: "Shared level", target_sl: 3 }));
    const { nodes } = buildStyledGraph(graph, [], view);
    const boxes = nodes.filter((n) => n.type === "zoneBoundary");
    expect(boxes.every((n) => n.position.y === 0)).toBe(true);
    for (let i = 1; i < boxes.length; i++) {
      expect(boxes[i].position.x).toBeCloseTo(boxes[i - 1].position.x + boxes[i - 1].data.width + 30);
    }
    expectHostsInside(nodes);
    expect(boxes.every((n) => n.data.securityLevel === 3 && n.data.label.endsWith(" · Target SL-3"))).toBe(true);
    expect(nodes.find((n) => n.type === "firewall")?.data.targetSL).toBe(3);
  });

  it("routes the left conduit above its peer and bends only outside the row", () => {
    const { nodes, edges } = buildStyledGraph(euFixture as LabGraph, [], view);
    const vendor = nodes.find((n) => n.id === "zone-boundary-vendor_net")!;
    const dmz = nodes.find((n) => n.id === "zone-boundary-ops_dmz_net")!;
    const gap = edges.find((e) => e.id === "conduit-gap-vendor_net")!;
    const exit = edges.find((e) => e.id === "fw-to-vendor_net")!;
    expect(gap.source).toBe("zone-vendor_net");
    expect(gap.target).toBe(exit.source);
    expect(exit.target).toBe("fw-1");
    expect(gap.data.label).toBe("");
    expect(exit.data.label).not.toBe("");
    expect(gap.data.zoneMeta).toEqual(exit.data.zoneMeta);
    expect(gap.style).toEqual(exit.style);

    for (const edge of [gap, exit]) {
      expect(edge.type).toBe("policyEdge");
      expect(edge.sourceHandle).toBe("right");
      expect(edge.targetHandle).toBe("left");
      const source = nodes.find((n) => n.id === edge.source)!;
      const target = nodes.find((n) => n.id === edge.target)!;
      const firewall = target.type === "firewall";
      // Use the actual ZoneNode and FirewallNode handle offsets and the
      // PolicyEdge formatter. A cubic Bezier stays within its control hull:
      // the first hull is above both boxes, the second wholly to their right.
      const [path] = getBezierPath({
        sourceX: source.position.x + 1, sourceY: source.position.y + 0.5,
        targetX: target.position.x + (firewall ? 36 : 0),
        targetY: target.position.y + (firewall ? 44 : 0.5),
        sourcePosition: Position.Right, targetPosition: Position.Left,
      });
      const coords = path.match(/-?\d+(?:\.\d+)?/g)!.map(Number);
      const xs = coords.filter((_, i) => i % 2 === 0);
      const ys = coords.filter((_, i) => i % 2 === 1);
      for (const box of [vendor, dmz]) {
        if (edge === gap) expect(Math.max(...ys)).toBeLessThan(box.position.y);
        else expect(Math.min(...xs)).toBeGreaterThan(box.position.x + box.data.width);
      }
    }
    expect(nodes.filter((n) => n.id.startsWith("conduit-waypoint-"))).toHaveLength(1);
    expect(edges.filter((e) => e.id.startsWith("conduit-gap-"))).toHaveLength(1);
  });

  it.each([euFixture, usFixture])("leaves the non-Purdue layout independent of levels for $networks.0.name", (fixture) => {
    const graph = structuredClone(fixture) as LabGraph;
    const policyView = { ...view, iec62443: false };
    const before = buildStyledGraph(graph, [], policyView);
    graph.networks = graph.networks.map((n) => ({ ...n, purdue: "Same level" }));
    expect(buildStyledGraph(graph, [], policyView)).toEqual(before);
  });

  it("preserves shipped US Purdue geometry", () => {
    const { nodes, edges } = buildStyledGraph(usFixture as LabGraph, [], view);
    expect(nodes.map((n) => [n.id, n.position.x, n.position.y, n.data.width ?? "-", n.data.height ?? "-"].join(" | "))).toMatchInlineSnapshot(`
      [
        "zone-boundary-enterprise_net | -400 | 0 | 948 | 200",
        "zone-enterprise_net | 548 | 100 | - | -",
        "corp-ws-1 | -370 | 50 | - | -",
        "kali-1 | -178 | 50 | - | -",
        "zone-boundary-vendor_net | -400 | 230 | 948 | 200",
        "zone-vendor_net | 548 | 330 | - | -",
        "vendor-jump-1 | -370 | 280 | - | -",
        "eng-ws-1 | -178 | 280 | - | -",
        "zone-boundary-ot_ops_net | -400 | 460 | 948 | 200",
        "zone-ot_ops_net | 548 | 560 | - | -",
        "hmi-1 | -370 | 510 | - | -",
        "rtac-1 | -178 | 510 | - | -",
        "openplc-1 | 14 | 510 | - | -",
        "historian-1 | 206 | 510 | - | -",
        "gps-1 | 398 | 510 | - | -",
        "zone-boundary-field_net | -400 | 690 | 948 | 200",
        "zone-field_net | 548 | 790 | - | -",
        "relay-1 | -370 | 740 | - | -",
        "recloser-1 | -178 | 740 | - | -",
        "regulator-1 | 14 | 740 | - | -",
        "capbank-1 | 206 | 740 | - | -",
        "fw-1 | 668 | 395 | - | -",
      ]
    `);
    expect(edges.map((e) => [e.id, e.source, e.target, e.sourceHandle, e.targetHandle, e.type].join(" | "))).toMatchInlineSnapshot(`
      [
        "fw-to-enterprise_net | zone-enterprise_net | fw-1 | right | left | policyEdge",
        "fw-to-vendor_net | zone-vendor_net | fw-1 | right | left | policyEdge",
        "fw-to-ot_ops_net | zone-ot_ops_net | fw-1 | right | left | policyEdge",
        "fw-to-field_net | zone-field_net | fw-1 | right | left | policyEdge",
      ]
    `);
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
