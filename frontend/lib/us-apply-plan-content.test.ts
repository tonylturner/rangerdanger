import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scenario } from "./api";
import { US_CURRICULUM as binding } from "./curriculum-storage.test-util";
import { US_PLANS } from "./plan-fixtures.test-util";
import { computeCoverage } from "./requirement-coverage";
import { injectDynamicContent } from "./scenario-runner-logic";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
function scenario(id: string): Scenario {
  return load(readFileSync(resolve(__dirname, "../../lab-definitions/scenarios", `${id}.yml`), "utf8")) as Scenario;
}

describe("US Apply Your Plan content", () => {
  it("renders rules and both verdicts for empty, partial and full plans", () => {
    const lab = scenario("firewall-implementation");
    for (const [id, phase] of Object.entries(binding.dynamicSteps)) {
      const step = lab.steps.find((s) => s.id === id)!;
      expect(step).toBeDefined();
      for (const plan of US_PLANS) {
        const text = injectDynamicContent(step.description, id, plan, binding);
        expect(text).not.toMatch(/\{\{(?:ruleTable|tests|noPlan)\}\}/);
        expect(text).not.toMatch(/fixed blocked outcome|not \*\*Apply Your Plan/);
        if (phase === "rules") {
          expect(text).toContain("Origin");
          expect(text).toContain("**Export plan record**");
          for (const row of plan.rules) expect(text).toContain(row.rule.id);
        } else {
          expect(text).toContain("Plan verdict");
          expect(text).toContain("Hardened verdict");
          for (const row of plan.expectations.filter((r) => r.category === (phase === "positive" ? "authorized" : "unauthorized"))) {
            expect(text).toContain(`${row.id}:`);
          }
        }
      }
    }
  });

  it("pins revision two's 8+13 proof universe and partial-plan gaps", () => {
    for (const plan of US_PLANS) {
      expect(plan.revision).toBe(2);
      expect(plan.expectations.filter((r) => r.category === "authorized")).toHaveLength(8);
      expect(plan.expectations.filter((r) => r.category === "unauthorized")).toHaveLength(13);
    }
    const partial = US_PLANS[1];
    expect(partial.actions.filter((a) => a.chosen).map((a) => a.id)).toEqual(["restrict-enterprise-to-vendor"]);
    expect(partial.expectations.filter((r) => r.planVerdict !== r.hardenedVerdict).map((r) => r.id))
      .toEqual(Array.from({ length: 11 }, (_, i) => `U${i + 1}`));
    const text = scenario("validation-evidence").steps.map((s) => s.description).join("\n");
    for (const required of ["Plan proof", "Hardened acceptance", "8/8", "13/13", "0 skipped", "hash", "capture", "healthy HMI"]) {
      expect(text).toContain(required);
    }
    expect(text).not.toMatch(/11\/11|12\/12|nineteen probes/);
  });

  it("requires L4 controls for prevention and logging only for BLOCK and LOG", () => {
    const kinds = new Map(US_PLANS[0].actions.map((a) => [a.id, a.kind]));
    const nonEnforce = new Set(US_PLANS[0].actions.filter((a) => a.kind !== "enforce").map((a) => a.id));
    for (const [id, req] of Object.entries(binding.requirements)) {
      for (const [verdict, actions] of Object.entries(req.impl)) {
        expect(actions.some((a) => kinds.get(a) === "enforce"), `${id}: ${verdict}`).toBe(true);
        expect(actions.filter((a) => kinds.get(a) === "plan")).toEqual([]);
        expect(actions.filter((a) => kinds.get(a) === "detect")).toEqual(verdict === "BLOCK and LOG" ? ["improve-logging"] : []);
        expect(computeCoverage([{ id, label: req.label, verdict }], nonEnforce, binding)[0].status).not.toBe("covered");
      }
    }
    const closures = ["block-enterprise-to-field", "block-vendor-to-field", "pin-rtac-to-field"];
    expect(binding.requirements["unauth-modbus-writes"].impl.BLOCK).toEqual(closures);
    expect(binding.requirements["unauth-modbus-writes"].impl["BLOCK and LOG"]).toEqual([...closures, "improve-logging"]);
  });

  it("labels every catalog action by its compiled kind", () => {
    const text = scenario("remediation-planning").steps[2].description;
    const rows = text.split("\n").filter((line) => /^\| (Enforceable|Detection-only|Planning-only) /.test(line));
    const labels = { enforce: "Enforceable", detect: "Detection-only", plan: "Planning-only" };
    for (const action of US_PLANS[0].actions) {
      expect(rows.find((line) => line.startsWith(`| ${labels[action.kind]} |`))).toContain(`\`${action.id}\``);
    }
    for (const required of ["empty", "partial", "Budget overrun is a warning only"]) expect(text).toContain(required);
  });

  it("names retest owners and preserves restoration without fixed HMI answers", () => {
    for (const [id, index, owners, decision] of [
      ["hardening-configurations", 4, ["block-enterprise-to-field"], "retest-hmi-state"],
      ["vendor-rdp-compromise", 5, ["restrict-enterprise-to-vendor", "block-vendor-to-field"], "vendor-retest-hmi-state"],
    ] as const) {
      const lab = scenario(id);
      const text = lab.steps.map((s) => s.description).join("\n");
      for (const owner of owners) expect(text).toContain(owner);
      const retest = lab.steps[index].description;
      expect(retest).toMatch(/deferred|Deferred/);
      expect(retest).toContain("Restore");
      const prompt = retest.split("\n").find((line) => line.includes(`id=${decision} `));
      expect(prompt).toBeDefined();
      expect(prompt).not.toContain("correct=");
      expect(retest).not.toMatch(/evade DPI|RTAC-origin write/);
    }
  });
});
