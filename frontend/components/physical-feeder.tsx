"use client";

import type { ReactNode } from "react";
import type { DeviceAudit, DeviceAuditEntry, FieldState, OperatorState } from "../lib/operator-api";
import { formatPointValue, formatUtcTime, pointCondition } from "../lib/operator-format";
import { useFieldAudit, useFieldState } from "../lib/operator-queries";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";

const AUDIT_SHOWN = 50;

function Row({ label, children, alert }: { label: string; children: ReactNode; alert?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-t border-slate-800/50 py-1 text-xs">
      <span className="shrink-0 text-slate-400">{label}</span>
      <span className={`min-w-0 break-words text-right font-mono ${alert ? "font-bold text-red-300" : "text-slate-200"}`}>{children}</span>
    </div>
  );
}

function Card({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-slate-800 bg-slate-950 p-4">
      <h2 className="text-[10px] font-bold uppercase tracking-wider text-slate-400">{title}</h2>
      <p className="mb-2 text-[10px] text-slate-400">{subtitle}</p>
      {children}
    </section>
  );
}

const yesNo = (value: boolean | null) => (value === null ? "unknown" : value ? "YES" : "no");
const num = (value: number | null, digits: number, unit: string) => (value === null ? "unknown" : `${value.toFixed(digits)} ${unit}`);

export function FieldCards({ field }: { field: FieldState }) {
  const { devices, clock, process } = field;
  return <div className="grid gap-4 md:grid-cols-2">
    {Object.entries(devices).map(([node, device]) => {
      const m = device.measurements;
      return <Card key={node} title={`${node} device state`} subtitle={`${device.device_id} · ${device.kind} · management network, outside IEC 104`}>
        {device.kind === "oltc" ? <>
          <Row label="Completed / target tap">{device.tap} / {device.target_tap}</Row>
          <Row label="Moving">{yesNo(device.moving)}</Row>
          <Row label="Mode">{device.auto ? "AUTO" : "MANUAL"}</Row>
          <Row label="Blocked / armed">{yesNo(device.blocked)} / {yesNo(device.armed)}</Row>
          <Row label="PLC request result">{device.plc_request_result}</Row>
          <Row label="Heartbeat">{device.heartbeat}</Row>
        </> : <Row label="Position">{device.position.toUpperCase()}</Row>}
        {device.kind === "breaker" && <>
          <Row label="Protection tripped" alert={device.protection_tripped}>{yesNo(device.protection_tripped)}</Row>
          <Row label="Lockout" alert={device.lockout}>{yesNo(device.lockout)}</Row>
        </>}
        {device.kind === "rmu" && <>
          <Row label="Earth closed" alert={device.earth_closed}>{yesNo(device.earth_closed)}</Row>
          <Row label="Fault passage" alert={device.fault_passage}>{yesNo(device.fault_passage)}</Row>
        </>}
        <Row label="Remote enabled">{yesNo(device.remote_enabled)}</Row>
        <Row label="Event sequence">{device.event_seq}</Row>
        <Row label="Revision / generation">{device.revision} / {device.generation}</Row>
        <Row label="Changed (UTC)">{formatUtcTime(device.changed_at)}</Row>
        <Row label="Measurements" alert={!m.valid}>{m.valid ? "valid" : "INVALID · last values, not current"}</Row>
        <Row label="Current">{num(m.current_a, 1, "A")}</Row>
        <Row label="Section / source voltage">{num(m.voltage_kv_ll, 2, "kV")} / {num(m.source_kv_ll, 2, "kV")}</Row>
        <Row label="Section / source live">{yesNo(m.section_live)} / {yesNo(m.source_live)}</Row>
        <Row label="Fault current" alert={m.valid && m.fault_current === true}>{yesNo(m.fault_current)}</Row>
        <Row label="Solve seq · solved (UTC)">{m.solve_seq ?? "unknown"} · {formatUtcTime(m.solved_at)}</Row>
        <Row label="Received (UTC)">{formatUtcTime(m.received_at)}</Row>
      </Card>;
    })}
    {clock && <Card title="Station clock (GPS) · truth" subtitle="Management network, outside IEC 104 · not the control centre time">
      <Row label="Sync status" alert={clock.sync_status !== "locked"}>{clock.sync_status.toUpperCase()}</Row>
      <Row label="Time offset">{num(clock.time_offset_sec, 3, "s")}</Row>
      <Row label="Satellites">{clock.satellite_count}</Row>
      <Row label="Reported time (UTC)">{clock.reported_time}</Row>
      <Row label="Actual time (UTC)">{clock.actual_time}</Row>
      <Row label="Alarm / comms OK" alert={clock.alarm || !clock.comms_ok}>{yesNo(clock.alarm)} / {yesNo(clock.comms_ok)}</Row>
    </Card>}
    <Card title="Solver output" subtitle={`OpenDSS · ${process.profile_id} · separate process plane`}>
      <Row label="Fault section" alert={process.fault_section !== "none"}>{process.fault_section}</Row>
      <Row label="F2 tripped" alert={process.f2_tripped}>{yesNo(process.f2_tripped)}</Row>
      <Row label="Load multiplier">{process.load_multiplier}</Row>
      <Row label="Source / tap">{process.source_pu} p.u. / {process.tap}</Row>
      <Row label="Device input stale" alert={process.truth_stale}>{yesNo(process.truth_stale)}</Row>
      <Row label="Solve seq · solved (UTC)">{process.solve_seq} · {formatUtcTime(process.solved_at)}</Row>
      {Object.entries(process.buses).map(([id, bus]) => <Row key={id} label={`${id} voltage / live`}>{num(bus.kv, 2, "kV")} / {yesNo(bus.live)}</Row>)}
      {Object.entries(process.devices).map(([id, delivery]) => <Row key={id} label={`${id} delivery`} alert={!delivery.delivered || delivery.solved_revision !== delivery.revision}>
        {delivery.delivered ? "delivered" : "FAILED"} · solved rev {delivery.solved_revision} / current {delivery.revision}
      </Row>)}
    </Card>
  </div>;
}

// Contrast is a separate table, not an input to any CC symbol.
export function OperatorContrast({ field, operator }: { field: FieldState; operator: OperatorState | undefined }) {
  const rows = Object.entries(field.devices).flatMap(([node, d]) => {
    const prefix = node === "breaker-1" ? "feeder-breaker" : node;
    return [{ label: `${node} ${d.kind === "oltc" ? "tap" : "position"}`, field: d.kind === "oltc" ? String(d.tap) : d.position.toUpperCase(), id: `${prefix}-${d.kind === "oltc" ? "tap" : "position"}` }];
  });
  return <div className="overflow-x-auto"><table className="w-full text-xs">
    <thead><tr className="text-left text-slate-400"><th>Quantity</th><th>Field</th><th>Control centre shows</th></tr></thead>
    <tbody>{rows.map((row) => {
      const p = operator?.points.find((point) => point.id === row.id);
      const condition = p ? pointCondition(p) : "never";
      return <tr key={row.id} className="border-t border-slate-800 font-mono"><td className="py-2 text-slate-400">{row.label}</td><td>{row.field}</td>
        <td className={condition === "good" ? "text-slate-200" : "text-amber-300"}>{p ? formatPointValue(p.id, p.value) : "unknown"}{condition === "stale" ? " ⚠ STALE" : condition === "never" ? " · UNKNOWN" : ""}</td></tr>;
    })}</tbody>
  </table></div>;
}

// The position change, a first solve's numbers, or the device's reason
// (a rejection cause or "no_op").
function change(entry: DeviceAuditEntry): string {
  const { detail } = entry;
  if (entry.operation === "first_solve" && typeof detail === "object" && detail !== null) {
    return `revision ${String(detail.revision)} · solve ${String(detail.solve_seq)} · ${String(detail.current_a)} A`;
  }
  const parts: string[] = [];
  if ("position" in entry.before && "position" in entry.after && entry.before.position !== entry.after.position) parts.push(`${entry.before.position} → ${entry.after.position}`);
  if (typeof detail === "string") parts.push(detail);
  return parts.join(" · ");
}

export function AuditTable({ audit }: { audit: DeviceAudit }) {
  const entries = audit.entries.slice(-AUDIT_SHOWN).reverse();
  if (entries.length === 0) return <div className="text-sm text-slate-400">No device audit entries yet.</div>;
  return (
    <div className="max-h-96 overflow-y-auto">
      <table className="w-full text-[11px]">
        <thead>
          <tr className="text-left text-[9px] uppercase tracking-wider text-slate-400">
            <th className="px-1 py-1">Seq</th>
            <th className="px-1 py-1">Time (UTC)</th>
            <th className="px-1 py-1">Operation</th>
            <th className="px-1 py-1">Result</th>
            <th className="px-1 py-1">Protocol · zone · peer</th>
            <th className="px-1 py-1">Change</th>
          </tr>
        </thead>
        <tbody className="font-mono">
          {entries.map((entry) => (
            <tr key={entry.seq} className="border-t border-slate-800/50">
              <td className="px-1 py-1 text-slate-400">{entry.seq}</td>
              <td className="px-1 py-1 text-slate-400">{formatUtcTime(entry.at)}</td>
              <td className="px-1 py-1 text-slate-200">{entry.operation}</td>
              <td className="px-1 py-1 text-slate-300">{entry.result}</td>
              <td className="px-1 py-1 text-slate-400">
                {entry.context.protocol} · {entry.context.zone} · {entry.context.peer}
              </td>
              <td className="px-1 py-1 text-slate-300">{change(entry)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function PhysicalFeeder({ operator }: { operator: OperatorState | undefined }) {
  const state = useFieldState(2000);
  const audit = useFieldAudit(3000);
  return <PhysicalFeederScreen field={state.data} stateError={state.error} audit={audit.data} auditError={audit.error} operator={operator} />;
}

export function PhysicalFeederScreen({ field, stateError, audit, auditError, operator }: {
  field: FieldState | undefined;
  stateError: unknown;
  audit: DeviceAudit | undefined;
  auditError: unknown;
  operator: OperatorState | undefined;
}) {
  return (
    <section aria-label="Physical feeder state" className="space-y-4 border-t border-slate-700 pt-6">
      <header>
        <h2 className="text-lg font-bold text-white">Live physical feeder state via OpenDSS</h2>
        <p className="mt-1 max-w-3xl text-sm text-slate-400">
          Read directly from all field devices and the physics engine, outside IEC 104. The control centre never reads
          these values: it sees only what IEC 104 delivers. Compare the field with its last received values below.
          Run lab steps to inject or clear faults and restore the devices, or use Reset Lab.
        </p>
      </header>
      {stateError && !isRangeNotReady(stateError) ? (
        <p role="alert" className="text-sm text-red-300">Physical feeder unavailable: {errorMessage(stateError)}{field ? " Showing the last answer." : ""}</p>
      ) : null}
      {field ? <>
        <FieldCards field={field} />
        <Card title="Field vs control centre" subtitle="Physical state compared with the last values delivered over IEC 104">
          <OperatorContrast field={field} operator={operator} />
        </Card>
      </> : <p className="text-sm text-slate-400">{isRangeNotReady(stateError) ? "Physical feeder state shows once the range is ready." : "Waiting for physical feeder state…"}</p>}
      <Card title="Device audit" subtitle={`${audit?.entries.at(-1)?.after.device_id ?? "Device"} · accepted and rejected actions with their source, newest first`}>
        {auditError ? <p role="alert" className="text-xs text-red-300">Audit unavailable: {errorMessage(auditError)}{audit ? " Showing the last answer." : ""}</p> : null}
        {audit ? <AuditTable audit={audit} /> : <p className="text-xs text-slate-400">Waiting for device audit…</p>}
      </Card>
    </section>
  );
}
