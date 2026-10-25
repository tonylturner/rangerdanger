import { buildContaindConfig } from "./advisory-policy.test-util";
import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { US_CURRICULUM } from "./curriculum-storage.test-util";
import { buildDynamicPlan, positiveValidationTests, negativeValidationTests } from "./remediation-to-rules";
import { injectDynamicContent } from "./scenario-runner-logic";

const root = resolve(__dirname, "../..");
const fixture = JSON.parse(readFileSync(resolve(root, "scripts/tests/fixtures/rd01-full-plan.json"), "utf8"));

describe("US package hotfix contracts", () => {
  const plan = buildDynamicPlan({ exerciseId: "remediation-planning", selectedActionIds: fixture.selectedActionIds, savedAt: "" }, US_CURRICULUM);

  it("retains the advisory full-plan fixture's actual generated policy", () => {
    expect(buildContaindConfig(plan)).toEqual(fixture.config);
  });

  it("injects factual reference guidance through stable step IDs", () => {
    const text = injectDynamicContent("replaced YAML", "phase-3-create-minimal-required-rules", plan);
    expect(text).toContain("Hardened reference");
    expect(text).toContain("design review only");
    expect(text).toContain("export config");
    expect(text).toContain("**Commit**");
    expect(text).toContain("The L4 source pin enforces; DPI detects and logs");
    expect(text).not.toContain("export config >");
    expect(text).not.toContain("vi /tmp/");
    const negative = injectDynamicContent("replaced YAML", "phase-6-validate-blocked-traffic", plan);
    expect(negative).toContain("config.commit");
    expect(negative).toContain("11/11");
    expect(negative).not.toContain("deny entries");
  });

  it("uses terminating, supported read commands and the working SSH account", () => {
    const text = [...positiveValidationTests(plan), ...negativeValidationTests(plan)].join("\n");
    expect(text).toContain("-c 4 -1 -t 0");
    expect(text).toContain("dnp3poll -a 1 -t 3s 10.40.40.20:20000");
    expect(text).toContain("rangerdanger@");
    expect(text).not.toContain(" eng@");
    expect(text).not.toContain("-t 1");
    expect(text).not.toContain("${ip:");
  });
});
