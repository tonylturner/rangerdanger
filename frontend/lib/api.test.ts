import { afterEach, expect, it, vi } from "vitest";
import { runValidationReport, stopTrafficGeneration, type ValidationReport } from "./api";

afterEach(() => vi.unstubAllGlobals());

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
