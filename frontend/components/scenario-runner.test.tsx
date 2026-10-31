// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Scenario } from "../lib/api";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { loadProgress, saveProgress } from "../lib/scenario-runner-storage";
import { ScenarioRunner } from "./scenario-runner";

const { scope } = vi.hoisted(() => ({
  scope: { packageId: "us-dnp3-substation", revision: 1 },
}));
vi.mock("../lib/curriculum-scope", () => ({ useCurriculumScope: () => scope }));
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
vi.mock("./scenario-description-blocks", () => ({
  DescriptionSegments: () => null,
}));
vi.mock("./scenario-runner-panels", () => ({
  CommandAuditPanels: () => null, StepNavigator: () => null, TerminalPanel: () => null,
}));
vi.mock("./ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const scenario: Scenario = {
  id: "reset-fixture", order: "1.2", name: "Reset fixture", description: "Test",
  package_id: "fixture", lab_template_id: "fixture", validator: "", tags: [], nodes: [],
  steps: [
    { id: "first", title: "First instruction", description: "Start here", action: { type: "decision" } },
    { id: "second", title: "Second instruction", description: "Continue", action: { type: "manual" } },
    { id: "third", title: "Third instruction", description: "Run this", action: { type: "probe" } },
  ],
};

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

describe("Reset Progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])(
    "returns %s from Summary to the first usable step and preserves other records", async (packageId) => {
      scope.packageId = packageId;
      Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
      vi.spyOn(window, "confirm").mockReturnValue(true);
      const client = new QueryClient();
      saveProgress(scope, scenario.id, { completedStepIds: ["first", "second"], notes: "old", cmdLog: ["old"] });
      saveProgress(scope, "another", { completedStepIds: ["kept"], notes: "keep", cmdLog: [] });
      const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={scenario} onExit={() => {}} /></QueryClientProvider>);
      const click = (label: string) => fireEvent.click(tree.getByRole("button", { name: label }));
      try {
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        expect(tree.container.textContent).toContain("Mark Complete is self-attested; it does not run a system check.");
        click("View Summary");
        expect(tree.container.textContent).toContain("Steps marked complete");
        expect(tree.container.textContent).toContain("Mark Complete is self-attested; it does not run a system check.");
        click("Reset Progress");
        expect(tree.container.textContent).not.toContain("Exercise Summary");
        expect(tree.container.innerHTML).toContain("First instruction");
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        expect((tree.getByRole("textbox") as HTMLTextAreaElement).value).toBe("");
        expect(loadProgress(scope, scenario)).toEqual({ completedStepIds: [], notes: "", cmdLog: [] });
        expect(loadProgress(scope, { id: "another", steps: [{ id: "kept", title: "", description: "" }] }).notes).toBe("keep");
        fireEvent.click(tree.getByRole("button", { name: /Mark Complete/ }));
        expect(tree.container.innerHTML).toContain("Second instruction");
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        fireEvent.click(tree.getByRole("button", { name: /Mark Complete/ }));
        expect(tree.container.innerHTML).toContain("Third instruction");
        await waitFor(() => expect(tree.getByRole("button", { name: "Execute Step" })).toBeTruthy());
      } finally {
        client.clear();
      }
    },
  );
});
