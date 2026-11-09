import { afterEach, expect, it, vi } from "vitest";
import { ApiError, compilePlan, applyPlan, executeScenarioStep, validateScenario, getActiveFirewallConfig, getSubstationNetworkEvents, sendFieldAction, stopTrafficGeneration, type NetworkEventsResponse } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("posts a simulation action only to the field endpoint and returns physical state", async () => {
  const state = { schema: 3, position: "open", lockout: false };
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(state)));
  vi.stubGlobal("fetch", fetch);
  const body = { device: "feeder-breaker", action: "reset_lockout" } as const;
  await expect(sendFieldAction(body)).resolves.toEqual(state);
  expect(fetch).toHaveBeenCalledExactlyOnceWith("/api/field/actions", expect.objectContaining({
    method: "POST", body: JSON.stringify(body),
  }));
});

it("preserves field action failure status and detail without a retry", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response('{"error":"device unreachable"}', { status: 502 }));
  vi.stubGlobal("fetch", fetch);
  await expect(sendFieldAction({ device: "rmu-1", action: "reset_fault_passage" })).rejects.toMatchObject({
    status: 502, detail: "device unreachable",
  });
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("retains monitor decision metadata and incomplete-history or engine-error evidence", async () => {
  const answer: NetworkEventsResponse = {
    source: "containd",
    since: "2026-10-31T12:00:00Z",
    historyTruncated: true,
    engineError: "engine unavailable",
    events: [{
      id: "shadow", timestamp: "2026-10-31T12:01:00Z", kind: "ics.decision",
      attributes: { decision: "would_drop", mode: "monitor", ruleId: "observe-cc-breaker-command", action: "DENY", reason: "rule" },
    }],
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  await expect(getSubstationNetworkEvents()).resolves.toEqual(answer);
});

it("names the observed run in an additive traffic stop request", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response('{"status":"stopping"}'));
  vi.stubGlobal("fetch", fetch);
  await expect(stopTrafficGeneration("run-to-stop")).resolves.toEqual({ status: "stopping" });
  expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({
    method: "POST", body: '{"run_id":"run-to-stop"}',
  }));
});

const planRequest = { packageId: "us-dnp3-substation", revision: 2, actionIds: [] };
it.each([[compilePlan, "/api/plan/compile"], [applyPlan, "/api/firewall/apply-plan"]] as const)("posts scoped IDs, not browser rules, to %s", async (call, path) => {
  const fetch = vi.fn().mockResolvedValue(new Response('{}'));
  vi.stubGlobal("fetch", fetch);
  await call(planRequest);
  expect(fetch).toHaveBeenCalledWith(path, expect.objectContaining({ method: "POST", body: JSON.stringify(planRequest) }));
});
it.each([400, 409, 422])("preserves failure warnings at status %i", async (status) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "Plan rejected", warnings: ["Over budget"] }), { status })));
  await expect(applyPlan(planRequest)).rejects.toMatchObject({ status, detail: "Plan rejected", warnings: ["Over budget"] });
  expect(new ApiError(status, "x").warnings).toEqual([]);
});
it("reads active plan provenance without relabelling custom", async () => {
  const answer = { active_config: "custom", policy_source: "plan", plan: { ...planRequest, label: "weak", policyHash: "abc", appliedAt: "now" } };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  await expect(getActiveFirewallConfig()).resolves.toEqual(answer);
});

it.each([undefined, planRequest, { ...planRequest, actionIds: ["pin-rtac-to-field"] }])(
  "POSTs interactive grading with optional scoped IDs: %j", async (changeRequest) => {
    const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response("{}")));
    vi.stubGlobal("fetch", fetch);
    await executeScenarioStep("lab/id", 2, changeRequest);
    await validateScenario("lab/id", changeRequest);
    const options = expect.objectContaining({ method: "POST", body: JSON.stringify({ changeRequest }) });
    expect(fetch).toHaveBeenNthCalledWith(1, "/api/scenarios/lab%2Fid/steps/2/execute", options);
    expect(fetch).toHaveBeenNthCalledWith(2, "/api/scenarios/lab%2Fid/validate", options);
  },
);
