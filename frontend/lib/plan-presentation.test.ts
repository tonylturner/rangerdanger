import { describe, expect, it } from "vitest";
import { US_PLANS, EU_PLANS } from "./plan-fixtures.test-util";
import { fixtureCurriculum, US_CURRICULUM } from "./curriculum-storage.test-util";
import { renderPlanTests, renderRuleTable } from "./plan-presentation";

const binding = US_CURRICULUM;
const eu = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");
describe("compiled tables", () => {
  it("includes global proof rows and filters restricted rows by lab role and category", () => {
    // All US proof rows are global; EU has both global and role-scoped rows.
    for (const role of ["implementation", "hardening", "baseline"]) {
      const table = renderPlanTests(US_PLANS[0], binding, role, "negative");
      expect(table.match(/\| U\d+:/g)).toHaveLength(13);
      expect(table).toContain("U2:");
      expect(table).not.toContain("A1:");
    }
    expect(renderPlanTests(EU_PLANS[0], eu, "implementation", "negative")).toContain("U2:");
    expect(renderPlanTests(EU_PLANS[0], eu, "hardening", "negative")).not.toContain("U2:");
    expect(renderPlanTests(EU_PLANS[0], eu, "hardening", "negative")).toContain("U1:");
    expect(renderPlanTests(EU_PLANS[0], eu, "baseline", "negative")).toContain("U20:");
    expect(renderPlanTests(EU_PLANS[0], eu, "baseline", "negative")).not.toContain("U1:");
    expect(renderPlanTests(US_PLANS[0], binding, "baseline", "positive")).toContain("A1:");
    expect(renderPlanTests(US_PLANS[0], binding, "baseline", "negative")).not.toContain("A1:");
  });
  it("shows Weak deferred exposure, selected and required origins, logging and inspection verbatim", () => {
    const empty = renderRuleTable(US_PLANS[0], binding);
    expect(empty).toContain("deferred");
    expect(empty).toContain("Weak exposure open");
    const full = US_PLANS[2];
    const before = JSON.stringify(full);
    const text = renderRuleTable(full, binding);
    expect(text).toContain("selected");
    expect(text).toContain("required");
    expect(text).toContain('"protocol":"modbus","functionCode":[1,2,3,4,5,6]');
    expect(text).toContain('"protocol":"dnp3","functionCode":[1]');
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
  it("labels monitor actions as hypothetical observations, not verdicts or L4 rules", () => {
    const plan = EU_PLANS[2];
    const before = JSON.stringify(plan);
    const table = renderRuleTable(plan, eu);
    expect(table).toContain("| Decision / effect |");
    expect(table).not.toContain("Verdict");
    const observations = table.split("\n").filter((row) => row.includes("| observation:"));
    expect(observations).toHaveLength(3);
    expect(observations[0]).toContain("| would deny (monitor) |");
    expect(observations[1]).toContain("| would allow (monitor) |");
    expect(observations[2]).toContain("| would allow (monitor) |");
    for (const row of observations) {
      expect(row).toContain("observation: Iec104 dpi (iec104-dpi) — monitor only, not an L4 rule");
      expect(row).not.toMatch(/\| (ALLOW|DENY) \|/);
    }
    expect(table.split("\n").find((row) => row.includes("control-centre-to-rtu-iec104:"))).toContain("| ALLOW | required |");
    expect(JSON.stringify(plan)).toBe(before);
  });
  it("trusts detect-only verdicts and renders bridged rows as invariant", () => {
    const plan = structuredClone(US_PLANS[0]);
    plan.actions.find((a) => a.id === "modbus-dpi")!.chosen = true;
    expect(plan.actions.find((a) => a.id === "modbus-dpi")).toMatchObject({ kind: "detect", chosen: true });
    expect(renderPlanTests(plan, binding, "hardening", "negative")).toContain("allow | deny");
    expect(renderPlanTests(plan, binding, "hardening", "positive")).toContain("A4: rtac-1 (10.30.30.20) → openplc-1 (10.30.30.30) TCP/8080 | allow | allow | invariant | bridged");
  });
  it.each(EU_PLANS)("never synthesizes US addresses or changes server ICS for EU $label", (plan) => {
    const before = JSON.stringify(plan);
    expect(renderRuleTable(plan, eu)).not.toContain("20000");
    expect(JSON.stringify(plan)).toBe(before);
  });
});
