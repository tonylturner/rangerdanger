import { afterEach, describe, expect, it, vi } from "vitest";
import { getFieldAudit, getFieldState } from "./operator-api";
import { AUDIT, TRUTH } from "./operator-fixtures.test-util";

afterEach(() => vi.unstubAllGlobals());

describe("physical feeder reads", () => {
  it.each([
    ["/field/state", getFieldState, TRUTH],
    ["/field/audit?after=0", getFieldAudit, AUDIT],
  ] as const)("reads %s without a mutation", async (path, read, answer) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(answer), {
      status: 200, headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(read()).resolves.toEqual(answer);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(new RegExp(path.replace("?", "\\?") + "$"));
    expect(fetchMock.mock.calls[0][1]?.method).toBeUndefined();
  });
});
