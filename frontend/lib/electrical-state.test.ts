import { expect, it } from "vitest";
import { voltageState } from "./electrical-state";
import { controlCentreObservations } from "./observation-readings";
import { FEEDER_STATE } from "./operator-fixtures.test-util";
import { sectionSupplyPath } from "./operator-supply-path";

it.each([
  [0, "DEAD"], [0.000004679, "DEAD"], [0.099, "DEAD"], [0.1, "LIVE"], [20, "LIVE"],
  [-1, "UNKNOWN"], [NaN, "UNKNOWN"], [Infinity, "UNKNOWN"], [null, "UNKNOWN"],
] as const)("classifies GOOD %s kV as %s without mistaking residual voltage for supply", (value, state) => {
  const read = controlCentreObservations(FEEDER_STATE.points.map((p) => p.id === "rmu-1-source-kv" ? { ...p, value } : p));
  expect(voltageState(read("rmu-1-source-kv"))).toBe(state);
  expect(sectionSupplyPath(read, "S1").state).toBe(state === "DEAD" ? "no supply" : state === "LIVE" ? "via CB1" : "unknown");
});

it.each([0, 0.000004679, 0.099, 0.1])("uses the same live boundary for the busbar at %s kV", (value) => {
  const read = controlCentreObservations(FEEDER_STATE.points.map((p) => p.id === "oltc-1-bus-kv" ? { ...p, value } : p));
  expect(sectionSupplyPath(read, "S1").state).toBe(value >= 0.1 ? "via CB1" : "path unknown");
});

it.each(["stale", "unknown"] as const)("never classifies %s values as live or dead", (condition) => {
  for (const value of [0, 0.000004679, 20]) expect(voltageState({ condition, value })).toBe("UNKNOWN");
});
