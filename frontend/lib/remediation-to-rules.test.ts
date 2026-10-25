import { buildContaindConfig } from "./advisory-policy.test-util";
import { US_CURRICULUM } from "./curriculum-storage.test-util";
import { describe, it, expect } from "vitest";
import {
  buildDynamicPlan,
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
} from "./remediation-to-rules";
import type { RemediationPlan } from "./remediation-plan";
import { splitDescription } from "./scenario-description";

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

describe("buildContaindConfig management safety", () => {
  it("preserves lan3 access and leaves NIC discovery to containd", () => {
    const config = buildContaindConfig(buildDynamicPlan(ALL_HARDENED, US_CURRICULUM)) as {
      interfaces: { name: string; zone: string; access: object; device?: string }[];
      zones: { name: string }[];
    };
    expect(config.interfaces.filter((iface) => iface.name === "lan3")).toEqual([
      { name: "lan3", zone: "lan3", access: { mgmt: true, http: true, https: true, ssh: true } },
    ]);
    expect(config.zones).toContainEqual({ name: "lan3", description: "RangerDanger management control plane" });
    expect(config.interfaces.every((iface) => !("device" in iface))).toBe(true);
    expect(config.interfaces.map((iface) => iface.name)).toEqual(["wan", "dmz", "lan1", "lan2", "lan3"]);
  });
});

describe("buildDynamicPlan", () => {
  it("flags no-plan when input is null", () => {
    const plan = buildDynamicPlan(null, US_CURRICULUM);
    expect(plan.hasRemediationPlan).toBe(false);
    expect(plan.selectedActions).toEqual([]);
    expect(plan.allRules.length).toBeGreaterThan(0); // baseline rules still present
  });

  it("flags has-plan when at least one action is selected", () => {
    const plan = buildDynamicPlan({
      exerciseId: "remediation-planning",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "2026-05-07T00:00:00Z",
    }, US_CURRICULUM);
    expect(plan.hasRemediationPlan).toBe(true);
    expect(plan.selectedActions).toContain("block-enterprise-to-field");
  });

  it("derives includeDpi from modbus-dpi or dnp3-dpi selection", () => {
    expect(buildDynamicPlan(null, US_CURRICULUM).includeDpi).toBe(false);
    expect(
      buildDynamicPlan({
        exerciseId: "x",
        selectedActionIds: ["modbus-dpi"],
        savedAt: "",
      }, US_CURRICULUM).includeDpi
    ).toBe(true);
    expect(
      buildDynamicPlan({
        exerciseId: "x",
        selectedActionIds: ["dnp3-dpi"],
        savedAt: "",
      }, US_CURRICULUM).includeDpi
    ).toBe(true);
  });

  it("enableLogging marks DENY + RTAC ALLOW rules with log:true", () => {
    const plan = buildDynamicPlan(ALL_HARDENED, US_CURRICULUM);
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
    }, US_CURRICULUM);
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
    const plan = buildDynamicPlan(ALL_HARDENED, US_CURRICULUM);
    const md = renderRuleTable(plan);
    const lines = md.split("\n");
    // header + separator + N rows
    expect(lines[0]).toContain("Source");
    expect(lines[1]).toContain("---");
    expect(lines.length).toBe(2 + plan.allRules.length);
  });

  it("adds a Log column when enableLogging is on", () => {
    const plan = buildDynamicPlan(ALL_HARDENED, US_CURRICULUM);
    expect(renderRuleTable(plan).split("\n")[0]).toContain("Log");

    const noLog = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"],
      savedAt: "",
    }, US_CURRICULUM);
    expect(renderRuleTable(noLog).split("\n")[0]).not.toContain("Log");
  });
});

describe("positiveValidationTests + negativeValidationTests", () => {
  it("positive tests always include RTAC→field and HMI→RTAC", () => {
    const plan = buildDynamicPlan(null, US_CURRICULUM);
    const tests = positiveValidationTests(plan);
    expect(tests.some((t) => t.includes("RTAC"))).toBe(true);
    expect(tests.some((t) => t.includes("HMI"))).toBe(true);
  });

  it("negative tests reflect what was blocked", () => {
    const plan = buildDynamicPlan(ALL_HARDENED, US_CURRICULUM);
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

  it("validation commands render as command blocks (indented, not prose)", () => {
    // Regression: the dynamic Phase 5/6 generators once emitted commands
    // at column 0, so splitDescription saw them as prose (no run/copy).
    const plan = buildDynamicPlan(ALL_HARDENED, US_CURRICULUM);
    const strings = [...positiveValidationTests(plan), ...negativeValidationTests(plan)];
    for (const t of strings) {
      const segs = splitDescription(t);
      expect(
        segs.some((s) => s.type === "cmd"),
        `expected a command block in: ${t.slice(0, 50)}`,
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
    const plan = buildDynamicPlan(null, US_CURRICULUM);
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
    }, US_CURRICULUM);
    const cfg = buildContaindConfig(plan) as { firewall: { rules: { id: string }[] } };
    expect(cfg.firewall.rules.map((r) => r.id)).toContain("deny-enterprise-to-field");
  });

  it("does NOT add deny rules for unselected remediations", () => {
    const plan = buildDynamicPlan({
      exerciseId: "x",
      selectedActionIds: ["block-enterprise-to-field"], // only this
      savedAt: "",
    }, US_CURRICULUM);
    const cfg = buildContaindConfig(plan) as { firewall: { rules: { id: string }[] } };
    const ids = cfg.firewall.rules.map((r) => r.id);
    expect(ids).not.toContain("deny-enterprise-to-ot"); // not selected
  });
});
