import { afterEach, describe, expect, it, vi } from "vitest";
import { getFirewallSessions } from "./operator-api";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getFirewallSessions", () => {
  it("serializes the destination filter with the other tuple filters", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ sessions: [], count: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getFirewallSessions({
      dport: 2404,
      src: "10.60.60.20",
      dst: "10.30.30.20",
    })).resolves.toEqual({ sessions: [], count: 0 });

    expect(String(fetchMock.mock.calls[0][0])).toMatch(
      /\/firewall\/sessions\?dport=2404&src=10\.60\.60\.20&dst=10\.30\.30\.20$/,
    );
  });
});
