import type { FieldState } from "./operator-api";
import type { DeviceId } from "./operator-workstation";
export type PhysicalAbnormality = { id: string; text: string; tone: "fault" | "warning" | "info"; device?: DeviceId };
export function physicalAbnormalities(field: FieldState): PhysicalAbnormality[] {
  const result: PhysicalAbnormality[] = [];
  const add = (id: string, text: string, tone: PhysicalAbnormality["tone"], device?: DeviceId) => result.push({ id, text, tone, device });
  for (const [key, d] of Object.entries(field.devices)) {
    const device: DeviceId = key === "breaker-1" ? "CB1" : key === "oltc-1" ? "T1" : key === "rmu-1" ? "RMU1" : "RMU2";
    if (d.kind === "breaker") {
      if (d.protection_tripped) add(`${key}-trip`, `${device} protection TRIPPED`, "fault", device);
      if (d.lockout) add(`${key}-lockout`, `${device} LOCKOUT`, "fault", device);
      if (d.position === "open" && !d.protection_tripped) add(`${key}-open`, `${device} open · off-normal switching`, "info", device);
    }
    if (d.protection.picked_up) add(`${key}-pickup`, `${device} protection picked up`, "warning", device);
    if (!d.remote_enabled) add(`${key}-local`, `${device} LOCAL control authority`, "warning", device);
    if (!d.measurements.valid) add(`${key}-measurement`, `${device} measurements INVALID · retained values`, "warning", device);
    if (d.kind === "rmu") {
      if (d.earth_closed) add(`${key}-earth`, `${device} earth CLOSED`, "fault", device);
      if (d.fault_passage) add(`${key}-fpi`, `${device} fault passage LATCHED`, "warning", device);
      if ((key === "rmu-1" && d.position === "open") || (key === "rmu-2" && d.position === "closed")) add(`${key}-switch`, `${device} ${d.position} · off-normal switching`, "info", device);
    }
    if (d.kind === "oltc" && (d.blocked || d.protection.picked_up)) add(`${key}-block`, "T1 OLTC effective block", "warning", device);
  }
  const p = field.process;
  if (p.fault_section !== "none") add("fault", `Fault section ${p.fault_section}`, "fault");
  if (p.f2_tripped) add("f2", "F2 TRIPPED", "fault");
  if (p.f2_protection.picked_up) add("f2-pickup", "F2 protection picked up", "warning");
  if (p.truth_stale) add("truth", "Simulation device input STALE · energization and paths unknown", "warning");
  for (const [id, delivery] of Object.entries(p.devices)) {
    if (!delivery.delivered) add(`delivery-${id}`, `${id} device delivery FAILED`, "warning");
    else if (delivery.revision !== delivery.solved_revision) add(`revision-${id}`, `${id} awaiting solved revision`, "warning");
  }
  if (field.clock && (field.clock.sync_status !== "locked" || field.clock.alarm || !field.clock.comms_ok)) add("clock", `Time-sync warning · ${field.clock.sync_status}${field.clock.alarm ? " · alarm" : ""}${!field.clock.comms_ok ? " · comms not OK" : ""}`, "warning");
  return result;
}
