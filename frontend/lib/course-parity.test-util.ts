import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import type { Scenario } from "./api";
import { fixtureCurriculum } from "./curriculum-storage.test-util";
import { US_PLANS, EU_PLANS } from "./plan-fixtures.test-util";
import { renderPlanTests } from "./plan-presentation";
import { injectDynamicContent } from "./scenario-runner-logic";
import { scenarioFiles } from "./scenario-sources.test-util";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };

export function coursePresentationProjection() {
  return Object.fromEntries(["us-dnp3-substation", "eu-iec104-substation"].map((id) => {
    const dir = `lab-definitions/packages/${id}`;
    const c = fixtureCurriculum(dir);
    const plans = id.startsWith("us-") ? US_PLANS : EU_PLANS;
    const scenarios = scenarioFiles(resolve(__dirname, "../..", dir)).map((file) => load(readFileSync(file, "utf8")) as Scenario);
    return [id, Object.fromEntries(scenarios.map((scenario) => {
      const role = Object.entries(c.roles).find(([, sid]) => sid === scenario.id)![0];
      return [scenario.id, plans.map((plan) => ({
        positive: [...renderPlanTests(plan, c, role, "positive").matchAll(/^\| ([^:|]+):/gm)].map((m) => m[1]),
        negative: [...renderPlanTests(plan, c, role, "negative").matchAll(/^\| ([^:|]+):/gm)].map((m) => m[1]),
        dynamic: Object.fromEntries(scenario.steps.filter((step) => role === "implementation" && c.dynamicSteps[step.id]).map((step) => [
          step.id, createHash("sha256").update(injectDynamicContent(step.description, step.id, plan, c, role)).digest("hex"),
        ])),
      }))];
    }))];
  }));
}
