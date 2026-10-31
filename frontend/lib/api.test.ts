import { afterEach, expect, it, vi } from "vitest";
import { ApiError, compilePlan, applyPlan, getActiveFirewallConfig, getSubstationNetworkEvents, runValidationReport, stopTrafficGeneration, type NetworkEventsResponse, type ValidationReport } from "./api";

afterEach(() => vi.unstubAllGlobals());

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

it.each([
  ["EU", "sha256:" + "a".repeat(64), true],
  ["US", "", false],
] as const)("reads %s validation proof metadata without changing the verdict", async (_, policy_hash, policy_stable) => {
  const answer: ValidationReport = {
    planProof: { applicable: false, pass: false, policyHash: "", hashStable: false, skipped: 0, rows: [] },
    acceptance: { pass: true, skipped: 0, rows: [], preventionGaps: [] },
    markdown: "# Segmentation evidence",
    summary: {
      authorized_pass: 1,
      authorized_total: 1,
      unauthorized_pass: 1,
      unauthorized_total: 1,
      result: "PASS",
    },
    active_config: "custom",
    policy_source: "manual-custom",
    pcap_path: "/captures/validation.pcap",
    policy_hash,
    window_start: "2026-10-18T12:00:00.123456789Z",
    window_end: "2026-10-18T12:00:09.987654321Z",
    policy_stable,
  };
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(answer)));
  vi.stubGlobal("fetch", fetch);

  await expect(runValidationReport()).resolves.toEqual(answer);
  expect(fetch).toHaveBeenCalledWith("/api/firewall/validation-report", expect.objectContaining({ method: "POST" }));
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
