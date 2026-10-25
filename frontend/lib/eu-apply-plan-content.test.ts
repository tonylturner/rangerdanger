import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scenario } from "./api";
import { fixtureCurriculum } from "./curriculum-storage.test-util";
import { EU_PLANS } from "./plan-fixtures.test-util";
import { computeCoverage } from "./requirement-coverage";
import { injectDynamicContent } from "./scenario-runner-logic";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
const root = resolve(__dirname, "../..");
const euDir = "lab-definitions/packages/eu-iec104-substation";
const eu = { binding: fixtureCurriculum(euDir), plans: EU_PLANS, scenarios: `${euDir}/scenarios`, prefix: "eu-", auth: 10, unauth: 27 };
function scenario(dir: string, id: string): Scenario {
  return load(readFileSync(resolve(root, dir, `${id}.yml`), "utf8")) as Scenario;
}

describe("EU Apply Your Plan content", () => {
  const { binding, plans, scenarios, prefix, auth, unauth } = eu;
  it("keeps common implementation instructions under empty, partial and full plans", () => {
    const lab = scenario(scenarios, `${prefix}firewall-implementation`);
    for (const [id, phase] of Object.entries(binding.dynamicSteps)) {
      const step = lab.steps.find((s) => s.id === id)!;
      expect(step).toBeDefined();
      for (const plan of plans) {
        const text = injectDynamicContent(step.description, id, plan, binding);
        // Only the generated section differs. Operational work is never hidden.
        expect(text).toContain(step.description.trim());
        expect(text).not.toMatch(/\{\{(?:ruleTable|tests|noPlan)\}\}/);
        expect(text).toContain(phase === "rules" ? "Origin" : "Plan verdict");
        if (phase !== "rules") expect(text).toContain("Hardened verdict");
      }
    }
  });

  it("gives no prevention coverage to detection or planning alone", () => {
    const kinds = new Map(plans[0].actions.map((a) => [a.id, a.kind]));
    const notEnforce = new Set(plans[0].actions.filter((a) => a.kind !== "enforce").map((a) => a.id));
    for (const [id, req] of Object.entries(binding.requirements)) {
      for (const [verdict, actions] of Object.entries(req.impl)) {
        if (!["BLOCK", "BLOCK and LOG", "RESTRICT"].includes(verdict)) continue;
        expect(actions.some((a) => kinds.get(a) === "enforce"), `${id}: ${verdict}`).toBe(true);
        expect(computeCoverage([{ id, label: req.label, verdict }], notEnforce, binding)[0].status).not.toBe("covered");
        expect(actions.filter((a) => kinds.get(a) === "detect" && verdict !== "BLOCK and LOG")).toEqual([]);
        expect(actions.filter((a) => kinds.get(a) === "plan")).toEqual([]);
      }
    }
  });

  it("documents the package proof counts and independent results", () => {
    const validation = scenario(scenarios, `${prefix}validation-evidence`);
    const text = validation.steps.map((s) => s.description).join("\n");
    expect(plans[0].expectations.filter((r) => r.category === "authorized")).toHaveLength(auth);
    expect(plans[0].expectations.filter((r) => r.category === "unauthorized")).toHaveLength(unauth);
    for (const expected of ["Plan proof", "Hardened acceptance", `${auth}/${auth}`, `${unauth}/${unauth}`, "0 skipped", "hash", "capture"]) {
      expect(text).toContain(expected);
    }
    expect(text).not.toMatch(/11\/11|12\/12|nineteen probes|not \*\*Apply Your Plan/);
  });

  it("names each pivot owner and keeps restoration common", () => {
    const vendor = scenario(scenarios, `${prefix}vendor-rdp-compromise`);
    const text = vendor.steps.map((s) => s.description).join("\n");
    expect(text).toContain("restrict-enterprise-to-vendor");
    expect(text).toContain("block-vendor-to-field");
    expect(text).toContain("Restore");
    const hardening = scenario(scenarios, `${prefix}hardening-configurations`);
    const retest = hardening.steps.find((s) => s.id === "re-test-the-attack-under-the-hardened-policy")!.description;
    expect(retest).toContain("block-enterprise-to-station");
    expect(retest).toContain("still");
    expect(retest).toContain("Restore");
    expect(retest).not.toMatch(/evade DPI|not enforced until Phase 4|Planned Phase 4/);
  });
});

it("retains EU CR-22 and post-commit reconnect for every plan", () => {
  const binding = eu.binding;
  const lab = scenario(`${euDir}/scenarios`, "eu-firewall-implementation");
  for (const plan of EU_PLANS) {
    const text = lab.steps.map((s) => injectDynamicContent(s.description, s.id, plan, binding)).join("\n");
    for (const expected of ["CR-22", "accidentally omits CC→RTU", "POST http://localhost:8088/api/operator/reconnect", "link.since", "PLC-originated", "GI"]) {
      expect(text).toContain(expected);
    }
  }
});
