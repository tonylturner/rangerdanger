import type { LabGraph, ZoneRuleSummary } from "./api";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import { describe, expect, it } from "vitest";
import {
  deriveNodeHealth,
  getFlowBlockReason,
  getZonePolicyInfo,
  humanizeSummary,
} from "./network-console-data";

describe("humanizeSummary", () => {
  it("maps known port labels and strips the more-count tail", () => {
    expect(humanizeSummary("502, 20000, 8080 +4 more")).toEqual([
      "Modbus",
      "DNP3",
      "HTTP",
    ]);
  });

  it("returns an empty list for a blank summary", () => {
    expect(humanizeSummary("  ")).toEqual([]);
  });
});

describe("getZonePolicyInfo", () => {
  const graph = euFixture as LabGraph;
  const rules: ZoneRuleSummary[] = [
    { source_zone: "wan", dest_zone: "lan1", summary: "2404", rule_details: ["WEAK: enterprise to station"], action: "ALLOW" },
    { source_zone: "dmz", dest_zone: "lan1", summary: "2404", rule_details: ["WEAK: vendor to station"], action: "ALLOW" },
    { source_zone: "lan4", dest_zone: "lan1", summary: "2404", rule_details: ["WEAK: control centre to station"], action: "ALLOW" },
  ];

  it.each([undefined, []])("does not claim an allow without policy data (%s)", (summaries) => {
    expect(getZonePolicyInfo("enterprise_net", summaries, graph.networks)).toEqual({
      label: "No policy data", action: "UNKNOWN", permissiveness: "unknown", details: [],
    });
  });

  it.each(["enterprise_net", "vendor_net", "control_centre_net"])("distinguishes inbound coverage from outgoing allows for %s", (zone) => {
    expect(getZonePolicyInfo(zone, rules, graph.networks)).toEqual({
      label: "No inbound rules", action: "UNKNOWN", permissiveness: "unknown", details: [],
    });
  });

  it("includes all inbound rules without including outgoing rules", () => {
    const info = getZonePolicyInfo("station_net", rules, graph.networks);
    expect(info).toMatchObject({ label: "IEC 104", action: "ALLOW", permissiveness: "over" });
    expect(info.details).toHaveLength(3);
  });

  it.each(["ALLOW", "DENY", "MIXED"] as const)("retains %s and wildcard destinations", (action) => {
    expect(getZonePolicyInfo("enterprise_net", [{ ...rules[0], dest_zone: "any", action }], graph.networks)).toMatchObject({
      action,
      permissiveness: action === "DENY" ? "blocked" : "over",
    });
  });
});

describe("getFlowBlockReason", () => {
  const graph: LabGraph = {
    nodes: [ ["eng-ws-1", "vendor_net"], ["relay-1", "field_net"] ].map(([id, zone]) => ({ id, type: "host", position: { x: 0, y: 0 }, data: { label: id, zone, networks: [zone] } })),
    edges: [], flows: [], liveness: {}, operations: { traffic_generation: true },
    networks: [{ id: "vendor_net", name: "Vendor", zone: "dmz" }, { id: "field_net", name: "Field", zone: "lan2" }],
  };
  const summaries = [
    {
      source_zone: "dmz",
      dest_zone: "lan2",
      summary: "502, 20000",
      rule_details: ["WEAK: vendor to field access"],
      action: "DENY" as const,
    },
  ];

  it("returns the matching denial reason for a blocked flow", () => {
    expect(getFlowBlockReason("eng-ws-1", "relay-1", summaries, graph)).toBe(
      "WEAK: vendor to field access",
    );
  });

  it("returns null for an allowed flow", () => {
    expect(
      getFlowBlockReason("eng-ws-1", "relay-1", [
        { ...summaries[0], action: "ALLOW" },
      ], graph),
    ).toBeNull();
  });

  it("uses real EU zone mappings without treating MIXED as non-RTAC denial", () => {
    const eu = euFixture as LabGraph;
    const rules = [{ source_zone: "lan4", dest_zone: "lan1", summary: "2404", rule_details: ["CC /32 only"], action: "MIXED" as const }];
    expect(getFlowBlockReason("cc-1", "rtu-1", rules, eu)).toBeNull();
    expect(getFlowBlockReason("cc-1", "rtu-1", [{ ...rules[0], action: "DENY" }], eu)).toBe("CC /32 only");
    expect(getFlowBlockReason("rtu-1", "gps-1", rules, eu)).toBeNull();
    expect(getZonePolicyInfo("station_net", rules, eu.networks)).toMatchObject({ action: "MIXED", label: "MIXED · source-restricted" });
  });

  it("keeps authorised EU HTTP flows distinct from logged zone-pair denies", () => {
    const eu = euFixture as LabGraph;
    const rules: ZoneRuleSummary[] = [
      { source_zone: "lan4", dest_zone: "lan5", summary: "8081", rule_details: ["CC ingest allow; other ingest sources denied and logged"], action: "MIXED" },
      { source_zone: "wan", dest_zone: "lan5", summary: "8080, 8081", rule_details: ["Enterprise read allow; ingest denied and logged"], action: "MIXED" },
      { source_zone: "dmz", dest_zone: "lan1", summary: "2404, 8080", rule_details: ["Engineering PLC web allow; other web sources denied and logged"], action: "MIXED" },
    ];
    for (const id of ["cc-historian", "enterprise-historian", "engineering-plc"]) {
      const flow = eu.flows.find((flow) => flow.id === id)!;
      expect(getFlowBlockReason(flow.source, flow.target, rules, eu)).toBeNull();
    }
    expect(getZonePolicyInfo("ops_dmz_net", rules, eu.networks)).toMatchObject({
      action: "MIXED", permissiveness: "allowed",
    });
  });

  it("retains the established US source-pin overlay", () => {
    const us = usFixture as LabGraph;
    const rules = [{ source_zone: "lan1", dest_zone: "lan2", summary: "502", rule_details: ["RTAC source pin"], action: "MIXED" as const }];
    expect(getFlowBlockReason("rtac-1", "relay-1", rules, us)).toBeNull();
    expect(getFlowBlockReason("historian-1", "relay-1", rules, us)).toContain("only RTAC");
  });
});

describe("deriveNodeHealth", () => {
  it("reports healthy device telemetry", () => {
    expect(
      deriveNodeHealth("relay-1", { deviceComms: { relay: true } }),
    ).toEqual({ health: "ok", healthSource: "RTAC device_comms[relay]" });
  });

  it("reports a stopped RTAC as down", () => {
    expect(deriveNodeHealth("rtac-1", { rtac: false })).toEqual({
      health: "down",
      healthSource: "RTAC /api/state",
    });
  });

  it("reports a failing firewall health check as down", () => {
    expect(deriveNodeHealth("fw-1", { firewall: false })).toEqual({
      health: "down",
      healthSource: "containd /api/v1/health",
    });
  });
});
