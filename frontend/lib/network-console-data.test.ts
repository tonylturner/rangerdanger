import type { LabGraph, ZoneRuleSummary } from "./api";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import { describe, expect, it } from "vitest";
import {
  deriveNodeHealth,
  getFlowAuthorization,
  getZonePolicyInfo,
  humanizeSummary,
  humanZoneName,
  zones,
} from "./network-console-data";

it("includes the SRA DMZ in multi-homed badges without relabelling EU lan4", () => {
  expect(zones).toContain("sra_dmz_net");
  expect(humanZoneName("sra_dmz_net")).toBe("Secure Remote Access DMZ");
  expect(humanZoneName("lan4")).toBe("Control Centre");
});

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

  it("labels a RESET-only zone as blocked, never allowed or over-permissive", () => {
    expect(getZonePolicyInfo("station_net", rules.map((rule) => ({ ...rule, action: "RESET" })), graph.networks)).toMatchObject({
      label: "RESET", action: "RESET", permissiveness: "blocked",
    });
  });

  it.each(["ALLOW", "DENY", "MIXED"] as const)("keeps RESET mixed with %s distinct from a single action", (action) => {
    expect(getZonePolicyInfo("station_net", [{ ...rules[0], action: "RESET" }, { ...rules[1], action }], graph.networks)).toMatchObject({
      label: "MIXED · authorization unknown", action: "MIXED", permissiveness: "unknown",
    });
  });

  it.each(["ALLOW", "DENY", "RESET", "MIXED"] as const)("retains %s and wildcard destinations", (action) => {
    expect(getZonePolicyInfo("enterprise_net", [{ ...rules[0], dest_zone: "any", action }], graph.networks)).toMatchObject({
      action,
      permissiveness: (action === "DENY" || action === "RESET") ? "blocked" : action === "MIXED" ? "unknown" : "over",
    });
  });
});

describe("getFlowAuthorization", () => {
  const eu = euFixture as LabGraph;
  const rule: ZoneRuleSummary = { source_zone: "lan4", dest_zone: "lan1", summary: "2404", rule_details: ["CC /32 only"], action: "MIXED" };

  it.each([undefined, [], [{ ...rule, source_zone: "wan" }]])("does not invent authorization without a matching summary (%s)", (rules) => {
    expect(getFlowAuthorization("cc-1", "rtu-1", rules, eu)).toMatchObject({ state: "unknown", text: expect.stringContaining("Authorization unknown") });
  });

  it.each(["MIXED", "ALLOW"] as const)("does not treat %s as endpoint/port authorization", (action) => {
    expect(getFlowAuthorization("cc-1", "rtu-1", [{ ...rule, action }], eu)).toEqual({
      state: "unknown", text: expect.stringContaining(`${action} zone-pair summary, not an endpoint/port evaluation`),
    });
  });

  it("labels RESET authorization even when a rule description is present", () => {
    expect(getFlowAuthorization("cc-1", "rtu-1", [{ ...rule, action: "RESET" }], eu)).toEqual({
      state: "denied", text: "Denied by zone-pair summary (RESET) — CC /32 only",
    });
  });

  it("keeps RESET denial evidence labelled with its action", () => {
    expect(getFlowAuthorization("cc-1", "rtu-1", [{ ...rule, action: "RESET", rule_details: [] }], eu)).toEqual({
      state: "denied", text: "Denied by zone-pair summary (RESET) — lan4 → lan1: RESET",
    });
  });

  it("reports denial evidence independently of liveness", () => {
    expect(getFlowAuthorization("cc-1", "rtu-1", [{ ...rule, action: "DENY" }], eu)).toEqual({ state: "denied", text: "Denied by zone-pair summary (DENY) — CC /32 only" });
  });

  it.each(["station-clock", "rtu-plc-1"])("identifies %s as outside firewall scope, even without summaries", (id) => {
    const f = eu.flows.find((f) => f.id === id)!;
    expect(getFlowAuthorization(f.source, f.target, undefined, eu)).toEqual({ state: "same-zone", text: expect.stringContaining("does not traverse containd") });
  });

  it("does not infer source pins from a US MIXED summary either", () => {
    const us = usFixture as LabGraph;
    const rules: ZoneRuleSummary[] = [{ ...rule, source_zone: "lan1", dest_zone: "lan2", rule_details: ["RTAC source pin"] }];
    for (const source of ["rtac-1", "historian-1"]) {
      expect(getFlowAuthorization(source, "relay-1", rules, us).state).toBe("unknown");
    }
  });

  it("keeps EU ingestion, read and engineering authorization unknown with MIXED summaries", () => {
    const rules: ZoneRuleSummary[] = [
      { ...rule, dest_zone: "lan5", summary: "8081" },
      { ...rule, source_zone: "wan", dest_zone: "lan5", summary: "8080, 8081" },
      { ...rule, source_zone: "dmz", summary: "2404, 8080" },
    ];
    for (const id of ["cc-historian", "enterprise-historian", "engineering-plc"]) {
      const flow = eu.flows.find((flow) => flow.id === id)!;
      expect(getFlowAuthorization(flow.source, flow.target, rules, eu).state).toBe("unknown");
    }
    expect(getZonePolicyInfo("ops_dmz_net", rules, eu.networks)).toMatchObject({
      action: "MIXED", permissiveness: "unknown", label: "MIXED · authorization unknown",
    });
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
