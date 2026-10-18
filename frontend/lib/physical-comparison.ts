import type { FieldState, OperatorPoint, OperatorState, PointValue } from "./operator-api";
import { pointCondition, pointLabel, qualityFlags } from "./operator-format";
import { PHYSICAL_POINTS, physicalSystemObservations, type Reading } from "./observation-readings";

export const PRIMARY_POINTS = ["feeder-breaker-position", "rmu-1-position", "rmu-2-position", "oltc-1-tap", "oltc-1-bus-kv", "f2-position"];
export const COMPARISON_STATUSES = ["MATCH", "MISMATCH", "STALE", "INVALID", "UNKNOWN"] as const;
export type ComparisonStatus = typeof COMPARISON_STATUSES[number];
export type ComparisonRow = {
  id: string;
  label: string;
  primary: boolean;
  physical: Reading;
  cc: OperatorPoint | undefined;
  flags: string[];
  status: ComparisonStatus;
  detail: string;
};
export type ComparisonCounts = Record<ComparisonStatus, number>;
const HOP: Record<string, string> = {
  link_lost: "CC↔RTU link lost",
  device_unreachable: "RTU cannot reach device",
  measurement_invalid: "device measurements invalid",
  heartbeat: "heartbeat stopped",
  not_updated: "not_updated · cyclic reporting watchdog expired",
};
const scalar = (v: PointValue | undefined) => typeof v === "object" && v !== null ? v.value : v;

function outcome(physical: Reading, p: OperatorPoint | undefined, field: FieldState, operator: OperatorState | undefined, id: string, unavailable: boolean): [ComparisonStatus, string] {
  if (!operator) return ["UNKNOWN", "CC snapshot absent"];
  if (!p || pointCondition(p) === "never" || p.stale_reason === "never_received") return ["UNKNOWN", "CC point not received"];
  const mapping = PHYSICAL_POINTS[id];
  const device = mapping ? field.devices[mapping.device] : undefined;
  if (device && Date.parse(device.changed_at) > Date.parse(operator.rendered_at)) return ["UNKNOWN", "awaiting CC snapshot"];
  const q = p.quality;
  if ((q.iv && (p.stale_reason === null || p.stale_reason === "measurement_invalid")) || q.sb || q.bl) return ["INVALID", ["unusable IEC quality", ...qualityFlags(q), p.stale_reason && HOP[p.stale_reason]].filter(Boolean).join(" · ")];
  if (physical.value === null || physical.value === undefined || physical.condition !== "good") return ["UNKNOWN", physical.staleReason ?? "physical value unknown"];
  if (p.kind === "float" && (p.reporting?.deadband === undefined || !Number.isFinite(p.reporting.deadband))) return ["UNKNOWN", "no configured reporting deadband"];
  if (qualityFlags(q).length || p.stale_reason || unavailable) return ["STALE", [p.stale_reason ? HOP[p.stale_reason] ?? p.stale_reason : null, ...qualityFlags(q), unavailable ? "cached snapshot · CC API unavailable" : null].filter(Boolean).join(" · ")];
  const actual = scalar(physical.value), observed = scalar(p.value);
  if (p.kind === "float" && typeof actual === "number" && typeof observed === "number") {
    const tolerance = p.reporting!.deadband;
    const equal = Math.abs(actual - observed) <= tolerance + Number.EPSILON * Math.max(1, Math.abs(actual), Math.abs(observed)) * 4;
    return [equal ? "MATCH" : "MISMATCH", `reporting deadband ±${tolerance}`];
  }
  return [actual === observed ? "MATCH" : "MISMATCH", "independent observations"];
}

export function physicalComparison(field: FieldState, operator: OperatorState | undefined, unavailable = false, fieldUnavailable = false): ComparisonRow[] {
  const read = physicalSystemObservations(field, fieldUnavailable);
  const ids = [...PRIMARY_POINTS, ...Object.keys(PHYSICAL_POINTS).filter((id) => !PRIMARY_POINTS.includes(id))];
  return ids.map((id) => {
    const physical = read(id), cc = operator?.points.find((p) => p.id === id);
    const [status, detail] = outcome(physical, cc, field, operator, id, unavailable);
    const motion = id === "oltc-1-tap" ? [physical.transient ? "field moving; completed tap shown" : null, typeof cc?.value === "object" && cc.value?.transient ? "CC TRANSIENT; last completed tap shown" : null].filter(Boolean) : [];
    return { id, label: pointLabel(id), primary: PRIMARY_POINTS.includes(id), physical, cc, flags: cc ? qualityFlags(cc.quality) : [], status, detail: [detail, ...motion].join(" · ") };
  });
}
export function comparisonCounts(rows: ComparisonRow[]): ComparisonCounts {
  const counts: ComparisonCounts = { MATCH: 0, MISMATCH: 0, STALE: 0, INVALID: 0, UNKNOWN: 0 };
  for (const row of rows) counts[row.status]++;
  return counts;
}
export function agreementText(counts: ComparisonCounts): string {
  const n = Object.values(counts).reduce((a, b) => a + b, 0);
  return `${counts.MATCH} matched / ${counts.MISMATCH} mismatched / ${counts.STALE} stale / ${counts.INVALID} invalid / ${counts.UNKNOWN} unknown of ${n}`;
}

// Quality alone says nothing about agreement with the independent physical source.
export function scadaQualityText(operator: OperatorState | undefined, unavailable = false): string {
  if (!operator) return "unknown · no CC snapshot";
  const usable = unavailable ? 0 : operator.points.filter((p) => pointCondition(p) === "good").length;
  return `${usable}/${operator.points.length} usable received points${unavailable ? " · cached CC snapshot" : ""}`;
}
