import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { CurriculumBinding, PackageSummary } from "../lib/api";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { fixtureCurriculum, US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { CompiledPlanSummary } from "./compiled-plan-summary";
import { PlanCoveragePanel } from "./scenario-description-blocks";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { TooltipProvider } from "./ui/tooltip";

const EU = fixtureCurriculum("backend/internal/labs/testdata/curriculum-eu");
function render(binding?: CurriculumBinding) {
  const pkg: PackageSummary = { id: "fixture", revision: 3, title: "Fixture", active: true, operator_view: "eu-iec104", policies: [], curriculum: binding };
  return renderToStaticMarkup(
    <CurriculumScopeProvider pkg={pkg}>
      <TooltipProvider>
        <DecisionPanel exerciseId={binding?.roles.planning ?? "unbound"} />
        <PlanCoveragePanel title="Coverage" />
        <RemediationPlanBanner currentExerciseId={binding?.roles.hardening ?? "unbound"} />
      </TooltipProvider>
    </CurriculumScopeProvider>,
  );
}

describe("package-bound curriculum presentation", () => {
  it("renders the EU catalog, capacities, role links and truthful effects", () => {
    const html = render(EU);
    expect(html).toContain('href="/exercises/eu-remediation-planning"');
    expect(html).not.toContain('href="/exercises/remediation-planning"');
    expect(html).toContain("500");
    expect(html).toContain("OT Engineer");
    expect(html).toContain("Firewall Admin");
    expect(html).toContain("iec104 dpi");
    expect(html).not.toMatch(/DNP3|RTAC/);
    expect(html).toContain("planned until applied in your plan");
  });

  it("renders nothing for the same planning directives without a binding", () => {
    expect(render()).toBe("");
  });

  it("shows compiler action kinds with one wording for both packages", () => {
    expect(render(US_CURRICULUM)).toContain('href="/exercises"');
    for (const [plans, binding] of [[US_PLANS, US_CURRICULUM], [EU_PLANS, EU]] as const) {
      for (const plan of plans) {
        const html = renderToStaticMarkup(<CompiledPlanSummary plan={plan} binding={binding} />);
        expect(html).toContain("planning only: no firewall change");
        expect(html).toContain("The L4 source pin enforces; DPI detects and logs");
        expect(html).toContain("Export compiled config");
        expect(html).not.toContain("applied in your plan");
        if (plan.label === "weak") expect(html).toContain("Weak exposures remain open");
      }
    }
  });
  it("exports the exact backend config, not a reconstruction from display rules", () => {
    const plan = { ...US_PLANS[1], config: { sentinel: { untouched: [1, 2, 3] } } };
    const html = renderToStaticMarkup(<CompiledPlanSummary plan={plan} binding={US_CURRICULUM} />);
    const href = html.match(/href="data:application\/json;charset=utf-8,([^"]+)"/)![1];
    expect(JSON.parse(decodeURIComponent(href))).toEqual(plan.config);
  });
  it("calls selected actions planned until their applied provenance exists", () => {
    const plan = US_PLANS[1];
    const appliedPlan = { packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id), label: plan.label, policyHash: plan.policyHash, appliedAt: "now" };
    const html = renderToStaticMarkup(<CompiledPlanSummary plan={plan} binding={US_CURRICULUM} appliedPlan={appliedPlan} />);
    expect(html).toContain("applied in your plan");
    expect(html).toContain("Weak exposure open");
  });

});
