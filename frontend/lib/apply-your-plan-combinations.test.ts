import { afterEach, describe, expect, it, vi } from "vitest";
import { compilePlan, applyPlan } from "./api";
import { ALL_PLANS } from "./plan-fixtures.test-util";

afterEach(() => vi.unstubAllGlobals());
describe("server-owned plans across packages and selection states", () => {
  it.each(ALL_PLANS)("round-trips $packageId $label unchanged", async (plan) => {
    const body = { packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id) };
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(plan)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ applied: true, warnings: ["Budget warning"], plan })));
    vi.stubGlobal("fetch", fetch);
    expect(await compilePlan(body)).toEqual(plan);
    expect(await applyPlan(body)).toEqual({ applied: true, warnings: ["Budget warning"], plan });
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body))).toEqual([body, body]);
  });
});
