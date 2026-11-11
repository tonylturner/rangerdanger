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
  it("shows Weak deferred exposure, selected and required origins, logging and readable inspection", () => {
    const empty = renderRuleTable(US_PLANS[0], binding);
    expect(empty).toContain("deferred");
    expect(empty).toContain("Weak exposure open");
    const full = US_PLANS[2];
    const before = JSON.stringify(full);
    const text = renderRuleTable(full, binding);
    expect(text).toContain("selected");
    expect(text).toContain("required");
    expect(text).toContain("ICS selectors: Protocol: Modbus; Function codes: 1, 2, 3, 4, 5, 6");
    expect(text).toContain("ICS selectors: Protocol: DNP3; Function codes: 1");
    expect(text).not.toContain('"functionCode":');
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
    expect(table).toContain("| Rule | From | To | Port | Action | Log |");
    expect(table).not.toContain("Verdict");
    const rows = table.split("\n");
    const observations = rows.filter((row) => row.includes("Origin: observation:"));
    expect(observations).toHaveLength(3);
    for (const [i, row] of observations.entries()) {
      expect(rows[rows.indexOf(row) - 1]).toContain(i === 0 ? "| would deny (monitor) |" : "| would allow (monitor) |");
      expect(row).toContain("observation: Monitor IEC 104 commands (iec104-dpi) — monitor only, not an L4 rule");
      expect(row).not.toMatch(/\| (ALLOW|DENY) \|/);
    }
    const required = rows.findIndex((row) => row.includes("control-centre-to-rtu-iec104:"));
    expect(rows[required]).toContain("| ALLOW |");
    expect(rows[required + 1]).toContain("Origin: required");
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
  it.each([...US_PLANS, ...EU_PLANS])("keeps every $packageId $label rule and its details in a six-column table", (plan) => {
    const table = renderRuleTable(plan, plan.packageId === "eu-iec104-substation" ? eu : binding);
    const rows = table.split("\n");
    expect(rows).toHaveLength(2 + 2 * plan.rules.length);
    for (const row of rows) expect(row.match(/(?<!\\)\|/g)).toHaveLength(7);
    for (const [i, { rule }] of plan.rules.entries()) {
      expect(rows[2 + 2 * i]).toContain(rule.id);
      expect(rows[3 + 2 * i]).toContain("**Rule details:** Origin:");
      if (rule.ics) {
        expect(rows[3 + 2 * i]).toContain("ICS selectors:");
        expect(table).not.toContain(JSON.stringify(rule.ics));
      }
    }
  });
  it("preserves every IEC selector, including false flags and zero qualifiers, without JSON", () => {
    const plan = structuredClone(EU_PLANS[2]);
    plan.rules[0].rule.ics = {
      protocol: "iec104", mode: "monitor", direction: "request", readOnly: false,
      typeIds: [46, 47], classes: ["control"], ca: [1], ioa: [{ start: 3001, end: 3001 }, { start: 3301, end: 3302 }],
      cot: [6], originators: [0], negative: false, test: false, selectExecute: "select", commandStates: [1, 2], commandQualifiers: [0],
    };
    const table = renderRuleTable(plan, eu);
    for (const selector of [
      "Protocol: IEC 104", "Mode: monitor", "Direction: request", "Read only: no", "Type IDs: 46, 47", "Classes: control",
      "Common addresses (CA): 1", "Information object addresses (IOA): 3001, 3301–3302", "Causes of transmission (COT): 6",
      "Originator addresses: 0", "Negative confirmation: no", "Test: no", "Select/execute: select", "Command states: 1, 2", "Command qualifiers: 0",
    ]) expect(table).toContain(selector);
    expect(table).not.toContain('"protocol":');
  });
  it("preserves Modbus restrictions and escapes detail-row Markdown", () => {
    const plan = structuredClone(US_PLANS[2]);
    plan.rules[0].rule.ics = {
      protocol: "modbus", mode: "enforce", direction: "response", readOnly: false, writeOnly: true,
      functionCode: [5, 6], unitId: 0, addresses: ["0-10|20", "30\n40"],
    };
    const table = renderRuleTable(plan, binding);
    expect(table).toContain("Mode: enforce; Read only: no; Function codes: 5, 6; Write only: yes; Unit ID: 0; Addresses: 0-10\\|20, 30 40");
    expect(table.split("\n")).toHaveLength(2 + 2 * plan.rules.length);
  });
});
