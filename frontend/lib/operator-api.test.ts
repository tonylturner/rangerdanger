import { afterEach, describe, expect, it, vi } from "vitest";
import { getFieldAudit, getFieldState } from "./operator-api";
import { AUDIT, BREAKER, CLOCK, TRUTH } from "./operator-fixtures.test-util";

afterEach(() => vi.unstubAllGlobals());

describe("physical feeder reads", () => {
  function respond(answer: unknown) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  }

  it("accepts an explicitly absent station clock", async () => {
    const answer = { ...TRUTH, clock: null };
    respond(answer);
    await expect(getFieldState()).resolves.toEqual(answer);
  });

  it.each(["locked", "holdover", "freerun"])("accepts GPS %s state without making it a feeder device", async (sync_status) => {
    const answer = { ...TRUTH, clock: { ...CLOCK, sync_status, holdover_started: sync_status === "holdover" ? CLOCK.actual_time : "" } };
    respond(answer);
    const field = await getFieldState();
    expect(Object.keys(field.devices)).toEqual(["breaker-1", "rmu-1", "rmu-2", "oltc-1"]);
    expect(field.clock).toEqual(answer.clock);
  });

  it.each(Object.keys(CLOCK))("rejects a clock missing %s", async (key) => {
    const clock: Record<string, unknown> = { ...CLOCK };
    delete clock[key];
    respond({ ...TRUTH, clock });
    await expect(getFieldState()).rejects.toThrow("Expected station clock state or null");
  });

  it.each([
    undefined, [], {}, "gps",
    { ...CLOCK, sync_status: "unknown" },
    { ...CLOCK, actual_time: "not a time" },
    { ...CLOCK, reported_time: null },
    { ...CLOCK, holdover_started: "bad" },
    { ...CLOCK, time_offset_sec: "2.5" },
    { ...CLOCK, satellite_count: -1 },
    { ...CLOCK, satellite_count: 1.5 },
    { ...CLOCK, holdover_drift_ppm: null },
    { ...CLOCK, ntp_enabled: 1 },
    { ...CLOCK, alarm: "false" },
  ])("rejects a malformed clock rather than dropping it (%j)", async (clock) => {
    respond({ ...TRUTH, clock });
    await expect(getFieldState()).rejects.toThrow("Expected station clock state or null");
  });

  it.each([
    null, [], {}, { ...BREAKER, schema: 1 }, { ...BREAKER, kind: "gps" },
    { ...BREAKER, position: "unknown" }, { ...BREAKER, measurements: null },
    { ...BREAKER, measurements: { ...BREAKER.measurements, current_a: "0" } },
    { ...BREAKER, lockout: undefined }, CLOCK,
  ])("rejects malformed and unknown devices, including GPS in devices (%j)", async (device) => {
    respond({ ...TRUTH, devices: { ...TRUTH.devices, unexpected: device } });
    await expect(getFieldState()).rejects.toThrow("Expected field and process schema 2");
  });

  it.each([null, [], {}, { ...TRUTH, devices: [] }, { ...TRUTH, devices: {} }, { ...TRUTH, process: null }])(
    "rejects malformed aggregate containers (%j)", async (answer) => {
      respond(answer);
      await expect(getFieldState()).rejects.toThrow("Expected field and process schema 2");
    },
  );

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

describe("rev 2 strict operator shape", () => {
  it("accepts all six worked examples, preserving received type, values and flags", async () => {
    const { getOperatorState } = await import("./operator-api");
    const { WORKED_STATES } = await import("./operator-fixtures.test-util");
    for (const state of WORKED_STATES) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(state))));
      await expect(getOperatorState()).resolves.toEqual(state);
    }
  });
  it("rejects unknown kinds, mismatched values / ASDUs, bad reporting and old GI", async () => {
    const { getOperatorState } = await import("./operator-api");
    const { FEEDER_STATE } = await import("./operator-fixtures.test-util");
    const point = FEEDER_STATE.points[0], float = FEEDER_STATE.points.find((p) => p.kind === "float")!;
    const answers = [
      null, {}, { ...FEEDER_STATE, link: null }, { ...FEEDER_STATE, rendered_at: "yesterday" },
      ...[{ kind: "status" }, { value: true }, { reporting: {} }, { quality: { ...point.quality, ov: false } }, { asdu: "M_SP_TB_1" }, { cot: "3" }, { stale_reason: ["not_updated"] }, { received_at: "not a time" }, { old_key: true }].map((patch) => ({ ...FEEDER_STATE, points: [{ ...point, ...patch }] })),
      ...[{ reporting: null }, { reporting: { cyclic_ms: 0, deadband: 0.1 } }, { reporting: { cyclic_ms: 10000, deadband: null } }, { reporting: { cyclic_ms: 10000 } }, { reporting: { cyclic_ms: 10000, deadband: -1 } }, { reporting: { cyclic_ms: 10000, deadband: 0, extra: true } }, { value: { value: 1, transient: false } }].map((patch) => ({ ...FEEDER_STATE, points: [{ ...float, ...patch }] })),
      ...[{ state: "complete", started_at: null, completed_at: null }, { ...FEEDER_STATE.link.gi, trigger: "unknown" }, { ...FEEDER_STATE.link.gi, state: ["running"] }, { ...FEEDER_STATE.link.gi, next_due_at: undefined }, { ...FEEDER_STATE.link.gi, last_complete_at: 0 }].map((gi) => ({ ...FEEDER_STATE, link: { ...FEEDER_STATE.link, gi } })),
    ];
    for (const answer of answers) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
      await expect(getOperatorState()).rejects.toThrow("Expected CC state schema 2");
    }
  });
  it("queues GI with a single bodyless POST and preserves refusal causes", async () => {
    const { postOperatorGi } = await import("./operator-api");
    const answer = { trigger: "operator", requested_at: "2026-10-09T12:00:00Z" };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(answer), { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(postOperatorGi()).resolves.toEqual(answer);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1].method).toBe("POST");
    expect(fetchMock.mock.calls[0][1].body).toBeUndefined();
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/operator\/gi$/);
    for (const [status, error] of [[409, "interrogation already running"], [503, "link not started"]] as const) {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error }), { status })));
      await expect(postOperatorGi()).rejects.toMatchObject({ status, detail: error });
    }
  });
});
