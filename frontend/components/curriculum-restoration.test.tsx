// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type ReactNode } from "react";
import { CurriculumScopeProvider, scopeOf } from "../lib/curriculum-scope";
import { US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { curriculumKey, decisionName, FIREWALL_TRACK, readCurriculum, writeCurriculum } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { saveRemediationPlan } from "../lib/remediation-plan";
import { useFirewallTrack } from "../lib/use-firewall-track";
import { DecisionBlock, FindingsPanel } from "./scenario-description-blocks";
import { RemediationPlanBanner } from "./remediation-plan-banner";

// Navigation is outside this storage-restoration harness.
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));

const pkg = { ...US_PACKAGE, curriculum: US_CURRICULUM };
const scope = scopeOf(pkg);
const wrap = (children: ReactNode, revision = pkg.revision) =>
  <CurriculumScopeProvider pkg={{ ...pkg, revision }}>{children}</CurriculumScopeProvider>;
const output = (tree: ReturnType<typeof render>) => tree.container.innerHTML;
const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");

beforeEach(() => {
  Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
});
afterEach(() => {
  vi.unstubAllGlobals();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

function mount(children: ReactNode) {
  return render(wrap(children));
}

function TrackChoice() {
  const { track, setTrack } = useFirewallTrack();
  return <button data-track={track} onClick={() => setTrack(track === "technical" ? "guided" : "technical")}>{track}</button>;
}

describe("curriculum restoration", () => {
  it("restores a saved decision and resets on decision, option and scope changes", () => {
    writeCurriculum(scope, decisionName("requirements", "first"), "BLOCK");
    const decision = (id: string, options = ["ALLOW", "BLOCK"]) =>
      <DecisionBlock scenarioId="requirements" decisionId={id} options={options} body="Decision" />;
    const mounted = mount(decision("first"));
    const select = () => mounted.container.querySelector("select") as HTMLSelectElement;
    expect(select().value).toBe("BLOCK");
    mounted.rerender(wrap(decision("second")));
    expect(select().value).toBe("");
    fireEvent.change(select(), { target: { value: "ALLOW" } });
    expect(readCurriculum(scope, decisionName("requirements", "second"))).toBe("ALLOW");
    mounted.rerender(wrap(decision("second", ["BLOCK"])));
    expect(select().value).toBe("");
    mounted.rerender(wrap(decision("first"), pkg.revision + 1));
    expect(select().value).toBe("");
  });

  it("inherits once, persists to the destination, and clears provenance on manual editing", () => {
    writeCurriculum(scope, decisionName("upstream", "finding"), "BLOCK");
    const mounted = mount(<DecisionBlock scenarioId="design" decisionId="verdict" options={["ALLOW", "BLOCK"]}
      defaultFrom="upstream:finding" body="Verdict" />);
    const select = () => mounted.container.querySelector("select") as HTMLSelectElement;
    expect(select().value).toBe("BLOCK");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe("BLOCK");
    expect(output(mounted)).toContain("inherited from earlier lab");
    fireEvent.change(select(), { target: { value: "ALLOW" } });
    expect(output(mounted)).not.toContain("inherited from earlier lab");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe("ALLOW");
    fireEvent.click(mounted.container.querySelector("button")!);
    expect(select().value).toBe("");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe(null);
  });

  it("restores findings on mount and reloads them for a different source or scope", () => {
    writeCurriculum(scope, decisionName("first", "finding"), "BLOCK");
    writeCurriculum(scope, decisionName("second", "finding"), "ALLOW");
    const findings = (sourceScenario: string) =>
      <FindingsPanel sourceScenario={sourceScenario} title="Findings" items={[{ id: "finding", label: "Recorded finding" }]} />;
    const mounted = mount(findings("first"));
    expect(output(mounted)).toContain("BLOCK");
    mounted.rerender(wrap(findings("second")));
    expect(output(mounted)).toContain("ALLOW");
    expect(output(mounted)).not.toContain("BLOCK");
    mounted.rerender(wrap(findings("second"), pkg.revision + 1));
    expect(output(mounted)).not.toContain("ALLOW");
  });

  it("restores the remediation plan and reloads it when the exercise or scope changes", () => {
    const plan = { exerciseId: US_CURRICULUM.roles.planning, selectedActionIds: ["block-enterprise-to-field"], savedAt: "saved" };
    saveRemediationPlan(scope, plan);
    const banner = (id: string) => <RemediationPlanBanner currentExerciseId={id} />;
    const mounted = mount(banner(US_CURRICULUM.roles.hardening));
    const actionCount = () => [...mounted.container.querySelectorAll("div")].find((node) => /^\d+ actions? planned$/.test(node.textContent ?? ""))!.textContent;
    expect(output(mounted)).toContain("Your Remediation Plan");
    expect(actionCount()).toBe("1 action planned");
    expect(output(mounted)).not.toContain("No remediation plan on file");
    saveRemediationPlan(scope, { ...plan, selectedActionIds: [] });
    const nextExercise = Object.entries(US_CURRICULUM.activities)
      .find(([id, activity]) => !activity.hideBanner && id !== US_CURRICULUM.roles.hardening)![0];
    mounted.rerender(wrap(banner(nextExercise)));
    expect(actionCount()).toBe("0 actions planned");
    mounted.rerender(wrap(banner(US_CURRICULUM.roles.hardening), pkg.revision + 1));
    expect(output(mounted)).toContain("No remediation plan on file");
    mounted.rerender(wrap(banner(US_CURRICULUM.roles.planning)));
    expect(mounted.container.innerHTML).toBe("");
  });

  it("restores the saved track and synchronizes both tabs and scopes", () => {
    writeCurriculum(scope, FIREWALL_TRACK, "technical");
    const choices = <><TrackChoice /><TrackChoice /></>;
    const mounted = mount(choices);
    const tracks = () => [...mounted.container.querySelectorAll("button")].map((button) => button.getAttribute("data-track"));
    expect(tracks()).toEqual(["technical", "technical"]);
    fireEvent.click(mounted.container.querySelector("button")!);
    expect(tracks()).toEqual(["guided", "guided"]);
    window.localStorage.setItem(curriculumKey(scope, FIREWALL_TRACK), "technical");
    const event = new Event("storage");
    Object.defineProperty(event, "key", { value: curriculumKey(scope, FIREWALL_TRACK) });
    fireEvent(window, event);
    expect(tracks()).toEqual(["technical", "technical"]);
    mounted.rerender(wrap(choices, pkg.revision + 1));
    expect(tracks()).toEqual(["guided", "guided"]);
  });
});
