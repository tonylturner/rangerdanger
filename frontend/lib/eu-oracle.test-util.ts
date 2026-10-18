import type { OperatorPoint } from "./operator-api";
import { FEEDER_STATE, TRUTH } from "./operator-fixtures.test-util";

// Transcribed from build/specs/eu-state-smoke/oracle.md, not from UI reachability.
// D=dead, B=CB1, F=F2, BF=both. F2 now has independent IEC indications.
export const EU_ORACLE = [
  { switches: "OOO", f2: "normal", supply: "D/D/F", vt: 0, currents: [0, 0, 0, 11.2] },
  { switches: "OOO", f2: "tripped", supply: "D/D/D", vt: 0, currents: [0, 0, 0, 0] },
  { switches: "OOC", f2: "normal", supply: "D/F/F", vt: 0, currents: [0, 0, 18.5, 29.8] },
  { switches: "OOC", f2: "tripped", supply: "D/D/D", vt: 0, currents: [0, 0, 0, 0] },
  { switches: "OCO", f2: "normal", supply: "D/D/F", vt: 0, currents: [0, 0, 0, 11.2] },
  { switches: "OCO", f2: "tripped", supply: "D/D/D", vt: 0, currents: [0, 0, 0, 0] },
  { switches: "OCC", f2: "normal", supply: "F/F/F", vt: 19.33, currents: [0, 43.9, 62.4, 73.5] },
  { switches: "OCC", f2: "tripped", supply: "D/D/D", vt: 0, currents: [0, 0, 0, 0] },
  { switches: "COO", f2: "normal", supply: "B/D/F", vt: 19.94, currents: [42.8, 0, 0, 53.6] },
  { switches: "COO", f2: "tripped", supply: "B/D/D", vt: 19.97, currents: [42.7, 0, 0, 42.7] },
  { switches: "COC", f2: "normal", supply: "B/F/F", vt: 19.91, currents: [42.9, 0, 18.6, 72.1] },
  { switches: "COC", f2: "tripped", supply: "B/D/D", vt: 19.97, currents: [42.7, 0, 0, 42.7] },
  { switches: "CCO", f2: "normal", supply: "B/B/F", vt: 19.91, currents: [61.4, 18.7, 0, 72.3] },
  { switches: "CCO", f2: "tripped", supply: "B/B/D", vt: 19.93, currents: [61.3, 18.7, 0, 61.3] },
  { switches: "CCC", f2: "normal", supply: "BF/BF/BF", vt: 19.91, currents: [40.4, 6, 21, 72.1] },
  { switches: "CCC", f2: "tripped", supply: "B/B/B", vt: 19.91, currents: [72.7, 30.3, 11.5, 72.7] },
  // No trip indication yet: fault current is not a substitute for received protection.
  // The oracle marks the old CC B!/B! as a residual-voltage error; the corrected
  // expectation is no supply on S1/S2, consistent with its D/D/F electrical truth.
  { switches: "CCO", f2: "normal", supply: "D/D/F", vt: 11.53, currents: [2701, 0, 0, 2707], fault: true },
] as const;
export type OracleRow = (typeof EU_ORACLE)[number];
export const ORACLE_SECTIONS = ["S1", "S2", "S3"] as const;
export const PHYSICAL_PATH = { D: "no supply", B: "via CB1", F: "via F2", BF: "via CB1 + F2" } as const;

export function oracleFixture(row: OracleRow) {
  const fault = "fault" in row && row.fault;
  const supply = row.supply.split("/");
  // Only the fault voltages have exact per-section values in the oracle.
  // Fault-free live values are representative 19–20 kV values, not probe precision.
  const kv = fault ? [0.000004679, 0.000004653, 11.5] : supply.map((s) => s === "D" ? 0 : 19.9);
  const live = supply.map((s) => s !== "D");
  const positions = row.switches.split("").map((s) => s === "C" ? "closed" : "open");
  const [cb1, rmu1, rmu2, t1] = row.currents;
  const field = structuredClone(TRUTH);
  field.process.fault_section = fault ? "S1" : "none";
  field.process.f2_tripped = row.f2 === "tripped";
  field.process.profile.enabled = false;
  field.process.tap = 0;
  field.process.buses = {
    bus20: { kv: 20, live: true },
    s1: { kv: kv[0], live: live[0] }, s2: { kv: kv[1], live: live[1] }, s3: { kv: kv[2], live: live[2] },
  };
  const f2 = field.devices["f2-breaker"];
  if (f2.kind === "breaker") {
    f2.position = row.f2 === "tripped" ? "open" : "closed";
    f2.protection_tripped = row.f2 === "tripped";
  }
  Object.assign(f2.measurements, { current_a: null, p_mw: null, q_mvar: null, pf: null });
  const deviceIds = ["breaker-1", "rmu-1", "rmu-2", "oltc-1"];
  for (const [index, id] of deviceIds.entries()) {
    const d = field.devices[id];
    if (d.kind === "breaker") { d.position = positions[0]; d.lockout = false; d.protection_tripped = false; }
    if (d.kind === "rmu") { d.position = positions[index]; d.fault_passage = false; }
    if (d.kind === "oltc") { d.tap = 0; d.target_tap = 0; d.moving = false; d.auto = true; d.armed = true; }
    // P/Q are outside this oracle; do not retain unrelated fault-era loads.
    Object.assign(d.measurements, { p_mw: null, q_mvar: null, pf: null, current_a: row.currents[index] });
    Object.assign(field.process.devices[d.device_id], { current_a: row.currents[index], p_mw: null, q_mvar: null });
  }
  Object.assign(field.devices["breaker-1"].measurements, { voltage_kv_ll: row.vt, source_kv_ll: 20, section_live: row.vt >= 0.1, source_live: true });
  Object.assign(field.devices["rmu-1"].measurements, { source_kv_ll: kv[0], voltage_kv_ll: kv[1], source_live: live[0], section_live: live[1] });
  Object.assign(field.devices["rmu-2"].measurements, { source_kv_ll: kv[2], voltage_kv_ll: kv[1], source_live: live[2], section_live: live[1] });
  Object.assign(field.devices["oltc-1"].measurements, { voltage_kv_ll: 20, source_kv_ll: 110, section_live: true, source_live: true });

  // Explicit CC channels keep adapter mapping bugs visible rather than generating
  // CC input by reading the physical adapter under test.
  const values: Record<string, OperatorPoint["value"]> = {
    "f2-position": row.f2 === "tripped" ? "open" : "closed", "f2-protection-trip": row.f2 === "tripped",
    "f2-current": null, "f2-p-mw": null,
    "feeder-breaker-position": positions[0], "rmu-1-position": positions[1], "rmu-2-position": positions[2],
    "feeder-voltage": row.vt, "feeder-current": cb1, "rmu-1-current": rmu1, "rmu-2-current": rmu2, "oltc-1-current": t1,
    "rmu-1-source-kv": kv[0], "rmu-1-section-kv": kv[1], "rmu-2-source-kv": kv[2], "rmu-2-section-kv": kv[1],
    "oltc-1-bus-kv": 20, "oltc-1-tap": { value: 0, transient: false },
    "feeder-p-mw": null, "feeder-q-mvar": null, "feeder-pf": null,
    "oltc-1-p-mw": null, "oltc-1-q-mvar": null, "oltc-1-pf": null,
  };
  const operator = structuredClone(FEEDER_STATE);
  operator.points = operator.points.map((p) => p.id in values ? { ...p, value: values[p.id] } : p);
  return { field, operator, kv, live };
}
