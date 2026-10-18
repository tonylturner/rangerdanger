import type { ControlCentreLink, OperatorPoint, OperatorState } from "./operator-api";
import { pointCondition, pointLabel } from "./operator-format";
import { LIVE_KV, voltageState } from "./electrical-state";
import { controlCentreObservations } from "./observation-readings";
import { SECTION_VOLTAGE, type SectionId } from "./operator-supply-path";

export type DeviceId = "T1" | "CB1" | "RMU1" | "RMU2" | "F2";
export const DEVICES: DeviceId[] = ["T1", "CB1", "RMU1", "RMU2", "F2"];
export const AUTHORITY_POINTS = {
  T1: { id: "oltc-1-remote", label: "T1 OLTC authority" },
  RMU1: { id: "rmu-1-remote", label: "RMU1 authority" },
  RMU2: { id: "rmu-2-remote", label: "RMU2 authority" },
};
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
    if (p.limits && typeof p.value === "number" && Number.isFinite(p.value) && p.value >= LIVE_KV && (p.value < p.limits.low || p.value > p.limits.high)) {
      alarms.push({ id: `${p.id}-voltage-limit`, text: `${pointLabel(p.id)}: voltage limit (${p.value.toFixed(2)} kV; ${p.limits.low.toFixed(2)}–${p.limits.high.toFixed(2)} kV)` });
    }
    if ((asserted.includes(p.id) && p.value === true) || (["plc-1-running", "plc-1-input-valid"].includes(p.id) && p.value === false)) {
      alarms.push({ id: p.id, text: `${pointLabel(p.id)}: ${p.value ? "asserted" : p.id.endsWith("running") ? "stopped" : "invalid"}` });
    }
  }
  return alarms;
}
export type ViewState = "ok" | "lost" | "recovering" | "not-established" | "unavailable";

// A periodic/operator GI does not revoke a completed GI on this connection.
// A completion retained from an earlier connection cannot establish this one.
export function hasCurrentConnectionGi({ since, gi }: ControlCentreLink): boolean {
  return since !== null && gi.last_complete_at !== null && Date.parse(gi.last_complete_at) >= Date.parse(since);
}
export function scadaViewState(state: OperatorState | undefined, unavailable = false): ViewState {
  if (unavailable) return "unavailable";
  if (!state) return "not-established";
  if (state.link.state !== "started") {
    const received = state.link.gi.last_complete_at !== null || state.points.some((p) => p.value !== null || p.received_at !== null);
    return received ? "lost" : "not-established";
  }
  return hasCurrentConnectionGi(state.link) && state.points.length > 0 && state.points.every((p) => pointCondition(p) === "good") ? "ok" : "recovering";
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
  for (const [id, label, normal] of [
    ["feeder-breaker-position", "CB1", "closed"],
    ["rmu-1-position", "RMU1 · NC", "closed"],
    ["rmu-2-position", "RMU2 · NOP", "open"],
  ]) {
    const r = read(id);
    if (r.condition === "good" && typeof r.value === "string" && ["open", "closed", "intermediate", "indeterminate"].includes(r.value) && r.value !== normal) {
      conditions.push({ id: `${id}-off-normal`, text: `${label}: ${r.value.toUpperCase()} (normal ${normal.toUpperCase()})` });
    }
  }
  for (const [section, label] of Object.entries({ S1: "residential", S2: "commercial", S3: "industrial" })) {
    if (voltageState(read(SECTION_VOLTAGE[section as SectionId])) === "DEAD") {
      conditions.push({ id: `${section}-no-supply`, text: `${section} ${label}: no supply` });
    }
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
