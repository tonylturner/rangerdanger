// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import * as api from "../lib/api";
import type { PackageSummary, Scenario, ValidationResult } from "../lib/api";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { loadProgress, loadValidation } from "../lib/scenario-runner-storage";
import { ScenarioRunner } from "./scenario-runner";

vi.mock("../lib/use-compiled-plan", () => ({ useCompiledPlan: () => ({}) }));
vi.mock("../lib/use-firewall-track", () => ({ useFirewallTrack: () => ({ track: "guided", setTrack: vi.fn() }) }));
vi.mock("../lib/operator-queries", () => ({
  useOperatorView: () => ({ status: "loading" }), usePackagePolicies: () => undefined,
}));
vi.mock("../lib/live-queries", async (importOriginal) => ({
  ...await importOriginal<typeof import("../lib/live-queries")>(),
  useActiveFirewall: () => ({}), useSubstationState: () => ({}),
  useSubstationAudit: () => ({}), useRefreshLive: () => vi.fn(),
}));
vi.mock("./runner-operator-status", () => ({ RunnerOperatorStatus: () => null }));
vi.mock("./terminal-context", () => ({ SharedTerminalPanel: () => null }));
vi.mock("./ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const packageIds = ["us-dnp3-substation", "eu-iec104-substation"] as const;
const scenario: Scenario = {
  id: "persistence-fixture", order: "2.3", name: "Persistence fixture", description: "Test",
  package_id: "fixture", lab_template_id: "fixture", validator: "state", tags: [], nodes: [],
  steps: [
    { id: "first", title: "First instruction", description: "", action: { type: "manual" } },
    { id: "second", title: "Second instruction", description: "", action: { type: "manual" } },
    { id: "third", title: "Untouched instruction", description: "", action: { type: "manual" } },
  ],
};
const scope = (packageId: string) => ({ packageId, revision: 1 });
const result = (outcome: "PASS" | "FAIL"): ValidationResult => ({
  scenario_id: scenario.id, outcome, timestamp: "2026-11-01T12:00:00Z",
  checks: [{ name: "Current state", status: outcome === "PASS" ? "pass" : "fail", detail: "Bounded observation" }],
});
const clients: QueryClient[] = [];
const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => {
  Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  vi.restoreAllMocks();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

function mount(initialPackage: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  let packageId = initialPackage, generation = 0;
  const content = () => {
    const pkg: PackageSummary = { id: packageId, revision: 1, title: packageId, active: true, operator_view: "", policies: [] };
    return <QueryClientProvider client={client}><CurriculumScopeProvider pkg={pkg}>
      {/* Match the package/revision/exercise key used by the exercise page. */}
      <ScenarioRunner key={`${packageId}:1:${scenario.id}:${generation}`} scenario={scenario} onExit={() => {}} />
    </CurriculumScopeProvider></QueryClientProvider>;
  };
  const tree = render(content());
  return Object.assign(tree, {
    refresh: () => { generation++; tree.rerender(content()); },
    switchPackage: (next: string) => { packageId = next; tree.rerender(content()); },
  });
}

function markAndSkip(tree: ReturnType<typeof mount>) {
  fireEvent.click(tree.getByRole("button", { name: "Mark Complete" }));
  fireEvent.click(tree.getByRole("button", { name: "Skip Step" }));
}
function expectProgress(tree: ReturnType<typeof mount>, packageId: string) {
  expect(tree.container.textContent).toContain("1/3 steps marked complete · 1 skipped");
  expect(loadProgress(scope(packageId), scenario)).toMatchObject({
    completedStepIds: ["first"], skippedStepIds: ["second"],
  });
}
async function validate(tree: ReturnType<typeof mount>, outcome: "PASS" | "FAIL") {
  fireEvent.click(tree.getByRole("button", { name: "Validate Exercise" }));
  await waitFor(() => expect(tree.container.textContent).toContain(`Last validation: ${outcome}`));
  expect(tree.container.textContent).toContain("Historical result, not proof of current lab state or step completion.");
}

describe.each(packageIds)("runner persistence in %s", (packageId) => {
  it.each(["PASS", "FAIL"] as const)("page refresh restores %s separately from completed/skipped steps", async (outcome) => {
    vi.spyOn(api, "validateScenario").mockResolvedValue(result(outcome));
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, outcome);
    tree.refresh();
    expectProgress(tree, packageId);
    expect(tree.container.textContent).toContain(`Last validation: ${outcome}`);
    expect(loadValidation(scope(packageId), scenario.id)).toEqual(result(outcome));
    fireEvent.click(tree.getByRole("button", { name: "View Summary" }));
    expect(tree.container.textContent).toContain("Step 2: Second instruction — Skipped");
    expect(tree.container.textContent).toContain("•Step 3: Untouched instruction");
  });

  it("lab reset preserves historical outcome and student progress through refresh", async () => {
    vi.spyOn(api, "validateScenario").mockResolvedValue(result("PASS"));
    const reset = vi.spyOn(api, "resetWorkshop").mockResolvedValue({ success: true, actions: [] });
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, "PASS");
    fireEvent.click(tree.getByRole("button", { name: "Reset Lab" }));
    await waitFor(() => expect(loadProgress(scope(packageId), scenario).cmdLog.join(" ")).toContain("Lab restored"));
    expect(reset).toHaveBeenCalledTimes(1);
    tree.refresh();
    expectProgress(tree, packageId);
    expect(tree.container.textContent).toContain("Last validation: PASS");
  });

  it.each(["PASS", "FAIL"] as const)("a second validation after %s changes only the outcome", async (first) => {
    const second = first === "PASS" ? "FAIL" : "PASS";
    vi.spyOn(api, "validateScenario").mockResolvedValueOnce(result(first)).mockResolvedValueOnce(result(second));
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, first);
    await validate(tree, second);
    tree.refresh();
    expectProgress(tree, packageId);
    expect(tree.container.textContent).toContain(`Last validation: ${second}`);
    expect(loadValidation(scope(packageId), scenario.id)).toEqual(result(second));
  });

  it("a failed validation request preserves the last recorded outcome and progress", async () => {
    vi.spyOn(api, "validateScenario").mockResolvedValueOnce(result("PASS")).mockRejectedValueOnce(new Error("offline"));
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, "PASS");
    fireEvent.click(tree.getByRole("button", { name: "Validate Exercise" }));
    await waitFor(() => expect(tree.getByRole("alert").textContent).toContain("Validation request failed"));
    tree.refresh();
    expectProgress(tree, packageId);
    expect(tree.container.textContent).toContain("Last validation: PASS");
  });

  it("explicit Reset Progress clears self-attested progress, not the historical outcome", async () => {
    vi.spyOn(api, "validateScenario").mockResolvedValue(result("FAIL"));
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, "FAIL");
    fireEvent.click(tree.getByRole("button", { name: "Reset Progress" }));
    tree.refresh();
    expect(tree.container.textContent).toContain("0/3 steps marked complete");
    expect(loadProgress(scope(packageId), scenario)).toEqual({ completedStepIds: [], skippedStepIds: [], notes: "", cmdLog: [] });
    expect(tree.container.textContent).toContain("Last validation: FAIL");
  });

  it("switches packages and back without merging progress or outcomes", async () => {
    const other = packageIds.find((id) => id !== packageId)!;
    vi.spyOn(api, "validateScenario").mockResolvedValueOnce(result("PASS")).mockResolvedValueOnce(result("FAIL"));
    const tree = mount(packageId);
    markAndSkip(tree);
    await validate(tree, "PASS");
    tree.switchPackage(other);
    expect(tree.container.textContent).toContain("0/3 steps marked complete");
    expect(tree.container.textContent).not.toContain("Last validation:");
    fireEvent.click(tree.getByRole("button", { name: "Skip Step" }));
    fireEvent.click(tree.getByRole("button", { name: "Mark Complete" }));
    await validate(tree, "FAIL");
    tree.switchPackage(packageId);
    expectProgress(tree, packageId);
    expect(tree.container.textContent).toContain("Last validation: PASS");
    tree.switchPackage(other);
    expect(tree.container.textContent).toContain("Last validation: FAIL");
    expect(loadProgress(scope(other), scenario)).toMatchObject({ completedStepIds: ["second"], skippedStepIds: ["first"] });
  });
});
