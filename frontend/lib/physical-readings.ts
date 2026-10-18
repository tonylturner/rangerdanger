import type { DeviceState, FieldState, OperatorPoint, PointValue } from "./operator-api";
import { formatPointValue, pointCondition, qualityFlags } from "./operator-format";

export type Reading = {
  value: PointValue | undefined;
  condition: "good" | "stale" | "unknown";
  text: string;
  shortText: string;
  flags: string[];
  staleReason: string | null | undefined;
  transient: boolean;
};
export type Readings = (id: string) => Reading;

export function controlCentreObservations(points: OperatorPoint[], unavailable = false): Readings {
  return (id) => {
    const p = points.find((point) => point.id === id);
    const condition = !p || p.value === null ? "unknown" : pointCondition(p) === "good" ? "good" : "stale";
    const flags = p ? qualityFlags(p.quality) : [];
    const transient = typeof p?.value === "object" && p.value !== null ? p.value.transient : false;
    const shortText = `${p ? formatPointValue(id, p.value, p.quality) : "unknown"} · ${condition.toUpperCase()}${flags.length ? ` · ${flags.join(" ")}` : ""}`;
    const text = `${shortText}${p?.stale_reason ? ` · ${p.stale_reason}` : ""}${unavailable ? " · cached snapshot · CC API unavailable" : ""}`;
    return { value: p?.value, condition, text, shortText, transient, flags, staleReason: p?.stale_reason };
  };
}

type Mapping = { device: string; value: (d: DeviceState) => PointValue; measurement?: boolean };
export const PHYSICAL_POINTS: Record<string, Mapping> = {
  "feeder-breaker-position": { device: "breaker-1", value: (d) => d.kind === "breaker" ? d.position : null },
  "feeder-protection-trip": { device: "breaker-1", value: (d) => d.kind === "breaker" ? d.protection_tripped : null },
  "oltc-1-tap": { device: "oltc-1", value: (d) => d.kind === "oltc" ? { value: d.tap, transient: d.moving } : null },
  "oltc-1-remote": { device: "oltc-1", value: (d) => d.remote_enabled },
  "oltc-1-auto": { device: "oltc-1", value: (d) => d.kind === "oltc" ? d.auto : null },
  "oltc-1-blocked": { device: "oltc-1", value: (d) => d.kind === "oltc" ? d.blocked || d.protection.picked_up : null },
  "oltc-1-overcurrent-block": { device: "oltc-1", value: (d) => d.protection.picked_up },
};
for (const [prefix, device] of [["feeder", "breaker-1"], ["oltc-1", "oltc-1"], ["rmu-1", "rmu-1"], ["rmu-2", "rmu-2"]]) {
  const measurements: Record<string, "current_a" | "voltage_kv_ll" | "source_kv_ll" | "p_mw" | "q_mvar" | "pf"> = {
    current: "current_a",
    ...(prefix === "feeder" ? { voltage: "voltage_kv_ll" } : prefix === "oltc-1" ? { "bus-kv": "voltage_kv_ll" } : { "section-kv": "voltage_kv_ll", "source-kv": "source_kv_ll" }),
    ...(!prefix.startsWith("rmu") ? { "p-mw": "p_mw", "q-mvar": "q_mvar", pf: "pf" } : {}),
  } as const;
  for (const [suffix, key] of Object.entries(measurements)) {
    PHYSICAL_POINTS[`${prefix}-${suffix}`] = { device, measurement: true, value: (d) => d.measurements[key] };
  }
  if (prefix.startsWith("rmu")) {
    PHYSICAL_POINTS[`${prefix}-position`] = { device, value: (d) => d.kind === "rmu" ? d.position : null };
    PHYSICAL_POINTS[`${prefix}-remote`] = { device, value: (d) => d.remote_enabled };
    PHYSICAL_POINTS[`${prefix}-earth`] = { device, value: (d) => d.kind === "rmu" ? d.earth_closed : null };
    PHYSICAL_POINTS[`${prefix}-fault-passage`] = { device, value: (d) => d.kind === "rmu" ? d.fault_passage : null };
  }
}

export function physicalSystemObservations(field: FieldState, unavailable = false): Readings {
  return (id) => {
    const mapping = PHYSICAL_POINTS[id];
    const device = mapping && field.devices[mapping.device];
    const isF2 = id === "f2-position";
    const busId = id.endsWith("-live") ? id.slice(0, -5) : null;
    const bus = busId ? field.process.buses[busId] : undefined;
    const processValue = isF2 ? field.process.truth_stale ? null : field.process.f2_tripped ? "open" : "closed" : bus?.live ?? null;
    const value = device ? mapping.value(device) : processValue;
    const invalid = !!(mapping?.measurement && device && !device.measurements.valid);
    const stale = unavailable || ((mapping?.measurement || isF2 || busId !== null) && field.process.truth_stale);
    const condition = value === null || value === undefined ? "unknown" : invalid || stale ? "stale" : "good";
    const staleReason = unavailable ? "field API unavailable · retained snapshot" : invalid ? "measurements invalid · last values, not current" : stale ? "device input stale" : null;
    const transient = typeof value === "object" && value !== null ? value.transient : false;
    const shortText = `${value === null || value === undefined ? "unknown" : formatPointValue(id, value)} · ${invalid ? "INVALID" : condition.toUpperCase()} · sim`;
    return { value, condition, shortText, text: `${shortText}${staleReason ? ` · ${staleReason}` : ""}`, flags: [], staleReason, transient };
  };
}
