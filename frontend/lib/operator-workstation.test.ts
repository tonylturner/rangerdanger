import { describe, expect, it } from "vitest";
import { activeAlarms, abnormalConditions, deviceCommsHealth, devicePoints, regulationBand, DEVICES } from "./operator-workstation";
import { BOOT_STATE, FEEDER_STATE, PERIODIC_GI_STATE, WATCHDOG_STATE } from "./operator-fixtures.test-util";
import { pointCondition } from "./operator-format";

describe("operator observation rules", () => {
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
  it("groups the entire point inventory into four devices without a field input", () => {
    expect(DEVICES.flatMap((id) => devicePoints(FEEDER_STATE.points, id)).map((p) => p.id).sort()).toEqual(FEEDER_STATE.points.map((p) => p.id).sort());
    expect(PERIODIC_GI_STATE.link.gi.last_complete_at).not.toBeNull();
  });
});

describe("voltage limits", () => {
  it.each([
    [0, false], [0.1, false], [0.10001, true], [18.999, true], [19, false], [20, false], [21, false], [21.001, true],
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
