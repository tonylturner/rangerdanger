import type { OperatorPoint, OperatorState } from "./operator-api";
import { pointCondition, pointLabel } from "./operator-format";

export type DeviceId = "T1" | "CB1" | "RMU1" | "RMU2";
export const DEVICES: DeviceId[] = ["T1", "CB1", "RMU1", "RMU2"];
export function devicePoints(points: OperatorPoint[], device: DeviceId) {
  const prefixes = device === "T1" ? ["oltc-1-", "plc-1-"] : device === "CB1" ? ["feeder-"] : [device === "RMU1" ? "rmu-1-" : "rmu-2-"];
  return points.filter((p) => prefixes.some((prefix) => p.id.startsWith(prefix)));
}
export function activeAlarms(state: OperatorState): { id: string; text: string }[] {
  const alarms: { id: string; text: string }[] = [];
  if (state.link.state !== "started") alarms.push({ id: "link", text: `IEC 104 link ${state.link.state}` });
  const asserted = ["feeder-protection-trip", "rmu-1-earth", "rmu-2-earth", "rmu-1-fault-passage", "rmu-2-fault-passage", "oltc-1-blocked", "oltc-1-overcurrent-block", "oltc-1-at-lower", "oltc-1-at-upper"];
  for (const p of state.points) {
    if (pointCondition(p) !== "good") continue;
    if (p.limits && typeof p.value === "number" && p.value > 0.1 && (p.value < p.limits.low || p.value > p.limits.high)) {
      alarms.push({ id: `${p.id}-voltage-limit`, text: `${pointLabel(p.id)}: voltage limit (${p.value.toFixed(2)} kV; ${p.limits.low.toFixed(2)}–${p.limits.high.toFixed(2)} kV)` });
    }
    if ((asserted.includes(p.id) && p.value === true) || (["plc-1-running", "plc-1-input-valid"].includes(p.id) && p.value === false)) {
      alarms.push({ id: p.id, text: `${pointLabel(p.id)}: ${p.value ? "asserted" : p.id.endsWith("running") ? "stopped" : "invalid"}` });
    }
  }
  return alarms;
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
  if (state.link.gi.last_complete_at === null) conditions.push({ id: "gi", text: "GI incomplete" });
  for (const [device, health] of Object.entries(deviceCommsHealth(state))) {
    if (health === "lost" || health === "degraded") conditions.push({ id: `comms-${device}`, text: `${device}: ${health} comms` });
  }
  return conditions;
}
