import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type ReactNode } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
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
const output = (tree: ReactTestRenderer) => JSON.stringify(tree.toJSON());
let tree: ReactTestRenderer | undefined;
let browser: EventTarget & { localStorage: Storage };

beforeEach(() => {
  browser = Object.assign(new EventTarget(), { localStorage: memoryStorage() });
  vi.stubGlobal("window", browser);
});
afterEach(() => {
  act(() => tree?.unmount());
  tree = undefined;
  vi.unstubAllGlobals();
});

function mount(children: ReactNode) {
  let mounted!: ReactTestRenderer;
  act(() => { mounted = create(wrap(children)); });
  tree = mounted;
  return mounted;
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
    expect(mounted.root.findByType("select").props.value).toBe("BLOCK");
    act(() => mounted.update(wrap(decision("second"))));
    expect(mounted.root.findByType("select").props.value).toBe("");
    act(() => mounted.root.findByType("select").props.onChange({ target: { value: "ALLOW" } }));
    expect(readCurriculum(scope, decisionName("requirements", "second"))).toBe("ALLOW");
    act(() => mounted.update(wrap(decision("second", ["BLOCK"]))));
    expect(mounted.root.findByType("select").props.value).toBe("");
    act(() => mounted.update(wrap(decision("first"), pkg.revision + 1)));
    expect(mounted.root.findByType("select").props.value).toBe("");
  });

  it("inherits once, persists to the destination, and clears provenance on manual editing", () => {
    writeCurriculum(scope, decisionName("upstream", "finding"), "BLOCK");
    const mounted = mount(<DecisionBlock scenarioId="design" decisionId="verdict" options={["ALLOW", "BLOCK"]}
      defaultFrom="upstream:finding" body="Verdict" />);
    expect(mounted.root.findByType("select").props.value).toBe("BLOCK");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe("BLOCK");
    expect(output(mounted)).toContain("inherited from earlier lab");
    act(() => mounted.root.findByType("select").props.onChange({ target: { value: "ALLOW" } }));
    expect(output(mounted)).not.toContain("inherited from earlier lab");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe("ALLOW");
    act(() => mounted.root.findByType("button").props.onClick());
    expect(mounted.root.findByType("select").props.value).toBe("");
    expect(readCurriculum(scope, decisionName("design", "verdict"))).toBe(null);
  });

  it("restores findings on mount and reloads them for a different source or scope", () => {
    writeCurriculum(scope, decisionName("first", "finding"), "BLOCK");
    writeCurriculum(scope, decisionName("second", "finding"), "ALLOW");
    const findings = (sourceScenario: string) =>
      <FindingsPanel sourceScenario={sourceScenario} title="Findings" items={[{ id: "finding", label: "Recorded finding" }]} />;
    const mounted = mount(findings("first"));
    expect(output(mounted)).toContain("BLOCK");
    act(() => mounted.update(wrap(findings("second"))));
    expect(output(mounted)).toContain("ALLOW");
    expect(output(mounted)).not.toContain("BLOCK");
    act(() => mounted.update(wrap(findings("second"), pkg.revision + 1)));
    expect(output(mounted)).not.toContain("ALLOW");
  });

  it("restores the remediation plan and reloads it when the exercise or scope changes", () => {
    const plan = { exerciseId: US_CURRICULUM.roles.planning, selectedActionIds: ["block-enterprise-to-field"], savedAt: "saved" };
    saveRemediationPlan(scope, plan);
    const banner = (id: string) => <RemediationPlanBanner currentExerciseId={id} />;
    const mounted = mount(banner(US_CURRICULUM.roles.hardening));
    const actionCount = () => mounted.root.findAllByType("div").find((node) => node.children.includes(" action"))!.children.join("");
    expect(output(mounted)).toContain("Your Remediation Plan");
    expect(actionCount()).toBe("1 action planned");
    expect(output(mounted)).not.toContain("No remediation plan on file");
    saveRemediationPlan(scope, { ...plan, selectedActionIds: [] });
    const nextExercise = Object.entries(US_CURRICULUM.activities)
      .find(([id, activity]) => !activity.hideBanner && id !== US_CURRICULUM.roles.hardening)![0];
    act(() => mounted.update(wrap(banner(nextExercise))));
    expect(actionCount()).toBe("0 actions planned");
    act(() => mounted.update(wrap(banner(US_CURRICULUM.roles.hardening), pkg.revision + 1)));
    expect(output(mounted)).toContain("No remediation plan on file");
    act(() => mounted.update(wrap(banner(US_CURRICULUM.roles.planning))));
    expect(mounted.toJSON()).toBe(null);
  });

  it("restores the saved track and synchronizes both tabs and scopes", () => {
    writeCurriculum(scope, FIREWALL_TRACK, "technical");
    const choices = <><TrackChoice /><TrackChoice /></>;
    const mounted = mount(choices);
    const tracks = () => mounted.root.findAllByType("button").map((button) => button.props["data-track"]);
    expect(tracks()).toEqual(["technical", "technical"]);
    act(() => mounted.root.findAllByType("button")[0].props.onClick());
    expect(tracks()).toEqual(["guided", "guided"]);
    browser.localStorage.setItem(curriculumKey(scope, FIREWALL_TRACK), "technical");
    const event = new Event("storage");
    Object.defineProperty(event, "key", { value: curriculumKey(scope, FIREWALL_TRACK) });
    act(() => { browser.dispatchEvent(event); });
    expect(tracks()).toEqual(["technical", "technical"]);
    act(() => mounted.update(wrap(choices, pkg.revision + 1)));
    expect(tracks()).toEqual(["guided", "guided"]);
  });
});
