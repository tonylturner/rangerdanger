// @vitest-environment jsdom
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PlanProvenance } from "../lib/api";
import { useCompiledPlan } from "../lib/use-compiled-plan";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { fixtureCurriculum, US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { curriculumKey, decisionName, writeCurriculum, type CurriculumScope } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { saveRemediationPlan } from "../lib/remediation-plan";

let panel: ReturnType<typeof render>;
let client: QueryClient;
const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
afterEach(() => {
  panel?.unmount();
  client?.clear();
  vi.unstubAllGlobals();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

function View({ scope, applied, target }: { scope: CurriculumScope; applied?: PlanProvenance; target?: "request" }) {
  const query = useCompiledPlan(scope, true, applied, target ?? (applied ? "applied" : "draft"));
  return <div>{query.error?.message ?? query.data?.policyHash ?? "loading"}
    {target && query.data?.expectations.map((row) => <span key={row.id}>{row.id}:{row.planVerdict}</span>)}
  </div>;
}
const scope: CurriculumScope = { packageId: US_PLANS[0].packageId, revision: US_PLANS[0].revision, curriculum: US_CURRICULUM };
async function mount(applied?: PlanProvenance, expectedText = applied?.policyHash ?? US_PLANS[0].policyHash) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
  panel = render(<QueryClientProvider client={client}><View scope={scope} applied={applied} /></QueryClientProvider>);
  await waitFor(() => expect(panel.container.textContent).toContain(expectedText));
}

describe("compiled plan reads", () => {
  for (const [packageDir, plans] of [
    ["us-dnp3-substation", US_PLANS],
    ["eu-iec104-substation", EU_PLANS],
  ] as const) {
    it(`follows the saved request, or full reference when absent/empty/excluded, for ${packageDir}`, async () => {
      const curriculum = fixtureCurriculum(`lab-definitions/packages/${packageDir}`);
      const requestScope = { packageId: plans[0].packageId, revision: plans[0].revision, curriculum };
      const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(plans[0]))));
      vi.stubGlobal("fetch", fetch);
      client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
      Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
      panel = render(<QueryClientProvider client={client}><View scope={requestScope} target="request" /></QueryClientProvider>);
      const expectReference = async (call: number) => {
        await waitFor(() => expect(fetch).toHaveBeenCalledTimes(call));
        expect(JSON.parse(fetch.mock.calls[call - 1][1].body).actionIds).toEqual(curriculum.catalog.map((a) => a.id));
        // Full-reference negatives must not inherit Weak's allowed exposures.
        await waitFor(() => {
          for (const row of plans[0].expectations) expect(panel.container.textContent).toContain(`${row.id}:${row.hardenedVerdict}`);
        });
      };
      await expectReference(1);
      act(() => saveRemediationPlan(requestScope, { exerciseId: curriculum.roles.planning, selectedActionIds: [], savedAt: "now" }));
      expect(fetch).toHaveBeenCalledTimes(1); // Absent and empty have the same target.
      act(() => saveRemediationPlan(requestScope, { exerciseId: curriculum.roles.planning, selectedActionIds: ["block-enterprise-to-field"], savedAt: "now" }));
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
      expect(JSON.parse(fetch.mock.calls[1][1].body).actionIds).toEqual(["block-enterprise-to-field"]);
      await waitFor(() => {
        for (const row of plans[0].expectations) expect(panel.container.textContent).toContain(`${row.id}:${row.planVerdict}`);
      });
      act(() => writeCurriculum(requestScope, decisionName(curriculum.roles.requirements, "risk-acceptance-authority"), "DISPUTED"));
      await expectReference(3);
    });
  }
  it("compiles an empty plan without requiring a saved nonempty selection", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(US_PLANS[0])));
    vi.stubGlobal("fetch", fetch);
    await mount();
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ packageId: scope.packageId, revision: scope.revision, actionIds: [] });
    expect(panel.container.textContent).toContain(US_PLANS[0].policyHash);
  });
  it("uses applied provenance, never draft edits, for proof expectations", async () => {
    const plan = US_PLANS[1];
    const applied = { packageId: plan.packageId, revision: plan.revision, actionIds: ["restrict-enterprise-to-vendor"], label: plan.label, policyHash: plan.policyHash, appliedAt: "now" };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(plan)));
    vi.stubGlobal("fetch", fetch);
    await mount(applied);
    act(() => saveRemediationPlan(scope, { exerciseId: "draft", selectedActionIds: ["modbus-dpi"], savedAt: "now" }));
    act(() => writeCurriculum(scope, decisionName(US_CURRICULUM.roles.requirements, "ot-eng-availability"), "UNAVAILABLE"));
    act(() => writeCurriculum(scope, decisionName(US_CURRICULUM.roles.requirements, "vendor-change-window"), "NOT_DISCUSSED"));
    fireEvent(window, new Event("focus"));
    expect(JSON.parse(fetch.mock.calls[0][1].body).actionIds).toEqual(applied.actionIds);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("rejects a compile response whose hash differs from the applied snapshot", async () => {
    const plan = US_PLANS[0];
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(plan))));
    await mount({ packageId: plan.packageId, revision: plan.revision, actionIds: [], label: plan.label, policyHash: `${plan.policyHash}-different`, appliedAt: "now" }, "no longer matches the applied plan hash");
    expect(panel.container.textContent).toContain("no longer matches the applied plan hash");
  });
  it("does not reuse draft IDs after a package switch", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(US_PLANS[0])))
      .mockResolvedValueOnce(new Response(JSON.stringify(EU_PLANS[0])));
    vi.stubGlobal("fetch", fetch);
    await mount();
    const euScope = { packageId: EU_PLANS[0].packageId, revision: EU_PLANS[0].revision, curriculum: fixtureCurriculum("lab-definitions/packages/eu-iec104-substation") };
    saveRemediationPlan(euScope, { exerciseId: "draft", selectedActionIds: ["iec104-dpi"], savedAt: "now" });
    panel.rerender(<QueryClientProvider client={client}><View scope={euScope} /></QueryClientProvider>);
    await waitFor(() => expect(panel.container.textContent).toContain(EU_PLANS[0].policyHash));
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ packageId: euScope.packageId, revision: euScope.revision, actionIds: ["iec104-dpi"] });
  });
  it("recompiles a saved draft immediately when readiness changes in this tab", async () => {
    const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(US_PLANS[0]))));
    vi.stubGlobal("fetch", fetch);
    await mount();
    act(() => saveRemediationPlan(scope, { exerciseId: "draft", selectedActionIds: ["block-enterprise-to-field"], savedAt: "now" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(JSON.parse(fetch.mock.calls[1][1].body).actionIds).toEqual(["block-enterprise-to-field"]);
    act(() => writeCurriculum(scope, decisionName(US_CURRICULUM.roles.requirements, "risk-acceptance-authority"), "DISPUTED"));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
    expect(JSON.parse(fetch.mock.calls[2][1].body).actionIds).toEqual([]);
    const key = curriculumKey(scope, decisionName(US_CURRICULUM.roles.requirements, "risk-acceptance-authority"));
    window.localStorage.setItem(key, "CLEAR");
    fireEvent(window, new StorageEvent("storage", { key }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(4));
    expect(JSON.parse(fetch.mock.calls[3][1].body).actionIds).toEqual(["block-enterprise-to-field"]);
  });
});
