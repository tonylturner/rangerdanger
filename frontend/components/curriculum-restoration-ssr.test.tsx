import { afterEach, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CurriculumScopeProvider, scopeOf } from "../lib/curriculum-scope";
import { US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { decisionName, FIREWALL_TRACK, writeCurriculum } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { saveRemediationPlan } from "../lib/remediation-plan";
import { useFirewallTrack } from "../lib/use-firewall-track";
import { DecisionBlock, FindingsPanel } from "./scenario-description-blocks";
import { RemediationPlanBanner } from "./remediation-plan-banner";

function TrackSnapshot() {
  return <span data-track={useFirewallTrack().track} />;
}

afterEach(() => vi.unstubAllGlobals());

it("keeps server snapshots independent of browser curriculum storage for hydration", () => {
  const browser = Object.assign(new EventTarget(), { localStorage: memoryStorage() });
  vi.stubGlobal("window", browser);
  const pkg = { ...US_PACKAGE, curriculum: US_CURRICULUM };
  const scope = scopeOf(pkg);
  writeCurriculum(scope, decisionName("requirements", "first"), "BLOCK");
  writeCurriculum(scope, FIREWALL_TRACK, "technical");
  saveRemediationPlan(scope, { exerciseId: US_CURRICULUM.roles.planning, selectedActionIds: [], savedAt: "saved" });
  const html = renderToStaticMarkup(<CurriculumScopeProvider pkg={pkg}>
    <DecisionBlock scenarioId="requirements" decisionId="first" options={["ALLOW", "BLOCK"]} body="Decision" />
    <FindingsPanel sourceScenario="requirements" title="Findings" items={[{ id: "first", label: "Recorded finding" }]} />
    <RemediationPlanBanner currentExerciseId={US_CURRICULUM.roles.hardening} />
    <TrackSnapshot />
  </CurriculumScopeProvider>);
  expect(html).toMatch(/<option value=""[^>]*selected=""/);
  expect(html).not.toContain("Your Remediation Plan");
  expect(html).toContain("No remediation plan on file");
  expect(html).toContain("haven&#x27;t recorded findings");
  expect(html).toContain('data-track="guided"');
});
