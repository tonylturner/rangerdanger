import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PlanProvenance } from "../lib/api";
import { useCompiledPlan } from "../lib/use-compiled-plan";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import type { CurriculumScope } from "../lib/curriculum-storage";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { saveRemediationPlan } from "../lib/remediation-plan";

let panel: ReactTestRenderer;
let client: QueryClient;
afterEach(() => { act(() => panel?.unmount()); client?.clear(); vi.unstubAllGlobals(); });

function View({ scope, applied }: { scope: CurriculumScope; applied?: PlanProvenance }) {
  const query = useCompiledPlan(scope, true, applied, !!applied);
  return <div>{query.error?.message ?? query.data?.policyHash ?? "loading"}</div>;
}
const scope: CurriculumScope = { packageId: US_PLANS[0].packageId, revision: 2, curriculum: US_CURRICULUM };
async function mount(applied?: PlanProvenance) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  vi.stubGlobal("window", Object.assign(new EventTarget(), { localStorage: memoryStorage() }));
  vi.stubGlobal("document", new EventTarget());
  await act(async () => {
    panel = create(<QueryClientProvider client={client}><View scope={scope} applied={applied} /></QueryClientProvider>);
  });
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)); });
}

describe("compiled plan reads", () => {
  it("compiles an empty plan without requiring a saved nonempty selection", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(US_PLANS[0])));
    vi.stubGlobal("fetch", fetch);
    await mount();
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ packageId: scope.packageId, revision: scope.revision, actionIds: [] });
    expect(JSON.stringify(panel.toJSON())).toContain(US_PLANS[0].policyHash);
  });
  it("uses applied provenance, never draft edits, for proof expectations", async () => {
    const plan = US_PLANS[1];
    const applied = { packageId: plan.packageId, revision: plan.revision, actionIds: ["restrict-enterprise-to-vendor"], label: plan.label, policyHash: plan.policyHash, appliedAt: "now" };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(plan)));
    vi.stubGlobal("fetch", fetch);
    await mount(applied);
    saveRemediationPlan(scope, { exerciseId: "draft", selectedActionIds: ["modbus-dpi"], savedAt: "now" });
    await act(async () => { window.dispatchEvent(new Event("focus")); });
    expect(JSON.parse(fetch.mock.calls[0][1].body).actionIds).toEqual(applied.actionIds);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("rejects a compile response whose hash differs from the applied snapshot", async () => {
    const plan = US_PLANS[0];
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(plan))));
    await mount({ packageId: plan.packageId, revision: plan.revision, actionIds: [], label: plan.label, policyHash: `${plan.policyHash}-different`, appliedAt: "now" });
    expect(JSON.stringify(panel.toJSON())).toContain("no longer matches the applied plan hash");
  });
  it("does not reuse draft IDs after a package switch", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(US_PLANS[0])))
      .mockResolvedValueOnce(new Response(JSON.stringify(EU_PLANS[0])));
    vi.stubGlobal("fetch", fetch);
    await mount();
    const euScope = { ...scope, packageId: EU_PLANS[0].packageId };
    saveRemediationPlan(euScope, { exerciseId: "draft", selectedActionIds: ["iec104-dpi"], savedAt: "now" });
    await act(async () => { panel.update(<QueryClientProvider client={client}><View scope={euScope} /></QueryClientProvider>); });
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ packageId: euScope.packageId, revision: euScope.revision, actionIds: ["iec104-dpi"] });
  });
});
