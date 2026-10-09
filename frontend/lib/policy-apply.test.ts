import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { applyPackagePolicy, policyApplyErrorText } from "./policy-apply";
import { RangeNotReadyError } from "./range";

function respond(statusCode: number, body: unknown) {
  const fetchMock = vi.fn(async () => new Response(JSON.stringify(body), { status: statusCode }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("applyPackagePolicy", () => {
  it("applies the named policy and refreshes the firewall views", async () => {
    const fetchMock = respond(200, { status: "applied", active_config: "improved" });
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    await applyPackagePolicy(client, "improved");
    expect(fetchMock).toHaveBeenCalledWith("/api/firewall/apply", expect.objectContaining({ method: "POST", body: '{"config":"improved"}' }));
    expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
      ["firewall-rules"],
      ["workshop", "status"],
      ["firewall-active"],
    ]);
  });

  it("surfaces a failed apply with the backend's text and refreshes nothing", async () => {
    respond(502, { error: "failed to apply config to containd: connection refused" });
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const error = await applyPackagePolicy(client, "improved").catch((e: unknown) => e);
    expect(policyApplyErrorText("improved", error)).toBe(
      "Could not apply improved: 502 failed to apply config to containd: connection refused",
    );
    expect(invalidate).not.toHaveBeenCalled();
  });
});

describe("policyApplyErrorText", () => {
  it("names the policy as the student picked it", () => {
    expect(policyApplyErrorText("Hardened", new Error("Failed to fetch"))).toBe("Could not apply Hardened: Failed to fetch");
  });

  it("leaves a not-ready range to the range banner's words", () => {
    const error = new RangeNotReadyError("starting");
    expect(policyApplyErrorText("weak", error)).toBe(error.message);
  });
});
