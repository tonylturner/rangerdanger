import { afterEach, describe, expect, it, vi } from "vitest";
import { getFieldAudit, getFieldState, sendOperatorCommand } from "./operator-api";
import { AUDIT, BREAKER, CLOCK, TRUTH, commandResult } from "./operator-fixtures.test-util";

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
    expect(Object.keys(field.devices)).toEqual(["breaker-1", "rmu-1", "rmu-2", "oltc-1", "f2-breaker"]);
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
    null, [], {}, { ...BREAKER, schema: 2 }, { ...BREAKER, kind: "gps" },
    { ...BREAKER, position: "unknown" }, { ...BREAKER, measurements: null },
    { ...BREAKER, measurements: { ...BREAKER.measurements, current_a: "0" } },
    { ...BREAKER, lockout: undefined }, CLOCK,
  ])("rejects malformed and unknown devices, including GPS in devices (%j)", async (device) => {
    respond({ ...TRUTH, devices: { ...TRUTH.devices, unexpected: device } });
    await expect(getFieldState()).rejects.toThrow("Expected field and process schema 3");
  });

  it.each([null, [], {}, { ...TRUTH, devices: [] }, { ...TRUTH, devices: {} }, { ...TRUTH, process: null }])(
    "rejects malformed aggregate containers (%j)", async (answer) => {
      respond(answer);
      await expect(getFieldState()).rejects.toThrow("Expected field and process schema 3");
    },
  );

  it.each([
    ["/field/state", getFieldState, TRUTH],
    ["/field/audit?after=0&device=breaker-1", getFieldAudit, AUDIT],
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
  // cc_command.c finishes these four paths after execute ACT-CON.
  it.each([
    { result: "completed", reason: null, term_at: "2026-10-09T12:00:01.700Z", feedback: { ioa: 1001, value: "open", at: "2026-10-09T12:00:01.600Z" } },
    { result: "rejected", reason: "execute rejected (negative ACT-CON)", term_at: null, feedback: null },
    { result: "failed", reason: "negative ACT-TERM", term_at: "2026-10-09T12:00:01.700Z", feedback: null },
    { result: "unknown", reason: "no good matching COT 11 feedback for feeder-breaker-position before ACT-TERM", term_at: "2026-10-09T12:00:01.700Z", feedback: null },
  ] as const)("decodes the CC $result response without changing its reason or evidence", async (outcome) => {
    const answer = {
      id: "cmd-17", point: "feeder-breaker-command", operation: "open",
      selected_at: "2026-10-09T12:00:01.000Z",
      executed_at: "2026-10-09T12:00:01.100Z",
      con_at: "2026-10-09T12:00:01.200Z",
      ...outcome,
    };
    expect(commandResult(outcome.result)).toEqual(answer);
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(answer)));
    vi.stubGlobal("fetch", fetchMock);
    await expect(sendOperatorCommand({ point: "feeder-breaker-command", operation: "open" })).resolves.toEqual(answer);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([undefined, 0, false, [], {}])("rejects a non-string, non-null reason (%j)", async (reason) => {
    const answer = { ...commandResult("completed"), reason };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
    await expect(sendOperatorCommand({ point: "feeder-breaker-command", operation: "open" })).rejects.toThrow("command outcome is unknown");
  });

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

  it("hard-cuts CC and field reads to schema 3", async () => {
    const { getOperatorState } = await import("./operator-api");
    const { FEEDER_STATE } = await import("./operator-fixtures.test-util");
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(FEEDER_STATE)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...FEEDER_STATE, schema: 2 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...TRUTH, process: { ...TRUTH.process, schema: 2 } })));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getOperatorState()).resolves.toEqual(FEEDER_STATE);
    await expect(getOperatorState()).rejects.toThrow("Expected CC state schema 3");
    await expect(getFieldState()).rejects.toThrow("Expected field and process schema 3");
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
      await expect(getOperatorState()).rejects.toThrow("Expected CC state schema 3");
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

describe("step-position QDS regression", () => {
  it("accepts the GI regression payload extended with synthetic F2 contract points without rewriting the tap's OV", async () => {
    const { default: live } = await import("./fixtures/operator-state-live.json");
    const { getOperatorState, validOperatorState } = await import("./operator-api");
    expect(live.points).toHaveLength(42);
    expect(live.points.find((p) => p.id === "oltc-1-tap")).toMatchObject({
      kind: "step-position", asdu: "M_ST_NA_1", quality: { ov: false }, value: { value: 0, transient: false },
    });
    expect(validOperatorState(live)).toBe(true);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(live))));
    await expect(getOperatorState()).resolves.toEqual(live);
  });
  it.each(["M_ST_NA_1", "M_ST_TB_1"])("requires boolean OV on %s, including true overflow", async (asdu) => {
    const { validOperatorState } = await import("./operator-api");
    const { FEEDER_STATE } = await import("./operator-fixtures.test-util");
    const tap = FEEDER_STATE.points.find((p) => p.kind === "step-position")!;
    for (const ov of [false, true, null, undefined, 0, "false"]) {
      expect(validOperatorState({ ...FEEDER_STATE, points: [{ ...tap, asdu, quality: { ...tap.quality, ov } }] })).toBe(typeof ov === "boolean");
    }
    for (const p of FEEDER_STATE.points.filter((p) => p.kind === "single-point" || p.kind === "double-point")) {
      expect(validOperatorState({ ...FEEDER_STATE, points: [{ ...p, quality: { ...p.quality, ov: false } }] })).toBe(false);
    }
  });
});

describe("schema 3 realism cutover", () => {
  function respond(answer: unknown) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  }
  it.each(["breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1"] as const)("requests the selected %s audit", async (device) => {
    respond({ ...AUDIT, entries: [] });
    await getFieldAudit(device);
    expect(String(vi.mocked(fetch).mock.calls[0][0])).toMatch(new RegExp(`device=${device}$`));
  });
  it("rejects old audit snapshots", async () => {
    respond({ ...AUDIT, entries: AUDIT.entries.map((e) => ({ ...e, before: { ...e.before, schema: 2 } })) });
    await expect(getFieldAudit()).rejects.toThrow("schema 3 snapshots");
  });
  it.each([
    { ...BREAKER, schema: 2 }, { ...BREAKER, protection: null },
    { ...BREAKER, protection: { ...BREAKER.protection, picked_up_since: "not a timestamp" } },
    { ...BREAKER, protection: { ...BREAKER.protection, function: "fpi" } },
    { ...BREAKER, measurements: { ...BREAKER.measurements, p_mw: undefined } },
    { ...BREAKER, measurements: { ...BREAKER.measurements, pf: "0.95" } },
  ])("rejects incomplete new device protection/metering (%j)", async (device) => {
    respond({ ...TRUTH, devices: { "breaker-1": device } });
    await expect(getFieldState()).rejects.toThrow("schema 3");
  });
  it.each([
    { ...TRUTH.process, schema: 2 }, { ...TRUTH.process, profile: null },
    { ...TRUTH.process, profile: { ...TRUTH.process.profile, source_pu: "1.01" } },
    { ...TRUTH.process, f2_protection: {} }, { ...TRUTH.process, fault_section: ["none"] },
    { ...TRUTH.process, devices: { "feeder-breaker": { delivered: true, revision: 1, solved_revision: 1 } } },
  ])("rejects incomplete new process state (%j)", async (process) => {
    respond({ ...TRUTH, process });
    await expect(getFieldState()).rejects.toThrow("schema 3");
  });
  it("accepts unsolved numeric nulls and signed power without zero substitution", async () => {
    const process = structuredClone(TRUTH.process);
    process.solved_at = null;
    process.solve_seq = 0;
    process.profile = { enabled: true, load_factors: { residential: null, commercial: null, industrial: null }, source_pu: null };
    for (const d of Object.values(process.devices)) Object.assign(d, { delivered: false, revision: null, solved_revision: null, current_a: null, p_mw: null, q_mvar: null });
    const answer = { ...TRUTH, process, devices: { "breaker-1": { ...BREAKER, measurements: { ...BREAKER.measurements, p_mw: -2.1, q_mvar: -0.3, pf: 0.99 } } } };
    respond(answer);
    await expect(getFieldState()).resolves.toEqual(answer);
  });
  it("requires both new CC keys and finite ordered limits", async () => {
    const { validOperatorState } = await import("./operator-api");
    const { FEEDER_STATE } = await import("./operator-fixtures.test-util");
    const p = FEEDER_STATE.points[0];
    for (const patch of [{ device: undefined }, { limits: undefined }, { device: 1 }, { limits: { low: 21, high: 19 } }, { limits: { low: 19, high: 21, extra: true } }]) {
      expect(validOperatorState({ ...FEEDER_STATE, points: [{ ...p, ...patch }] })).toBe(false);
    }
    expect(validOperatorState({ ...FEEDER_STATE, schema: 2 })).toBe(false);
  });
});
