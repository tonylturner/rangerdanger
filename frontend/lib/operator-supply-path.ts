import type { Readings } from "./observation-readings";
import { pointLabel } from "./operator-format";
import { LIVE_KV } from "./electrical-state";

export type SectionId = "S1" | "S2" | "S3";
export type SupplyPath = {
  state: "via CB1" | "via F2" | "via CB1 + F2" | "path unknown" | "no supply" | "unknown";
  reason: string;
};

export const SECTION_VOLTAGE: Record<SectionId, string> = {
  S1: "rmu-1-source-kv",
  S2: "rmu-1-section-kv",
  S3: "rmu-2-source-kv",
};
const SWITCHES = ["feeder-breaker-position", "rmu-1-position", "rmu-2-position"];
const SECTION_INDEX: Record<SectionId, number> = { S1: 0, S2: 1, S3: 2 };

// Voltage presence and switch reachability are separate evidence.
// Each caller supplies its own observations, never the other source's values.
export function sectionSupplyPath(read: Readings, section: SectionId): SupplyPath {
  const goodValue = (id: string) => {
    const r = read(id);
    return r.condition === "good" ? r.value : null;
  };
  const result = (state: SupplyPath["state"], reason: string): SupplyPath => ({ state, reason });
  const voltageId = SECTION_VOLTAGE[section];
  const kv = goodValue(voltageId);
  if (typeof kv !== "number" || !Number.isFinite(kv) || kv < 0) {
    return result("unknown", `${pointLabel(voltageId)} voltage is unavailable or invalid`);
  }
  if (kv < LIVE_KV) return result("no supply", `${pointLabel(voltageId)} voltage is GOOD and below ${LIVE_KV} kV (${kv} kV measured)`);

  const bus = goodValue("oltc-1-bus-kv");
  const index = SECTION_INDEX[section];
  const path = SWITCHES.slice(0, index + 1);
  const unproven = path.filter((id) => goodValue(id) !== "closed");
  const busLive = typeof bus === "number" && Number.isFinite(bus) && bus >= LIVE_KV;
  const f2 = goodValue("f2-position");
  if (f2 === "closed" || f2 === "open") {
    const cb1Path = busLive && unproven.length === 0;
    const f2Switches = SWITCHES.slice(index + 1);
    if (!busLive) return result("path unknown", "20 kV busbar voltage is not confirmed GOOD and live");
    const f2Path = f2 === "closed" && f2Switches.every((id) => goodValue(id) === "closed");
    if (cb1Path && f2Path) return result("via CB1 + F2", "Both observed switch paths connect the T1 / bus20 source to this live section");
    if (cb1Path) return result("via CB1", "Observed CB1 path connects to this live section");
    if (f2Path) return result("via F2", "Observed F2 path connects the same T1 / bus20 source to this live section");
    return result("unknown", "Live measured kV contradicts or cannot establish a supply path from the switch states");
  }
  return result("path unknown", "F2 position is missing, stale or not established; supply path is uncertain");
}
