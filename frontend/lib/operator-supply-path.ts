import type { Readings } from "./physical-readings";
import { pointLabel } from "./operator-format";

export type SectionId = "S1" | "S2" | "S3";
export type SupplyPath = {
  state: "via CB1" | "via F2" | "via CB1 + F2" | "path unknown" | "no supply" | "unknown";
  reason: string;
  f2ContributionUnobserved: boolean;
};

export const SECTION_VOLTAGE: Record<SectionId, string> = {
  S1: "rmu-1-source-kv",
  S2: "rmu-1-section-kv",
  S3: "rmu-2-source-kv",
};
const SWITCHES = ["feeder-breaker-position", "rmu-1-position", "rmu-2-position"];
const SECTION_INDEX: Record<SectionId, number> = { S1: 0, S2: 1, S3: 2 };

// Voltage presence and switch reachability are separate evidence.
// CC callers leave F2 unknown; physical callers provide its solver state.
export function sectionSupplyPath(read: Readings, section: SectionId): SupplyPath {
  const goodValue = (id: string) => {
    const r = read(id);
    return r.condition === "good" ? r.value : null;
  };
  const result = (state: SupplyPath["state"], reason: string, f2ContributionUnobserved = false): SupplyPath =>
    ({ state, reason, f2ContributionUnobserved });
  const voltageId = SECTION_VOLTAGE[section];
  const kv = goodValue(voltageId);
  if (typeof kv !== "number" || !Number.isFinite(kv) || kv < 0) {
    return result("unknown", `${pointLabel(voltageId)} voltage is unavailable or invalid`);
  }
  if (kv === 0) return result("no supply", `${pointLabel(voltageId)} voltage is GOOD and 0 kV`);

  const bus = goodValue("oltc-1-bus-kv");
  const index = SECTION_INDEX[section];
  const path = SWITCHES.slice(0, index + 1);
  const unproven = path.filter((id) => goodValue(id) !== "closed");
  const busLive = typeof bus === "number" && Number.isFinite(bus) && bus > 0;
  const f2 = goodValue("f2-position");
  if (f2 === "closed" || f2 === "open") {
    const cb1Path = busLive && unproven.length === 0;
    const f2Switches = SWITCHES.slice(index + 1);
    const f2Path = f2 === "closed" && f2Switches.every((id) => goodValue(id) === "closed");
    if (cb1Path && f2Path) return result("via CB1 + F2", "Both physical switch paths connect to this live section");
    if (cb1Path) return result("via CB1", "Physical CB1 path connects to this live section");
    if (f2Path) return result("via F2", "Physical F2 path connects to this live section");
    return result("unknown", "Live measured kV contradicts or cannot establish a supply path from the switch states");
  }
  if (!busLive || unproven.length) {
    const gaps = [
      ...(!busLive ? ["20 kV busbar voltage is not confirmed GOOD and live"] : []),
      ...unproven.map((id) => `${pointLabel(id)} is not confirmed GOOD and closed`),
    ];
    return result("path unknown", `F2 unobserved; ${gaps.join("; ")}`);
  }
  // Reverse reachability from S3 excludes CB1: S3 is adjacent to F2,
  // S2 needs RMU2 closed, and S1 needs both RMUs closed.
  const f2Reachable = SWITCHES.slice(index + 1).every((id) => goodValue(id) === "closed");
  return result("via CB1", `20 kV busbar and ${section} voltages are GOOD and live; observed path switches are GOOD and closed${f2Reachable ? "; F2 contribution unobserved" : ""}`, f2Reachable);
}
