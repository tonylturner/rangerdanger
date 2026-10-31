// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it } from "vitest";
import { CurriculumScopeProvider, scopeOf } from "../lib/curriculum-scope";
import { fixtureCurriculum } from "../lib/curriculum-storage.test-util";
import { decisionName, writeCurriculum } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { loadSavedRemediationPlan, saveRemediationPlan } from "../lib/remediation-plan";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { PlanCoveragePanel } from "./scenario-description-blocks";

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() }));
afterEach(() => {
  cleanup();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

for (const packageId of ["us-dnp3-substation", "eu-iec104-substation"]) {
  const binding = fixtureCurriculum(`lab-definitions/packages/${packageId}`);
  const pkg = { ...US_PACKAGE, id: packageId, curriculum: binding };
  const scope = scopeOf(pkg);
  const vendor = packageId.startsWith("us") ? "restrict-vendor-to-ot" : "restrict-vendor-to-station";
  const setAnswer = (id: string, value: string) => act(() =>
    writeCurriculum(scope, decisionName(binding.roles.requirements, id), value));

  it(`${packageId}: shows adjusted caps/effort, retains exclusions and restores defaults`, () => {
    saveRemediationPlan(scope, { exerciseId: binding.roles.planning, selectedActionIds: [vendor], savedAt: "saved" });
    setAnswer("ot-eng-availability", "STRETCHED");
    setAnswer("fw-admin-skills", "VENDOR_LED");
    setAnswer("vendor-change-window", "NEGOTIATING");
    const tree = render(<CurriculumScopeProvider pkg={pkg}><DecisionPanel exerciseId={binding.roles.planning} /></CurriculumScopeProvider>);
    expect(tree.container.textContent).toContain("120h → 60h");
    expect(tree.container.textContent).toContain("140h → 80h");
    expect(tree.container.textContent).toContain("50h → 75h");
    expect(tree.container.textContent).toContain("25h/role");
    setAnswer("ot-eng-availability", "UNAVAILABLE");
    expect(tree.container.textContent).toContain("120h → 0h");
    expect(tree.container.innerHTML).not.toContain("NaN");
    expect(tree.container.textContent).toContain("0 eligible selected");
    expect(screen.getByRole("status").textContent).toContain("UNAVAILABLE");
    const blocked = screen.getByRole("button", { name: (name) => name.startsWith(binding.catalog.find((a) => a.id === "ot-asset-inventory")!.title) }) as HTMLButtonElement;
    expect(blocked.disabled).toBe(true);
    expect(loadSavedRemediationPlan(scope)?.selectedActionIds).toEqual([vendor]);
    fireEvent.click(screen.getByRole("checkbox", { name: "Use default budget" }));
    expect(tree.container.textContent).toContain("1 eligible selected");
    expect(tree.container.textContent).not.toContain("120h → 0h");
    expect(screen.queryByRole("status")).toBeNull();
    tree.unmount();
    render(<CurriculumScopeProvider pkg={pkg}><DecisionPanel exerciseId={binding.roles.planning} /></CurriculumScopeProvider>);
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(true);
  });

  it(`${packageId}: downstream coverage/banner react without reopening the picker; applied history stays intact`, () => {
    saveRemediationPlan(scope, { exerciseId: binding.roles.planning, selectedActionIds: binding.requirements["enterprise-to-field"].impl.BLOCK, savedAt: "saved" });
    setAnswer("enterprise-to-field", "BLOCK");
    const applied = { packageId, revision: pkg.revision, actionIds: binding.requirements["enterprise-to-field"].impl.BLOCK, label: "partial" as const, policyHash: "original-hash", appliedAt: "then" };
    const tree = render(<CurriculumScopeProvider pkg={pkg}>
      <RemediationPlanBanner currentExerciseId={binding.roles.hardening} appliedPlan={applied} />
      <PlanCoveragePanel title="Draft coverage" />
    </CurriculumScopeProvider>);
    expect(tree.container.textContent).toContain("1 / 1 covered");
    setAnswer("risk-acceptance-authority", "DISPUTED");
    expect(tree.container.textContent).toContain("0 / 1 covered");
    expect(tree.container.textContent).toContain(`${applied.actionIds.length} action${applied.actionIds.length === 1 ? "" : "s"} applied in your plan`);
    expect(tree.container.textContent).toContain("Applied-plan provenance and proof expectations are unchanged");
    expect(tree.container.textContent).toContain("DISPUTED");
    expect(applied.actionIds).toEqual(binding.requirements["enterprise-to-field"].impl.BLOCK);
    setAnswer("risk-acceptance-authority", "CLEAR");
    expect(tree.container.textContent).toContain("1 / 1 covered");
  });
}
