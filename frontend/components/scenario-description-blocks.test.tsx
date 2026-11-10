// @vitest-environment jsdom
import { act, fireEvent, render as rtlRender, screen } from "@testing-library/react";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CurriculumBinding, PackageSummary } from "../lib/api";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { fixtureCurriculum, US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { curriculumKey, decisionName, REMEDIATION_PLAN, writeCurriculum } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { CompiledPlanSummary } from "./compiled-plan-summary";
import { CommandBlock, DecisionBlock, FindingsPanel, PlanCoveragePanel } from "./scenario-description-blocks";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { TooltipProvider } from "./ui/tooltip";

vi.mock("next/link", async () => {
  const { createElement } = await import("react");
  return {
    default: ({ href, children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) =>
      createElement("a", { href, ...props }, children),
  };
});

const EU = fixtureCurriculum("backend/internal/labs/testdata/curriculum-eu");
function mountDecision({ correct, explanation = "" }: { correct?: string; explanation?: string }) {
  const pkg: PackageSummary = {
    id: "us-dnp3-substation", revision: 1, title: "US", active: true,
    operator_view: "us-dnp3", policies: [], curriculum: US_CURRICULUM,
  };
  return rtlRender(
    <CurriculumScopeProvider pkg={pkg}>
      <DecisionBlock
        scenarioId="decision-feedback-test"
        decisionId="verdict"
        options={["BLOCK", "ALLOW"]}
        body="What should happen?"
        explanation={explanation}
        correct={correct}
      />
    </CurriculumScopeProvider>,
  );
}

beforeEach(() => vi.stubGlobal("localStorage", memoryStorage()));
afterEach(() => vi.unstubAllGlobals());

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

describe("configuration CLI command blocks", () => {
  it("uses a neutral marker, stays copy-only and copies only the command", () => {
    const writeText = vi.fn();
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onRun = vi.fn();
    const { container } = rtlRender(
      <CommandBlock cmd="show running-config" runId="cli" runningId={null} onRun={onRun} cli />,
    );
    expect(container.textContent).toContain("CLI · ");
    expect(container.textContent).not.toMatch(/containd(?:#|\(config)/);
    expect(screen.getByText("containd cli")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Run" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    expect(writeText).toHaveBeenCalledExactlyOnceWith("show running-config");
    expect(onRun).not.toHaveBeenCalled();
  });
});

describe("shared decision feedback", () => {
  it("shows the keyed correct chip and markdown explanation only after an answer", () => {
    const tree = mountDecision({ correct: "BLOCK", explanation: "Reference design: **BLOCK**." });
    expect(screen.queryByText("✓ Correct")).toBeNull();
    expect(screen.queryByText("Why")).toBeNull();

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "BLOCK" } });

    expect(screen.getByText("✓ Correct")).toBeTruthy();
    expect(screen.getByText("Why")).toBeTruthy();
    expect(tree.container.querySelector("strong")?.textContent).toBe("BLOCK");
  });

  it("shows the expected answer for an incorrect keyed answer", () => {
    mountDecision({ correct: "BLOCK", explanation: "Reference design: **BLOCK**." });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "ALLOW" } });
    expect(screen.getByText("✗ Expected BLOCK")).toBeTruthy();
    expect(screen.getByText("Why")).toBeTruthy();
    expect(screen.queryByText(/capture|doesn.t match/i)).toBeNull();
  });

  it("shows unkeyed explanation without a grade chip and hides it when cleared", () => {
    mountDecision({ explanation: "Compare the consequences of each option." });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "ALLOW" } });
    expect(screen.queryByText("✓ Correct")).toBeNull();
    expect(screen.queryByText("✗ Expected")).toBeNull();
    expect(screen.getByText("Why")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "clear" }));
    expect(screen.queryByText("Why")).toBeNull();
    expect(screen.queryByText(/Compare the consequences/)).toBeNull();
  });
});

describe("findings panel source label", () => {
  it("labels the upstream link by the scenario order, not its raw id", () => {
    const pkg: PackageSummary = {
      id: "us-dnp3-substation", revision: 1, title: "US", active: true,
      operator_view: "us-dnp3", policies: [], curriculum: US_CURRICULUM,
    };
    const tree = rtlRender(
      <CurriculumScopeProvider pkg={pkg}>
        <FindingsPanel
          sourceScenario="eu-baseline-assessment"
          sourceScenarioOrder="1.2"
          title="Findings"
          items={[{ id: "flow", label: "Flow" }]}
        />
      </CurriculumScopeProvider>,
    );
    expect(tree.getByRole("link", { name: "Lab 1.2" }).getAttribute("href")).toBe("/exercises/eu-baseline-assessment");
    expect(tree.container.textContent).not.toContain("Lab eu-baseline-assessment");
  });
});

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
    expect(html).not.toContain("for your plan");
  });

  it("renders nothing for the same planning directives without a binding", () => {
    expect(render()).toBe("");
  });

  it("shows compiler action kinds with one wording for both packages", () => {
    expect(render(US_CURRICULUM)).toContain(`href="/exercises/${US_CURRICULUM.roles.planning}"`);
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

describe("live shared curriculum panels", () => {
  it("updates decisions in the same tab and plan coverage from another tab", () => {
    const storage = memoryStorage();
    const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
    const pkg: PackageSummary = {
      id: "fixture",
      revision: 3,
      title: "Fixture",
      active: true,
      operator_view: "us-dnp3",
      policies: [],
      curriculum: US_CURRICULUM,
    };
    const scope = { packageId: pkg.id, revision: pkg.revision, curriculum: pkg.curriculum };
    const dispatchCrossTabStorage = (key: string) => {
      const event = new Event("storage");
      Object.defineProperty(event, "key", { value: key });
      fireEvent(window, event);
    };

    try {
      const tree = rtlRender(
        <CurriculumScopeProvider pkg={pkg}>
          <>
            <DecisionPanel exerciseId={US_CURRICULUM.roles.planning} />
            <PlanCoveragePanel title="Coverage" />
          </>
        </CurriculumScopeProvider>,
      );
      const requirementKey = curriculumKey(
        scope,
        decisionName(US_CURRICULUM.roles.requirements, "enterprise-to-field"),
      );
      // The same-tab write event refreshes both readers.
      act(() => writeCurriculum(
        scope,
        decisionName(US_CURRICULUM.roles.requirements, "enterprise-to-field"),
        "BLOCK",
      ));
      expect(tree.container.textContent).toContain("BLOCK");
      expect(tree.container.textContent).toContain("deferred");
      expect(tree.container.textContent).toContain("Current selection coverage only, not firewall proof");

      storage.setItem(
        curriculumKey(scope, REMEDIATION_PLAN),
        JSON.stringify({
          exerciseId: US_CURRICULUM.roles.planning,
          selectedActionIds: ["block-enterprise-to-field"],
          savedAt: "another tab",
        }),
      );
      dispatchCrossTabStorage(curriculumKey(scope, REMEDIATION_PLAN));
      expect(tree.container.textContent).toContain("planned actions selected");
      expect(tree.container.textContent).not.toContain("deferred. Implementing actions");
      expect(screen.getByRole("button", { name: /Block enterprise → field direct access/ }).className).toContain("border-sky-600");

      storage.setItem(requirementKey, "ALLOW");
      dispatchCrossTabStorage(requirementKey);
      expect(tree.container.textContent).toContain("ALLOW");
      expect(tree.container.textContent).toContain("design says ALLOW");
      expect(tree.container.textContent).not.toContain("BLOCK, planned actions selected");
    } finally {
      vi.restoreAllMocks();
      if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
      else Reflect.deleteProperty(window, "localStorage");
    }
  });
});

describe("copy-only long-running commands", () => {
  it("names the target terminal and has no Run action", () => {
    const tree = rtlRender(<CommandBlock cmd="tcpdump -i any" runId="capture" runningId={null} onRun={vi.fn()} sourceNode="fw-1" copyOnly interactive />);
    expect(tree.queryByRole("button", { name: "Run" })).toBeNull();
    expect(tree.getByText("Long-running: paste into the fw-1 terminal and leave it running.")).toBeTruthy();
    expect(tree.getByRole("button", { name: "copy" }).className).toContain("group-focus-within:opacity-100");
  });
});
