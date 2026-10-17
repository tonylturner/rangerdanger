import { describe, expect, it } from "vitest";
import { activeAlarms, devicePoints, regulationBand, DEVICES } from "./operator-workstation";
import { BOOT_STATE, FEEDER_STATE, PERIODIC_GI_STATE, WATCHDOG_STATE } from "./operator-fixtures.test-util";
import { pointCondition } from "./operator-format";

describe("operator observation rules", () => {
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
    expect(regulationBand(FEEDER_STATE.points)).toEqual({ low: 19.8, high: 20.2, setpoint: 20, bus: 20.02 });
    expect(regulationBand(WATCHDOG_STATE.points)?.bus).toBeNull();
    expect(regulationBand(BOOT_STATE.points)).toBeNull();
    expect(regulationBand(FEEDER_STATE.points.map((p) => p.id === "plc-1-deadband-pct" ? { ...p, quality: { ...p.quality, sb: true } } : p))).toBeNull();
  });
  it("groups the entire point inventory into four devices without a field input", () => {
    expect(DEVICES.flatMap((id) => devicePoints(FEEDER_STATE.points, id)).map((p) => p.id).sort()).toEqual(FEEDER_STATE.points.map((p) => p.id).sort());
    expect(PERIODIC_GI_STATE.link.gi.last_complete_at).not.toBeNull();
  });
});
