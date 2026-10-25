import { afterEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
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
    { id: "first", title: "First instruction", description: "Start here" },
    { id: "second", title: "Second instruction", description: "Continue" },
  ],
};

afterEach(() => vi.unstubAllGlobals());

describe("Reset Progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])(
    "returns %s from Summary to the first usable step and preserves other records", (packageId) => {
      scope.packageId = packageId;
      vi.stubGlobal("window", Object.assign(new EventTarget(), { localStorage: memoryStorage(), confirm: () => true }));
      vi.stubGlobal("document", new EventTarget());
      const client = new QueryClient();
      saveProgress(scope, scenario.id, { completedStepIds: ["first", "second"], notes: "old", cmdLog: ["old"] });
      saveProgress(scope, "another", { completedStepIds: ["kept"], notes: "keep", cmdLog: [] });
      let tree!: ReactTestRenderer;
      act(() => { tree = create(<QueryClientProvider client={client}><ScenarioRunner scenario={scenario} onExit={() => {}} /></QueryClientProvider>); });
      const click = (label: string) => act(() => tree.root.findByProps({ "aria-label": label }).props.onClick());
      try {
        click("View Summary");
        expect(JSON.stringify(tree.toJSON())).toContain("Exercise Summary");
        click("Reset Progress");
        expect(JSON.stringify(tree.toJSON())).not.toContain("Exercise Summary");
        expect(JSON.stringify(tree.toJSON())).toContain("First instruction");
        expect(tree.root.findByType("textarea").props.value).toBe("");
        expect(loadProgress(scope, scenario)).toEqual({ completedStepIds: [], notes: "", cmdLog: [] });
        expect(loadProgress(scope, { id: "another", steps: [{ id: "kept", title: "", description: "" }] }).notes).toBe("keep");
        const complete = tree.root.findAllByType("button").find((b) => b.children.includes("Mark Complete"))!;
        act(() => complete.props.onClick());
        expect(JSON.stringify(tree.toJSON())).toContain("Second instruction");
      } finally {
        act(() => tree.unmount());
        client.clear();
      }
    },
  );
});
