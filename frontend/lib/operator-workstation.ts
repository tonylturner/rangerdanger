import type { ControlCentreLink, OperatorPoint, OperatorState } from "./operator-api";
import { pointCondition, pointLabel } from "./operator-format";
import { LIVE_KV, voltageState } from "./electrical-state";
import { controlCentreObservations, type Readings } from "./observation-readings";

export const SWITCH_NORMALS = {
  CB1: { id: "feeder-breaker-position", label: "CB1", normal: "closed" },
  F2: { id: "f2-position", label: "F2", normal: "closed" },
  RMU1: { id: "rmu-1-position", label: "RMU1 · NC", normal: "closed" },
  RMU2: { id: "rmu-2-position", label: "RMU2 · NOP", normal: "open" },
} as const;
export const SUPPLY_VOLTAGES: Record<string, { id: string; label: string }> = {
  "rmu-1-source-kv": { id: "S1-no-supply", label: "S1 residential" },
  "rmu-1-section-kv": { id: "S2-no-supply", label: "S2 commercial" },
  "rmu-2-source-kv": { id: "S3-no-supply", label: "S3 industrial" },
  "oltc-1-bus-kv": { id: "bus20-no-supply", label: "20 kV busbar (T1 secondary)" },
};

export type DeviceId = "T1" | "CB1" | "RMU1" | "RMU2" | "F2";
export const DEVICES: DeviceId[] = ["T1", "CB1", "RMU1", "RMU2", "F2"];
export const AUTHORITY_POINTS = {
  T1: { id: "oltc-1-remote", label: "T1 OLTC authority" },
  RMU1: { id: "rmu-1-remote", label: "RMU1 authority" },
  RMU2: { id: "rmu-2-remote", label: "RMU2 authority" },
};
export function switchOffNormal(read: Readings, device: keyof typeof SWITCH_NORMALS): boolean {
  const { id, normal } = SWITCH_NORMALS[device];
  const r = read(id);
  return r.condition === "good" && typeof r.value === "string" &&
    ["open", "closed", "intermediate", "indeterminate"].includes(r.value) && r.value !== normal;
}
export function deviceOffNormal(read: Readings, device: DeviceId): boolean {
  const authority = device in AUTHORITY_POINTS ? AUTHORITY_POINTS[device as keyof typeof AUTHORITY_POINTS] : undefined;
  const local = authority ? read(authority.id).condition === "good" && read(authority.id).value === false : false;
  const manual = read("oltc-1-auto");
  return local || (device === "T1" ? manual.condition === "good" && manual.value === false : switchOffNormal(read, device));
}
export function devicePoints(points: OperatorPoint[], device: DeviceId) {
  const prefixes = device === "T1" ? ["oltc-1-", "plc-1-"] : device === "CB1" ? ["feeder-"] : device === "F2" ? ["f2-"] : [device === "RMU1" ? "rmu-1-" : "rmu-2-"];
  return points.filter((p) => prefixes.some((prefix) => p.id.startsWith(prefix)));
}
export function activeAlarms(state: OperatorState): { id: string; text: string }[] {
  const alarms: { id: string; text: string }[] = [];
  if (state.link.state !== "started") alarms.push({ id: "link", text: `IEC 104 link ${state.link.state}` });
  const asserted = ["feeder-protection-trip", "f2-protection-trip", "rmu-1-earth", "rmu-2-earth", "rmu-1-fault-passage", "rmu-2-fault-passage", "oltc-1-blocked", "oltc-1-overcurrent-block", "oltc-1-at-lower", "oltc-1-at-upper"];
  for (const p of state.points) {
    if (pointCondition(p) !== "good") continue;
    const supply = SUPPLY_VOLTAGES[p.id];
    if (supply && voltageState({ condition: "good", value: p.value }) === "DEAD") {
      alarms.push({ id: supply.id, text: `${supply.label}: loss of supply` });
    }
    if (p.limits && typeof p.value === "number" && Number.isFinite(p.value) && p.value >= LIVE_KV && (p.value < p.limits.low || p.value > p.limits.high)) {
      alarms.push({ id: `${p.id}-voltage-limit`, text: `${pointLabel(p.id)}: voltage limit (${p.value.toFixed(2)} kV; ${p.limits.low.toFixed(2)}–${p.limits.high.toFixed(2)} kV)` });
    }
    if ((asserted.includes(p.id) && p.value === true) || (["plc-1-running", "plc-1-input-valid"].includes(p.id) && p.value === false)) {
      alarms.push({ id: p.id, text: `${pointLabel(p.id)}: ${p.value ? "asserted" : p.id.endsWith("running") ? "stopped" : "invalid"}` });
    }
  }
  return alarms;
}
export type ViewState = "ok" | "lost" | "recovering" | "degraded" | "not-established" | "unavailable";

// A periodic/operator GI does not revoke a completed GI on this connection.
// A completion retained from an earlier connection cannot establish this one.
export function hasCurrentConnectionGi({ since, gi }: ControlCentreLink): boolean {
  return since !== null && gi.last_complete_at !== null && Date.parse(gi.last_complete_at) >= Date.parse(since);
}
// IV alone can mean an electrically undefined measurement, not loss of view.
export function pointObservationNotCurrent(p: OperatorPoint): boolean {
  return p.value === null || p.quality.nt ||
    p.stale_reason === "link_lost" || p.stale_reason === "device_unreachable" ||
    p.stale_reason === "heartbeat" || p.stale_reason === "not_updated" || p.stale_reason === "never_received";
}
export function scadaViewState(state: OperatorState | undefined, unavailable = false): ViewState {
  if (unavailable) return "unavailable";
  if (!state) return "not-established";
  if (state.link.state !== "started") {
    const received = state.link.gi.last_complete_at !== null || state.points.some((p) => p.value !== null || p.received_at !== null);
    return received ? "lost" : "not-established";
  }
  if (!hasCurrentConnectionGi(state.link)) return "recovering";
  return state.points.length > 0 && !state.points.some(pointObservationNotCurrent) ? "ok" : "degraded";
}

export function receivedAuthority(points: OperatorPoint[], device: DeviceId, unavailable = false): "LOCAL" | "REMOTE" | "not established" {
  const authority = device === "T1" || device === "RMU1" || device === "RMU2" ? AUTHORITY_POINTS[device] : undefined;
  const p = authority && points.find((point) => point.id === authority.id);
  return !unavailable && p && pointCondition(p) === "good" && typeof p.value === "boolean" ? p.value ? "REMOTE" : "LOCAL" : "not established";
}
export type VoltageBand = { low: number; high: number; setpoint: number; bus: number | null };
export function regulationBand(points: OperatorPoint[]): VoltageBand | null {
  const number = (id: string) => {
    const p = points.find((p) => p.id === id);
    return p && pointCondition(p) === "good" && typeof p.value === "number" ? p.value : null;
  };
  const setpoint = number("plc-1-setpoint-kv"), percent = number("plc-1-deadband-pct");
  if (setpoint === null || percent === null) return null;
  const delta = setpoint * percent / 100;
  return { low: setpoint - delta, high: setpoint + delta, setpoint, bus: number("oltc-1-bus-kv") };
}
export function reportingText(p: OperatorPoint): string {
  const unit = p.id.endsWith("-p-mw") ? " MW" : p.id.endsWith("-q-mvar") ? " Mvar" : p.id.endsWith("current") ? " A" : p.id.includes("kv") || p.id === "feeder-voltage" ? " kV" : p.id.endsWith("pct") ? " %" : p.id.endsWith("-s") ? " s" : "";
  return p.reporting ? `cyclic ${p.reporting.cyclic_ms / 1000} s · deadband ${p.reporting.deadband}${unit}` : "on change · timed event; GI untimed";
}

export type CommsHealth = "link_down" | "lost" | "degraded" | "ok";
// Group by the source identity supplied by the CC, not UI label prefixes.
export function deviceCommsHealth(state: OperatorState): Record<string, CommsHealth> {
  const groups = new Map<string, OperatorPoint[]>();
  for (const p of state.points) groups.set(p.device, [...(groups.get(p.device) ?? []), p]);
  const lost = (p: OperatorPoint) => p.stale_reason === "device_unreachable" || p.stale_reason === "never_received";
  const degraded = (p: OperatorPoint) => lost(p) || p.stale_reason === "heartbeat" || p.stale_reason === "not_updated";
  return Object.fromEntries([...groups].map(([device, points]) => [device,
    state.link.state !== "started" ? "link_down" : points.every(lost) ? "lost" : points.some(degraded) ? "degraded" : "ok",
  ]));
}
export function abnormalConditions(state: OperatorState): { id: string; text: string }[] {
  const conditions = activeAlarms(state);
  const read = controlCentreObservations(state.points);
  for (const device of Object.keys(SWITCH_NORMALS) as (keyof typeof SWITCH_NORMALS)[]) {
    const { id, label, normal } = SWITCH_NORMALS[device];
    const r = read(id);
    if (switchOffNormal(read, device)) {
      const value = String(r.value);
      conditions.push({ id: `${id}-off-normal`, text: `${label}: ${value.toUpperCase()} (normal ${normal.toUpperCase()})` });
    }
  }
  if (read("oltc-1-auto").condition === "good" && read("oltc-1-auto").value === false) {
    conditions.push({ id: "oltc-1-auto-off-normal", text: "T1: MANUAL (normal AUTO)" });
  }
  for (const authority of Object.values(AUTHORITY_POINTS)) {
    const point = state.points.find((p) => p.id === authority.id);
    if (point && pointCondition(point) === "good" && point.value === false) {
      conditions.push({ id: authority.id, text: `${authority.label}: LOCAL` });
    }
  }
  if (state.link.gi.last_complete_at === null) conditions.push({ id: "gi", text: "GI incomplete" });
  for (const [device, health] of Object.entries(deviceCommsHealth(state))) {
    if (health === "lost" || health === "degraded") conditions.push({ id: `comms-${device}`, text: `${device}: ${health} comms` });
  }
  return conditions;
}
