import { describe, expect, it } from "vitest";
import { US_PLANS, EU_PLANS } from "./plan-fixtures.test-util";
import { US_CURRICULUM } from "./curriculum-storage.test-util";
import { renderPlanTests, renderRuleTable } from "./plan-presentation";

const binding = US_CURRICULUM;
describe("compiled tables", () => {
  it("includes global proof rows and filters restricted rows by lab role and category", () => {
    expect(renderPlanTests(US_PLANS[0], binding, "implementation", "negative")).toContain("U1:");
    expect(renderPlanTests(US_PLANS[0], binding, "implementation", "negative")).not.toContain("U2:");
    expect(renderPlanTests(US_PLANS[0], binding, "hardening", "negative")).toContain("U2:");
    expect(renderPlanTests(US_PLANS[0], binding, "baseline", "positive")).toContain("A1:");
    expect(renderPlanTests(US_PLANS[0], binding, "baseline", "negative")).not.toContain("A1:");
  });
  it("shows Weak deferred exposure, selected and required origins, logging and inspection verbatim", () => {
    const empty = renderRuleTable(US_PLANS[0], binding);
    expect(empty).toContain("deferred");
    expect(empty).toContain("Weak exposure open");
    const full = structuredClone(US_PLANS[2]);
    full.rules[0].rule.ics = { protocol: "modbus", functionCode: [3, 4] };
    full.rules[0].rule.log = true;
    const before = JSON.stringify(full);
    const text = renderRuleTable(full, binding);
    expect(text).toContain("selected");
    expect(text).toContain("required");
    expect(text).toContain('"functionCode":[3,4]');
    expect(text).toContain("yes");
    expect(JSON.stringify(full)).toBe(before);
  });
  it("renders optional rule fields and global lab scope when omitted by Go JSON", () => {
    const plan = structuredClone(US_PLANS[0]);
    delete plan.rules[0].rule.description;
    delete plan.rules[0].rule.protocols![0].port;
    delete plan.expectations[0].labs;
    expect(renderRuleTable(plan, binding)).toContain("tcp/any");
    expect(renderRuleTable(plan, binding)).not.toContain("undefined");
    expect(renderPlanTests(plan, binding, "hardening", "positive")).toContain("A1:");
  });
  it("trusts detect-only verdicts and renders bridged rows as invariant", () => {
    const plan = structuredClone(US_PLANS[0]);
    plan.label = "partial";
    plan.actions.find((a) => a.id === "modbus-dpi")!.chosen = true;
    plan.expectations[0].bridged = true;
    expect(renderPlanTests(plan, binding, "hardening", "negative")).toContain("allow | deny");
    expect(renderPlanTests(plan, binding, "hardening", "positive")).toContain("invariant | bridged");
  });
  it.each(EU_PLANS)("never synthesizes US addresses or ICS for EU $label", (plan) => {
    const before = JSON.stringify(plan);
    expect(renderRuleTable(plan, binding)).not.toContain("20000");
    expect(JSON.stringify(plan)).toBe(before);
  });
});
