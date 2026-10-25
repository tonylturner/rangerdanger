import { buildContaindConfig } from "./advisory-policy.test-util";
import { US_CURRICULUM } from "./curriculum-storage.test-util";
// Historical generated-policy compatibility oracle. Application of these
// advisory Lab 1.4 policies is retired; preserve every existing catalog and
// guidance assertion without shipping a dead production policy compiler.

import { describe, it, expect } from "vitest";
import { buildDynamicPlan } from "./remediation-to-rules";
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
  return buildContaindConfig(buildDynamicPlan(plan, US_CURRICULUM)) as any;
}

// Enumerate all 2^8 selection combinations.
const COMBOS: string[][] = [];
for (let mask = 0; mask < 1 << ACTIONS.length; mask++) {
  COMBOS.push(ACTIONS.filter((_, i) => mask & (1 << i)));
}

describe("Historical advisory policy — all 1.4 combinations", () => {
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

// Locks compiler quirks, coverage, historical shared-rule logging mutation and
// all three rendered phases across every reachable plan. The prose golden
// includes the US hotfix's supported reads and reference-policy guidance.
import { createHash } from "node:crypto";
import { computeCoverage } from "./requirement-coverage";
import { injectDynamicContent } from "./scenario-runner-logic";
import { DECISION_DEFAULT_OPTIONS } from "./scenario-description";

it("locks US policy, coverage and corrected guidance across all 256 plans", () => {
  const verdicts = ["BLOCK", "BLOCK and LOG", "RESTRICT", "ALLOW", ""];
  expect(verdicts).toEqual([...DECISION_DEFAULT_OPTIONS, ""]);
  const outputs = COMBOS.map((ids) => {
    const plan = buildDynamicPlan({ exerciseId: "remediation-planning", selectedActionIds: ids, savedAt: "" }, US_CURRICULUM);
    return JSON.stringify({
      plan,
      config: buildContaindConfig(plan),
      coverage: verdicts.map((verdict) =>
        computeCoverage(["enterprise-to-field", "enterprise-to-ot", "vendor-to-ot", "non-rtac-to-field", "unauth-modbus-writes"].map((id) => ({ id, label: id, verdict })), new Set(ids), US_CURRICULUM)),
      phases: ["phase-3-create-minimal-required-rules", "phase-5-validate-allowed-traffic", "phase-6-validate-blocked-traffic"].map((title) => injectDynamicContent("original", title, plan)),
    });
  });
  expect(outputs).toHaveLength(256);
  expect(createHash("sha256").update(outputs.join("\n")).digest("hex")).toMatchInlineSnapshot(`"640fa5e9de88550bebea8d84e48eb39adec7babd78adbf51a688f99be75c777f"`);
});
