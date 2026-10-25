import { afterEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import type { ReactNode } from "react";
import type { Scenario } from "../lib/api";
import type { CurriculumScope } from "../lib/curriculum-storage";
import { US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { loadProgress, saveProgress } from "../lib/scenario-runner-storage";
import { ScenarioRunner } from "./scenario-runner";

const { scope } = vi.hoisted(() => ({
  scope: { packageId: "us-dnp3-substation", revision: 1 } as CurriculumScope,
}));
vi.mock("../lib/curriculum-scope", () => ({ useCurriculumScope: () => scope }));
vi.mock("../lib/use-firewall-track", () => ({ useFirewallTrack: () => ({ track: "guided", setTrack: vi.fn() }) }));
vi.mock("../lib/operator-queries", () => ({
  useOperatorView: () => ({ status: "loading" }), usePackagePolicies: () => undefined,
}));
vi.mock("../lib/live-queries", () => ({
  useActiveFirewall: () => ({}), useSubstationState: () => ({}),
  useSubstationAudit: () => ({}), useRefreshLive: () => vi.fn(),
}));
vi.mock("./runner-operator-status", () => ({ RunnerOperatorStatus: () => null }));
vi.mock("./scenario-description-blocks", () => ({
  DescriptionSegments: () => null, RemediationPlanSummary: () => null,
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

afterEach(() => {
  scope.curriculum = undefined;
  vi.unstubAllGlobals();
});

describe("Reset Progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])(
    "returns %s from Summary to the first usable step and preserves other records", (packageId) => {
      scope.packageId = packageId;
      vi.stubGlobal("window", { localStorage: memoryStorage(), confirm: () => true });
      saveProgress(scope, scenario.id, { completedStepIds: ["first", "second"], notes: "old", cmdLog: ["old"] });
      saveProgress(scope, "another", { completedStepIds: ["kept"], notes: "keep", cmdLog: [] });
      let tree!: ReactTestRenderer;
      act(() => { tree = create(<ScenarioRunner scenario={scenario} onExit={() => {}} />); });
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
      }
    },
  );
});

it("offers the reference and manual commit workflow, never the advisory generated plan", () => {
  scope.curriculum = US_CURRICULUM;
  vi.stubGlobal("window", { localStorage: memoryStorage() });
  vi.stubGlobal("document", { addEventListener: vi.fn(), removeEventListener: vi.fn() });
  let tree!: ReactTestRenderer;
  act(() => { tree = create(<ScenarioRunner scenario={{ ...scenario, id: US_CURRICULUM.roles.implementation }} onExit={() => {}} />); });
  try {
    const text = JSON.stringify(tree.toJSON());
    expect(text).toContain("Apply Hardened");
    expect(text).toContain("Lab 1.4 plans are advisory");
    expect(text).not.toContain("Apply Your Plan");
  } finally {
    act(() => tree.unmount());
  }
});
