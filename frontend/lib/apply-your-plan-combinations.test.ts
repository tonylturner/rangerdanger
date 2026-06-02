// Contract test: "Apply Your Plan" (Lab 2.2 side panel) builds a containd
// config from the student's Lab 1.4 remediation picks via
// buildContaindConfig(). The picks are a free combination of 8 actions,
// so there are 2^8 = 256 possible plans. Every one must produce a config
// the backend's POST /api/firewall/apply-custom will accept (it rejects
// configs missing `interfaces` or `firewall.rules`) and that containd can
// load (unique rule ids, valid actions, correct ICS schema).
//
// This was added after a real bug: the Modbus DPI predicate emitted
// `functionCodes` (plural) while containd's schema is singular
// `functionCode`, so the function-code filter was silently dropped.

import { describe, it, expect } from "vitest";
import { buildDynamicPlan, buildContaindConfig } from "./remediation-to-rules";
import type { RemediationPlan } from "./remediation-plan";

const ACTIONS = [
  "block-enterprise-to-field",
  "block-enterprise-to-ot",
  "restrict-vendor-to-ot",
  "block-vendor-to-field",
  "pin-rtac-to-field",
  "modbus-dpi",
  "dnp3-dpi",
  "improve-logging",
];

function configFor(ids: string[]): any {
  const plan: RemediationPlan = {
    exerciseId: "remediation-planning",
    selectedActionIds: ids,
    savedAt: "2026-06-01T00:00:00Z",
  };
  return buildContaindConfig(buildDynamicPlan(plan)) as any;
}

// Enumerate all 2^8 selection combinations.
const COMBOS: string[][] = [];
for (let mask = 0; mask < 1 << ACTIONS.length; mask++) {
  COMBOS.push(ACTIONS.filter((_, i) => mask & (1 << i)));
}

describe("Apply Your Plan — every 1.4 combination yields an applicable config", () => {
  it("enumerates all 256 selection combinations", () => {
    expect(COMBOS).toHaveLength(256);
  });

  it("every combination meets the /apply-custom contract + containd basics", () => {
    for (const ids of COMBOS) {
      const cfg = configFor(ids);
      const label = ids.join(",") || "(none)";
      // backend rejects configs without interfaces or firewall.rules
      expect(
        Array.isArray(cfg.interfaces) && cfg.interfaces.length > 0,
        `interfaces for [${label}]`,
      ).toBe(true);
      const rules = cfg.firewall?.rules;
      expect(
        Array.isArray(rules) && rules.length > 0,
        `firewall.rules for [${label}]`,
      ).toBe(true);
      expect(cfg.firewall.defaultAction, `defaultAction for [${label}]`).toBe("DENY");
      // containd keys rules by id — duplicates would clobber
      const ruleIds = rules.map((r: any) => r.id);
      expect(new Set(ruleIds).size, `unique rule ids for [${label}]`).toBe(ruleIds.length);
      for (const r of rules) {
        expect(["ALLOW", "DENY"], `valid action in [${label}]`).toContain(r.action);
      }
      expect(() => JSON.stringify(cfg), `serialisable [${label}]`).not.toThrow();
    }
  });

  it("Modbus DPI uses containd's singular `functionCode` field (regression)", () => {
    const cfg = configFor(["modbus-dpi"]);
    const mb = cfg.firewall.rules.find(
      (r: any) => r.ics?.protocol === "modbus",
    );
    expect(mb, "a modbus DPI rule is present").toBeTruthy();
    expect(Array.isArray(mb.ics.functionCode)).toBe(true);
    expect(mb.ics.functionCode.length).toBeGreaterThan(0);
    expect(mb.ics.functionCodes, "no plural field that containd ignores").toBeUndefined();
  });

  it("DNP3 DPI attaches dnp3 ICS to the RTAC DNP3 rule", () => {
    const cfg = configFor(["dnp3-dpi"]);
    expect(
      cfg.firewall.rules.some((r: any) => r.ics?.protocol === "dnp3"),
    ).toBe(true);
  });

  it("improve-logging marks DENY rules with log:true (and absent without it)", () => {
    const withLog = configFor(["block-enterprise-to-field", "improve-logging"]);
    expect(
      withLog.firewall.rules.find((r: any) => r.id === "deny-enterprise-to-field")?.log,
    ).toBe(true);
    const noLog = configFor(["block-enterprise-to-field"]);
    expect(
      noLog.firewall.rules.find((r: any) => r.id === "deny-enterprise-to-field")?.log,
    ).toBeUndefined();
  });
});
