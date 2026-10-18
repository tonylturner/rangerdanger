import { describe, expect, it } from "vitest";
import { activeAlarms, abnormalConditions, deviceCommsHealth, devicePoints, receivedAuthority, regulationBand, scadaViewState, DEVICES } from "./operator-workstation";
import { BOOT_STATE, FEEDER_STATE, LOST_LINK_STATE, PERIODIC_GI_STATE, WATCHDOG_STATE } from "./operator-fixtures.test-util";
import { pointCondition } from "./operator-format";

describe("operator observation rules", () => {
  it.each([
    ["feeder-breaker-position", "open", "CB1: OPEN (normal CLOSED)"],
    ["rmu-1-position", "open", "RMU1 · NC: OPEN (normal CLOSED)"],
    ["rmu-2-position", "closed", "RMU2 · NOP: CLOSED (normal OPEN)"],
    ["rmu-1-position", "intermediate", "RMU1 · NC: INTERMEDIATE (normal CLOSED)"],
    ["rmu-2-position", "indeterminate", "RMU2 · NOP: INDETERMINATE (normal OPEN)"],
    ["rmu-1-section-kv", 0.000004653, "S2 commercial: no supply"],
  ] as const)("lists GOOD %s off-normal observations without adding alarms", (id, value, text) => {
    const state = structuredClone(FEEDER_STATE);
    state.points.find((p) => p.id === id)!.value = value;
    expect(abnormalConditions(state).map((c) => c.text)).toEqual([text]);
    expect(activeAlarms(state)).toEqual([]);
    for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
      const bad = { ...state, points: state.points.map((p) => p.id === id ? { ...p, quality: { ...p.quality, [flag]: true } } : p) };
      expect(abnormalConditions(bad)).toEqual([]);
    }
    for (const patch of [{ value: null }, { stale_reason: "not_updated" as const }]) {
      const bad = { ...state, points: state.points.map((p) => p.id === id ? { ...p, ...patch } : p) };
      expect(abnormalConditions(bad).map((c) => c.text)).not.toContain(text);
    }
    expect(abnormalConditions({ ...state, points: state.points.filter((p) => p.id !== id) })).toEqual([]);
  });
  it.each([
    ["rmu-1-remote", "RMU1 authority: LOCAL"],
    ["rmu-2-remote", "RMU2 authority: LOCAL"],
    ["oltc-1-remote", "T1 OLTC authority: LOCAL"],
  ])("treats GOOD LOCAL %s as an off-normal state, not an alarm", (id, text) => {
    const state = structuredClone(FEEDER_STATE);
    const point = state.points.find((p) => p.id === id)!;
    point.value = false;
    expect(abnormalConditions(state)).toContainEqual({ id, text });
    expect(activeAlarms(state)).toEqual([]);
    point.value = true;
    expect(abnormalConditions(state)).toEqual([]);
    for (const value of [false, true]) {
      point.value = value;
      for (const flag of ["iv", "nt", "sb", "bl"] as const) {
        const bad = { ...state, points: state.points.map((p) => p.id === id ? { ...p, quality: { ...p.quality, [flag]: true } } : p) };
        expect(abnormalConditions(bad).some((c) => c.id === id)).toBe(false);
      }
      point.stale_reason = "not_updated";
      expect(abnormalConditions(state).some((c) => c.id === id)).toBe(false);
      point.stale_reason = null;
    }
    point.value = null;
    expect(abnormalConditions(state).some((c) => c.id === id)).toBe(false);
    expect(abnormalConditions({ ...state, points: state.points.filter((p) => p.id !== id) }).some((c) => c.id === id)).toBe(false);
  });

  it("derives process alarms only from full GOOD and separates bad quality", () => {
    const state = structuredClone(FEEDER_STATE);
    for (const id of ["feeder-protection-trip", "rmu-1-earth", "rmu-2-fault-passage", "oltc-1-blocked", "oltc-1-at-lower", "oltc-1-at-upper"]) {
      state.points.find((p) => p.id === id)!.value = true;
    }
    state.points.find((p) => p.id === "plc-1-running")!.value = false;
    state.points.find((p) => p.id === "plc-1-input-valid")!.value = false;
    expect(activeAlarms(state)).toHaveLength(8);
    for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
      const bad = { ...state, points: state.points.map((p) => ({ ...p, quality: { ...p.quality, [flag]: true } })) };
      expect(activeAlarms(bad)).toEqual([]);
      expect(bad.points.every((p) => pointCondition(p) !== "good")).toBe(true);
    }
    expect(activeAlarms(BOOT_STATE)).toEqual([{ id: "link", text: "IEC 104 link connecting" }]);
    expect(activeAlarms(FEEDER_STATE)).toEqual([]); // normal open tie is not an alarm
  });
  it("uses present telemetry for the percentage band and rejects bad inputs", () => {
    const band = regulationBand(FEEDER_STATE.points);
    expect(band).toMatchObject({ setpoint: 20.4, bus: 20.02 });
    expect(band!.low).toBeCloseTo(20.196, 6);
    expect(band!.high).toBeCloseTo(20.604, 6);
    expect(regulationBand(WATCHDOG_STATE.points)?.bus).toBeNull();
    expect(regulationBand(BOOT_STATE.points)).toBeNull();
    expect(regulationBand(FEEDER_STATE.points.map((p) => p.id === "plc-1-deadband-pct" ? { ...p, quality: { ...p.quality, sb: true } } : p))).toBeNull();
  });
  it("groups the entire point inventory into five devices without a field input", () => {
    expect(DEVICES.flatMap((id) => devicePoints(FEEDER_STATE.points, id)).map((p) => p.id).sort()).toEqual(FEEDER_STATE.points.map((p) => p.id).sort());
    expect(PERIODIC_GI_STATE.link.gi.last_complete_at).not.toBeNull();
  });
});

describe("view establishment", () => {
  it.each([
    [undefined, "not-established"], [BOOT_STATE, "not-established"],
    [LOST_LINK_STATE, "lost"], [FEEDER_STATE, "ok"],
    [PERIODIC_GI_STATE, "ok"], [WATCHDOG_STATE, "degraded"],
  ] as const)("uses link, current-connection GI and the full inventory: %s", (state, expected) => {
    expect(scadaViewState(state)).toBe(expected);
    expect(scadaViewState(state, true)).toBe("unavailable");
  });
  it("requires GI on this connection, not just an old completion or started link", () => {
    const state = structuredClone(FEEDER_STATE);
    state.link.since = "2026-10-09T12:00:30Z";
    expect(scadaViewState(state)).toBe("recovering");
    state.link.gi.last_complete_at = "2026-10-09T12:00:31Z";
    expect(scadaViewState(state)).toBe("ok");
    state.link.gi.last_complete_at = null;
    expect(scadaViewState(state)).toBe("recovering");
  });
  it("requires every inventory point GOOD, including the indication-only F2 bay", () => {
    for (const point of FEEDER_STATE.points) {
      for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
        const state = { ...FEEDER_STATE, points: FEEDER_STATE.points.map((p) => p.id === point.id ? { ...p, quality: { ...p.quality, [flag]: true } } : p) };
        expect(scadaViewState(state)).toBe("degraded");
      }
      expect(scadaViewState({ ...FEEDER_STATE, points: FEEDER_STATE.points.map((p) => p.id === point.id ? { ...p, value: null } : p) })).toBe("degraded");
    }
    expect(scadaViewState({ ...FEEDER_STATE, points: [] })).toBe("degraded");
  });
  it.each(DEVICES)("distinguishes unreachable %s observations after GI from recovery without GI", (device) => {
    const state = structuredClone(FEEDER_STATE);
    for (const p of devicePoints(state.points, device)) {
      p.quality = { ...p.quality, iv: true, nt: true };
      p.stale_reason = "device_unreachable";
    }
    expect(scadaViewState(state)).toBe("degraded");
    state.link.gi.last_complete_at = null;
    expect(scadaViewState(state)).toBe("recovering");
    state.link.gi.last_complete_at = "2026-10-09T11:59:57Z";
    expect(scadaViewState(state)).toBe("recovering");
  });
});

describe("F2 and command authority", () => {
  it("alarms for F2 protection only on GOOD true feedback", () => {
    const state = structuredClone(FEEDER_STATE);
    const trip = state.points.find((p) => p.id === "f2-protection-trip")!;
    trip.value = true;
    expect(activeAlarms(state)).toEqual([{ id: trip.id, text: "F2 protection trip: asserted" }]);
    for (const flag of ["iv", "nt", "sb", "bl"] as const) {
      expect(activeAlarms({ ...state, points: [{ ...trip, quality: { ...trip.quality, [flag]: true } }] })).toEqual([]);
    }
    trip.stale_reason = "device_unreachable";
    expect(activeAlarms(state)).toEqual([]);
  });
  it("never establishes authority from missing, stale, cached or non-boolean input", () => {
    expect(receivedAuthority(FEEDER_STATE.points, "CB1")).toBe("not established");
    expect(receivedAuthority(FEEDER_STATE.points, "F2")).toBe("not established");
    for (const device of ["T1", "RMU1", "RMU2"] as const) {
      const id = device === "T1" ? "oltc-1-remote" : device === "RMU1" ? "rmu-1-remote" : "rmu-2-remote";
      const p = FEEDER_STATE.points.find((point) => point.id === id)!;
      expect(receivedAuthority([p], device)).toBe("REMOTE");
      expect(receivedAuthority([{ ...p, value: false }], device)).toBe("LOCAL");
      for (const flag of ["iv", "nt", "sb", "bl"] as const) {
        expect(receivedAuthority([{ ...p, quality: { ...p.quality, [flag]: true } }], device)).toBe("not established");
      }
      for (const value of [null, "closed", 1] as const) expect(receivedAuthority([{ ...p, value }], device)).toBe("not established");
      expect(receivedAuthority([{ ...p, stale_reason: "not_updated" }], device)).toBe("not established");
      expect(receivedAuthority([p], device, true)).toBe("not established");
      expect(receivedAuthority([], device)).toBe("not established");
    }
  });
});

describe("voltage limits", () => {
  it.each([
    [0, false], [0.000004679, false], [0.099, false], [-1, false], [NaN, false], [Infinity, false], [0.1, true], [0.10001, true], [18.999, true], [19, false], [20, false], [21, false], [21.001, true],
  ])("alarms at %s kV: %s", (value, alarm) => {
    const point = FEEDER_STATE.points.find((p) => p.id === "feeder-voltage")!;
    expect(activeAlarms({ ...FEEDER_STATE, points: [{ ...point, value }] }).some((a) => a.id === "feeder-voltage-voltage-limit")).toBe(alarm);
  });
  it("clears for non-good quality or no limits, leaving uncertainty in Points", () => {
    const point = { ...FEEDER_STATE.points.find((p) => p.id === "feeder-voltage")!, value: 18 };
    for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
      expect(activeAlarms({ ...FEEDER_STATE, points: [{ ...point, quality: { ...point.quality, [flag]: true } }] })).toEqual([]);
    }
    expect(activeAlarms({ ...FEEDER_STATE, points: [{ ...point, stale_reason: "not_updated" }] })).toEqual([]);
    expect(activeAlarms({ ...FEEDER_STATE, points: [{ ...point, limits: null }] })).toEqual([]);
  });
});

describe("device comms precedence", () => {
  it("groups by CC source device, with link_down > lost > degraded > ok", () => {
    const points = FEEDER_STATE.points.filter((p) => p.device === "feeder-breaker");
    const state = { ...FEEDER_STATE, points };
    expect(deviceCommsHealth(state)).toEqual({ "feeder-breaker": "ok" });
    for (const stale_reason of ["device_unreachable", "never_received", "heartbeat", "not_updated"] as const) {
      const mixed = { ...state, points: points.map((p, i) => i === 0 ? { ...p, stale_reason } : p) };
      expect(deviceCommsHealth(mixed)["feeder-breaker"]).toBe("degraded");
      const all = { ...state, points: points.map((p, i) => ({ ...p, stale_reason: i % 2 ? "never_received" as const : "device_unreachable" as const })) };
      expect(deviceCommsHealth(all)["feeder-breaker"]).toBe("lost");
      expect(deviceCommsHealth({ ...all, link: { ...all.link, state: "connecting" } })["feeder-breaker"]).toBe("link_down");
    }
    expect(deviceCommsHealth({ ...state, points: points.map((p) => ({ ...p, stale_reason: "measurement_invalid", quality: { ...p.quality, iv: true } })) })).toEqual({ "feeder-breaker": "ok" });
  });
  it("summarises GI, trip, FPI, blocking, voltage and device comms independently", () => {
    const state = structuredClone(FEEDER_STATE);
    state.link.gi.last_complete_at = null;
    for (const id of ["feeder-protection-trip", "rmu-1-fault-passage", "oltc-1-blocked"]) state.points.find((p) => p.id === id)!.value = true;
    state.points.find((p) => p.id === "feeder-voltage")!.value = 18;
    state.points.find((p) => p.id === "rmu-2-current")!.stale_reason = "not_updated";
    expect(abnormalConditions(state).map((c) => c.id)).toEqual(expect.arrayContaining(["gi", "feeder-protection-trip", "rmu-1-fault-passage", "oltc-1-blocked", "feeder-voltage-voltage-limit", "comms-rmu-2"]));
    expect(abnormalConditions(FEEDER_STATE)).toEqual([]);
  });
});
