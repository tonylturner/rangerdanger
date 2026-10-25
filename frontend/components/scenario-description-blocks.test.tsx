import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { CurriculumBinding, PackageSummary } from "../lib/api";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { fixtureCurriculum, US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { buildDynamicPlan } from "../lib/remediation-to-rules";
import { PlanCoveragePanel, RemediationPlanSummary } from "./scenario-description-blocks";
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
    expect(html).toContain("planned, not enforceable in this range yet");
    expect(html).toContain("planning-only");
  });

  it("renders nothing for the same planning directives without a binding", () => {
    expect(render()).toBe("");
  });

  it("retains the US DPI notice and generic no-plan catalog link", () => {
    expect(render(US_CURRICULUM)).toContain('href="/exercises"');
    const usPlan = buildDynamicPlan({ exerciseId: US_CURRICULUM.roles.planning, selectedActionIds: ["modbus-dpi"], savedAt: "" }, US_CURRICULUM);
    const html = renderToStaticMarkup(<RemediationPlanSummary plan={usPlan} />);
    expect(html).toContain('<span class="font-bold">ICS DPI enabled</span> - containd provides Modbus/DNP3 function-code visibility and detects and logs writes; the L4 source pin enforces field access from the RTAC source address');
    const euPlan = buildDynamicPlan({ exerciseId: EU.roles.planning, selectedActionIds: ["iec104-dpi"], savedAt: "" }, EU);
    const euHtml = renderToStaticMarkup(<RemediationPlanSummary plan={euPlan} />);
    expect(euHtml).toContain("not enforceable in this range yet");
    expect(euHtml).not.toContain("ICS DPI enabled");
  });
});
