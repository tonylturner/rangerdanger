import type { OperatorPoint } from "./operator-api";
import { pointCondition, pointLabel } from "./operator-format";

export type SectionId = "S1" | "S2" | "S3";
export type SupplyPath = {
  state: "via CB1" | "path unknown" | "no supply" | "unknown";
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

// Voltage presence is independent of path certainty. F2 is deliberately
// unobserved: neither field truth nor current direction can prove its state.
export function sectionSupplyPath(points: OperatorPoint[], section: SectionId): SupplyPath {
  const goodValue = (id: string) => {
    const point = points.find((p) => p.id === id);
    return point && pointCondition(point) === "good" ? point.value : null;
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
