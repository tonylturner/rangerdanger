import { describe, expect, it } from "vitest";
import {
  deriveNodeHealth,
  getFlowBlockReason,
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

describe("getFlowBlockReason", () => {
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
    expect(getFlowBlockReason("eng-ws-1", "relay-1", summaries)).toBe(
      "WEAK: vendor to field access",
    );
  });

  it("returns null for an allowed flow", () => {
    expect(
      getFlowBlockReason("eng-ws-1", "relay-1", [
        { ...summaries[0], action: "ALLOW" },
      ]),
    ).toBeNull();
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
