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

describe("increment-4 operator contract", () => {
  it.each([
    ["feeder-breaker-command", "open"], ["feeder-breaker-command", "close"],
    ["rmu-1-command", "open"], ["rmu-1-command", "close"],
    ["rmu-2-command", "open"], ["rmu-2-command", "close"],
    ["oltc-1-step", "lower"], ["oltc-1-step", "raise"],
    ["oltc-1-mode", "auto"], ["oltc-1-mode", "manual"],
  ] as const)("sends one select/execute request for %s %s", async (point, operation) => {
    const { sendOperatorCommand } = await import("./operator-api");
    const { commandResult } = await import("./operator-fixtures.test-util");
    const result = { ...commandResult("completed"), point, operation };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(result)));
    vi.stubGlobal("fetch", fetchMock);
    await expect(sendOperatorCommand({ point, operation })).resolves.toEqual(result);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/operator\/commands$/);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST", body: JSON.stringify({ point, operation }) });
  });

  it("hard-cuts CC and field reads to schema 2", async () => {
    const { getOperatorState } = await import("./operator-api");
    const { FEEDER_STATE } = await import("./operator-fixtures.test-util");
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(FEEDER_STATE)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...FEEDER_STATE, schema: 1 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...TRUTH, process: { ...TRUTH.process, schema: 1 } })));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getOperatorState()).resolves.toEqual(FEEDER_STATE);
    await expect(getOperatorState()).rejects.toThrow("Expected CC state schema 2");
    await expect(getFieldState()).rejects.toThrow("Expected field and process schema 2");
  });

  it("preserves the CC reason on a 409 refusal", async () => {
    const { sendOperatorCommand } = await import("./operator-api");
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ reason: "feedback_stale" }), { status: 409 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(sendOperatorCommand({ point: "oltc-1-step", operation: "raise" })).rejects.toMatchObject({ status: 409, detail: "feedback_stale" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});


it("does not accept the old command-result wire shape", async () => {
  const { sendOperatorCommand } = await import("./operator-api");
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: "old", status: "completed", select: {}, execute: {}, feedback: {} }))));
  await expect(sendOperatorCommand({ point: "rmu-1-command", operation: "open" })).rejects.toThrow("command outcome is unknown");
});
