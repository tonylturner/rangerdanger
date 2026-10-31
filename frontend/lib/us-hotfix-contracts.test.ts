import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { US_CURRICULUM } from "./curriculum-storage.test-util";
import { US_PLANS } from "./plan-fixtures.test-util";
import { renderRuleTable, renderPlanTests } from "./plan-presentation";

describe("US compiled-plan presentation", () => {
  it("renders the server rule order and origins without editing config", () => {
    for (const plan of US_PLANS) {
      const before = JSON.stringify(plan);
      const text = renderRuleTable(plan, US_CURRICULUM);
      expect(text).toContain("Source zones / addresses");
      expect(text).toContain("required");
      expect(text.indexOf(plan.rules[0].rule.id)).toBeLessThan(text.indexOf(plan.rules[1].rule.id));
      expect(text).toContain(plan.rules[1].origin.kind);
      expect(JSON.stringify(plan)).toBe(before);
    }
  });
  it("uses server verdicts and both resolved addresses, with owners named", () => {
    const text = renderPlanTests(US_PLANS[0], US_CURRICULUM, "hardening", "negative");
    expect(text).toContain("10.10.10.50");
    expect(text).toContain("10.20.20.10");
    expect(text).toContain("allow | deny");
    expect(text).toContain(US_CURRICULUM.catalog.find((a) => a.id === "block-enterprise-to-field")!.title);
  });
  it("retains safe CLI evidence guidance independently of plan compilation", () => {
    const text = US_CURRICULUM.phases.rules;
    expect(text).toContain("export config");
    expect(text).toContain("**Commit**");
    expect(text).toContain("The L4 source pin enforces; DPI detects and logs");
    expect(text).not.toContain("export config >");
    expect(text).not.toContain("vi /tmp/");
  });
  it("retains supported supplemental reads without using them as policy expectations", () => {
    const text = US_CURRICULUM.phases.positive;
    expect(text).toContain("-c 4 -1 -t 0");
    expect(text).toContain("dnp3poll -a 1 -t 3s 10.40.40.20:20000");
    expect(text).not.toContain("-t 1");
    expect(text).not.toContain("${ip:");
    const supplemental = US_CURRICULUM.tests.map((t) => t.text).join("\n");
    expect(supplemental).toContain("curl -s http://10.30.30.20:8080/api/state | python3 -m json.tool");
    expect(supplemental).toContain("TCP reachability alone is not operational proof");
    // SSH evidence lives in the validation scenario, not the compiled matrix.
    const validation = readFileSync(resolve(__dirname, "../../lab-definitions/scenarios/validation-evidence.yml"), "utf8");
    expect(validation).toContain("rangerdanger@10.30.30.20");
    expect(validation).not.toContain(" eng@");
  });
});
