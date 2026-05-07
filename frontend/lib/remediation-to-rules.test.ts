import { describe, it, expect } from "vitest";
import {
  buildDynamicPlan,
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
  buildContaindConfig,
} from "./remediation-to-rules";
import type { RemediationPlan } from "./remediation-plan";

const ALL_HARDENED: RemediationPlan = {
  exerciseId: "remediation-planning",
  selectedActionIds: [
    "block-enterprise-to-field",
    "block-enterprise-to-ot",
    "restrict-vendor-to-ot",
    "block-vendor-to-field",
    "pin-rtac-to-field",
    "modbus-dpi",
    "dnp3-dpi",
    "improve-logging",
    "tighten-firewall-objects",
    "positive-validation",
  ],
  savedAt: "2026-05-07T00:00:00Z",
};

describe("buildDynamicPlan", () => {
  it("flags no-plan when input is null", () => {
    const plan = buildDynamicPlan(null);
    expect(plan.hasRemediationPlan).toBe(false);
    expect(plan.selectedActions).toEqual([]);
    expect(plan.allRules.length).toBeGreaterThan(0); // baseline rules still present
  });

  it("flags has-plan when at least one action is selected", () => {
    const plan = buildDynamicPlan({
      exerciseId: "remediation-planning",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "2026-05-07T00:00:00Z",
    });
    expect(plan.hasRemediationPlan).toBe(true);
    expect(plan.selectedActions).toContain("block-enterprise-to-field");
  });

  it("derives includeDpi from modbus-dpi or dnp3-dpi selection", () => {
    expect(buildDynamicPlan(null).includeDpi).toBe(false);
    expect(
      buildDynamicPlan({
        exerciseId: "x",
        selectedActionIds: ["modbus-dpi"],
        savedAt: "",
      }).includeDpi
    ).toBe(true);
    expect(
      buildDynamicPlan({
        exerciseId: "x",
        selectedActionIds: ["dnp3-dpi"],
        savedAt: "",
      }).includeDpi
    ).toBe(true);
  });

  it("enableLogging marks DENY + RTAC ALLOW rules with log:true", () => {
    const plan = buildDynamicPlan(ALL_HARDENED);
    expect(plan.enableLogging).toBe(true);
    const denyLogged = plan.allRules.filter((r) => r.action === "DENY" && r.log);
    expect(denyLogged.length).toBeGreaterThan(0);
    const rtacLogged = plan.allRules.filter((r) => r.id.startsWith("rtac-") && r.log);
    expect(rtacLogged.length).toBeGreaterThan(0);
  });

  it("selectedSummary + unselectedSummary partition the action set", () => {
    const oneAction = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "",
    });
    expect(oneAction.selectedSummary.map((a) => a.id)).toEqual(
      ["block-enterprise-to-field"]
    );
    expect(
      oneAction.unselectedSummary.find((a) => a.id === "block-enterprise-to-field")
    ).toBeUndefined();
    expect(oneAction.unselectedSummary.length).toBeGreaterThan(0);
  });
});

describe("renderRuleTable", () => {
  it("produces a markdown table with one row per rule", () => {
    const plan = buildDynamicPlan(ALL_HARDENED);
    const md = renderRuleTable(plan);
    const lines = md.split("\n");
    // header + separator + N rows
    expect(lines[0]).toContain("Source");
    expect(lines[1]).toContain("---");
    expect(lines.length).toBe(2 + plan.allRules.length);
  });

  it("adds a Log column when enableLogging is on", () => {
    const plan = buildDynamicPlan(ALL_HARDENED);
    expect(renderRuleTable(plan).split("\n")[0]).toContain("Log");

    const noLog = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "",
    });
    expect(renderRuleTable(noLog).split("\n")[0]).not.toContain("Log");
  });
});

describe("positiveValidationTests + negativeValidationTests", () => {
  it("positive tests always include RTAC→field and HMI→RTAC", () => {
    const plan = buildDynamicPlan(null);
    const tests = positiveValidationTests(plan);
    expect(tests.some((t) => t.includes("RTAC"))).toBe(true);
    expect(tests.some((t) => t.includes("HMI"))).toBe(true);
  });

  it("negative tests reflect what was blocked", () => {
    const plan = buildDynamicPlan(ALL_HARDENED);
    const tests = negativeValidationTests(plan);
    expect(tests.length).toBeGreaterThan(0);
    // Every negative test should include the word "blocked", "deny", or "should fail"
    for (const t of tests) {
      const lower = t.toLowerCase();
      expect(
        lower.includes("blocked") ||
          lower.includes("deny") ||
          lower.includes("should fail") ||
          lower.includes("should time out") ||
          lower.includes("should be filtered")
      ).toBe(true);
    }
  });
});

describe("buildContaindConfig", () => {
  // Note: buildContaindConfig assembles its rule list independently of
  // plan.allRules — it always starts with the same 2 base rules
  // (allow-mgmt-ui, enterprise-to-vendor) and appends one rule per
  // selected remediation. Tests verify shape + selection-driven
  // additions, not parity with plan.allRules.length.

  it("returns a containd policy with at least the 2 base rules", () => {
    const plan = buildDynamicPlan(null);
    const cfg = buildContaindConfig(plan) as { firewall: { rules: { id: string }[] } };
    expect(cfg.firewall).toBeDefined();
    expect(Array.isArray(cfg.firewall.rules)).toBe(true);
    const ids = cfg.firewall.rules.map((r) => r.id);
    expect(ids).toContain("allow-mgmt-ui");
    expect(ids).toContain("enterprise-to-vendor");
  });

  it("adds deny-enterprise-to-field when that remediation is selected", () => {
    const plan = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "",
    });
    const cfg = buildContaindConfig(plan) as { firewall: { rules: { id: string }[] } };
    expect(cfg.firewall.rules.map((r) => r.id)).toContain("deny-enterprise-to-field");
  });

  it("does NOT add deny rules for unselected remediations", () => {
    const plan = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"], // only this
      savedAt: "",
    });
    const cfg = buildContaindConfig(plan) as { firewall: { rules: { id: string }[] } };
    const ids = cfg.firewall.rules.map((r) => r.id);
    expect(ids).not.toContain("deny-enterprise-to-ot"); // not selected
  });
});
