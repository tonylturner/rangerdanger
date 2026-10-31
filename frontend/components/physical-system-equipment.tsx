import type { DeviceState, FieldState } from "../lib/operator-api";
import { formatUtcTime } from "../lib/operator-format";
import type { DeviceId } from "../lib/operator-workstation";
import { ACTION, EQUIPMENT, Group, PANEL, Row, num, position, yesNo } from "./physical-system-parts";

export function ProtectionDiagnostics({ device: d }: { device: DeviceState }) {
  return <>
    {d.kind === "breaker" && <><Row label="Protection tripped">{yesNo(d.protection_tripped)}</Row><Row label="Lockout">{yesNo(d.lockout)}</Row></>}
    {d.kind === "rmu" && <><Row label="Earth closed">{yesNo(d.earth_closed)}</Row><Row label="Fault passage">{yesNo(d.fault_passage)}</Row></>}
    {d.kind === "oltc" && <Row label="Effective block">{yesNo(d.blocked || d.protection.picked_up)}</Row>}
    <Row label="Protection function">{d.protection.function}</Row>
    <Row label="Pickup / configured delay">{d.protection.pickup_a} A / {d.protection.delay_ms} ms</Row>
    <Row label="Instantaneous pickup">{d.protection.instantaneous_a === null ? "Not configured" : num(d.protection.instantaneous_a, 0, "A")}</Row>
    <Row label="Protection picked up">{yesNo(d.protection.picked_up)}</Row>
    <Row label="Picked up since (UTC)">{formatUtcTime(d.protection.picked_up_since)}</Row>
    <Row label="Last trip current">{num(d.protection.last_trip_current_a, 1, "A")}</Row>
  </>;
}

export function DeviceDiagnostics({ device }: { device: DeviceState }) {
  const m = device.measurements;
  return <>
    <Row label="Device / kind">{device.device_id} / {device.kind}</Row>
    <Row label="Revision / generation">{device.revision} / {device.generation}</Row>
    <Row label="Event sequence">{device.event_seq}</Row>
    {device.kind === "oltc" && <><Row label="Heartbeat">{device.heartbeat}</Row><Row label="PLC request result">{device.plc_request_result}</Row></>}
    <Row label="Solver epoch">{m.solver_epoch ?? "unknown"}</Row>
    <Row label="Solve seq · solved (UTC)">{m.solve_seq ?? "unknown"} · {formatUtcTime(m.solved_at)}</Row>
    <Row label="Measurement received (UTC)">{formatUtcTime(m.received_at)}</Row>
  </>;
}
export function EquipmentCard({ id, title, device: d, open, onToggle, stale }: {
  id: DeviceId; title: string; device: DeviceState | undefined; open: boolean; onToggle: () => void; stale: boolean;
}) {
  const m = d?.measurements;
  return <article id={`physical-equipment-${id}`} aria-label={title} className={`${PANEL} border-slate-700 scroll-mt-4`}>
    <button type="button" className={`${ACTION} mb-2 flex w-full items-center justify-between gap-2 text-left`} aria-expanded={open} aria-controls={`physical-details-${id}`} onClick={onToggle}>
      <h3 className="font-semibold">{title}</h3><span>{open ? "− Collapse" : "+ Expand"}</span>
    </button>
    {!d || !m ? <p className="text-xs text-amber-300">Unknown · device absent</p> : <>
      <div className="text-sm font-mono text-slate-100">{d.kind === "oltc" ? `Completed tap ${d.tap} · ${d.auto ? "AUTO" : "MANUAL"} · ${d.moving ? "moving" : "stationary"}` : position(d)}</div>
      <div className="my-1 text-xs font-mono text-slate-300">{d.kind === "oltc" ? `Bus ${num(m.voltage_kv_ll, 2, "kV")}` : `I ${num(m.current_a, 1, "A")} · ${d.kind === "breaker" ? `P ${num(m.p_mw, 2, "MW")}` : `Section ${num(m.voltage_kv_ll, 2, "kV")}`}`}</div>
      <p className="text-xs text-slate-300">{d.kind === "breaker" ? `Protection ${d.protection_tripped ? "TRIPPED" : d.protection.picked_up ? "picked up" : "normal"}${d.lockout ? " · LOCKOUT" : ""}` : d.kind === "rmu" ? `FPI ${d.fault_passage ? "LATCHED" : "clear"} · Earth ${d.earth_closed ? "CLOSED" : "open"}` : `Control authority ${d.remote_enabled ? "REMOTE" : "LOCAL"}`}</p>
      <p className={`mt-1 text-xs ${!m.valid || stale ? "text-amber-300" : "text-slate-400"}`}>{!m.valid ? "INVALID · retained measurements" : stale ? "STALE · retained measurements" : "Measurements valid · sim"}</p>
      {open && <div id={`physical-details-${id}`} className="grid gap-x-5 sm:grid-cols-2">
        <Group title="Electrical">
          <Row label="Current">{num(m.current_a, 1, "A")}</Row>
          <Row label={d.kind === "oltc" ? "20 kV bus / 110 kV source (measured)" : "Section / source voltage"}>{num(m.voltage_kv_ll, 2, "kV")} / {num(m.source_kv_ll, 2, "kV")}</Row>
          <Row label="Section / source energization">{stale || !m.valid ? "unknown · retained: " : ""}{yesNo(m.section_live)} / {yesNo(m.source_live)}</Row>
          <Row label="Active power P">{num(m.p_mw, 2, "MW")}</Row>
          <Row label="Reactive power Q">{num(m.q_mvar, 2, "Mvar")}</Row>
          <Row label="Power factor PF">{num(m.pf, 3)}</Row>
        </Group>
        <Group title="Switching & control">
          {d.kind === "oltc" ? <>
            <Row label="Completed tap">{d.tap}</Row><Row label="Target tap">{d.target_tap}</Row>
            <Row label="Mode / moving">{d.auto ? "AUTO" : "MANUAL"} / {yesNo(d.moving)}</Row>
            <Row label="Manual block / armed">{yesNo(d.blocked)} / {yesNo(d.armed)}</Row>
          </> : <Row label="Position">{position(d)}</Row>}
          <Row label="Control authority">{d.remote_enabled ? "REMOTE" : "LOCAL"}</Row>
          <p className="text-xs text-slate-400">Authority does not imply command acceptance. See device audit.</p>
        </Group>
        <Group title="Protection & interlocks">
          <ProtectionDiagnostics device={d} />
        </Group>
        <Group title="Events & timestamps">
          <Row label="Changed (UTC)">{formatUtcTime(d.changed_at)}</Row>
          <Row label="Measurement solved (UTC)">{formatUtcTime(m.solved_at)}</Row>
          <Row label="Measurement received (UTC)">{formatUtcTime(m.received_at)}</Row>
        </Group>
        <Group title="Diagnostics"><DeviceDiagnostics device={d} /></Group>
      </div>}
    </>}
  </article>;
}
export function PhysicalEquipment({ field, expanded, onToggle, unavailable }: { field: FieldState; expanded: Record<string, boolean>; onToggle: (id: DeviceId) => void; unavailable: boolean }) {
  return <section aria-label="Equipment overview" className="space-y-3">
    <h2 className="text-sm font-semibold text-slate-200">Equipment overview</h2>
    <div className="grid min-w-0 gap-3 lg:grid-cols-2">{EQUIPMENT.map(({ id, key, title }) => <EquipmentCard key={id} id={id} title={title} device={field.devices[key]} open={!!expanded[id]} onToggle={() => onToggle(id)} stale={unavailable || field.process.truth_stale} />)}</div>
  </section>;
}
