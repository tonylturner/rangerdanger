import { expect, it } from "vitest";
import { EU_ORACLE, PHYSICAL_PATH, ORACLE_SECTIONS, oracleFixture } from "./eu-oracle.test-util";
import { controlCentreObservations, physicalSystemObservations } from "./observation-readings";
import { SECTION_VOLTAGE, sectionSupplyPath } from "./operator-supply-path";
import { voltageState } from "./electrical-state";
import { abnormalConditions, activeAlarms } from "./operator-workstation";
import { physicalAbnormalities } from "./physical-abnormalities";

it.each(EU_ORACLE)("$switches / F2 $f2 / $supply / VT $vt: both adapters match the electrical oracle", (row) => {
  const { field, operator, kv, live } = oracleFixture(row);
  const cc = controlCentreObservations(operator.points), physical = physicalSystemObservations(field);
  const supply = row.supply.split("/") as (keyof typeof PHYSICAL_PATH)[];
  for (const [index, section] of ORACLE_SECTIONS.entries()) {
    expect(sectionSupplyPath(physical, section).state).toBe(PHYSICAL_PATH[supply[index]]);
    expect(sectionSupplyPath(cc, section)).toMatchObject({
      state: PHYSICAL_PATH[supply[index]],
    });
    for (const read of [cc, physical]) {
      expect(read(SECTION_VOLTAGE[section]).value).toBe(kv[index]);
      expect(voltageState(read(SECTION_VOLTAGE[section]))).toBe(live[index] ? "LIVE" : "DEAD");
      expect(read("rmu-2-section-kv").value).toBe(kv[1]);
      expect(read("feeder-voltage").value).toBe(row.vt);
      for (const [i, point] of ["feeder-current", "rmu-1-current", "rmu-2-current", "oltc-1-current"].entries()) {
        expect(read(point).value).toBe(row.currents[i]);
      }
    }
  }
  const tripped = row.f2 === "tripped";
  const expectedConditions: string[] = tripped ? ["F2 protection trip: asserted"] : [];
  const expectedPhysical: string[] = [];
  if (row.switches[0] === "O") { expectedConditions.push("CB1: OPEN (normal CLOSED)"); expectedPhysical.push("breaker-1-open"); }
  if (row.switches[1] === "O") { expectedConditions.push("RMU1 · NC: OPEN (normal CLOSED)"); expectedPhysical.push("rmu-1-switch"); }
  if (row.switches[2] === "C") { expectedConditions.push("RMU2 · NOP: CLOSED (normal OPEN)"); expectedPhysical.push("rmu-2-switch"); }
  for (const [i, label] of ["S1 residential", "S2 commercial", "S3 industrial"].entries()) {
    if (supply[i] === "D") expectedConditions.push(`${label}: no supply`);
  }
  const conditions = abnormalConditions(operator);
  expect(conditions.filter((c) => !c.id.endsWith("-voltage-limit")).map((c) => c.text)).toEqual(expectedConditions);
  if ("fault" in row) {
    expect(activeAlarms(operator).map((c) => c.id).sort()).toEqual(["feeder-voltage-voltage-limit", "rmu-2-source-kv-voltage-limit"]);
    expectedPhysical.push("fault");
  } else {
    expect(activeAlarms(operator).map((c) => c.id)).toEqual(tripped ? ["f2-protection-trip"] : []);
  }
  if (row.f2 === "tripped") expectedPhysical.push("f2-breaker-trip");
  expect(physicalAbnormalities(field).map((c) => c.id).sort()).toEqual(expectedPhysical.sort());
  expect(conditions.some((c) => c.text.includes("F2"))).toBe(tripped);
});
