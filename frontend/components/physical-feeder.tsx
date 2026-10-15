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
      <span className="text-slate-400">{label}</span>
      <span className={`font-mono ${alert ? "font-bold text-red-300" : "text-slate-200"}`}>{children}</span>
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

const yesNo = (value: boolean) => (value ? "YES" : "no");
const num = (value: number | null, digits: number, unit: string) => (value === null ? "—" : `${value.toFixed(digits)} ${unit}`);

export function FieldCards({ field }: { field: FieldState }) {
  const { device, process } = field;
  const m = device.measurements;
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card title="Breaker device state" subtitle={`${device.device_id} · simulation API on the management network`}>
        <Row label="Position">{device.position.toUpperCase()}</Row>
        <Row label="Fault active" alert={device.fault_active}>{yesNo(device.fault_active)}</Row>
        <Row label="Protection tripped" alert={device.protection_tripped}>{yesNo(device.protection_tripped)}</Row>
        <Row label="Lockout" alert={device.lockout}>{yesNo(device.lockout)}</Row>
        <Row label="Remote enabled">{yesNo(device.remote_enabled)}</Row>
        <Row label="Revision / generation">
          {device.revision} / {device.generation}
        </Row>
        <Row label="Changed (UTC)">{formatUtcTime(device.changed_at)}</Row>
        <Row label="Measurements">{m.valid ? "valid" : "INVALID"}</Row>
        <Row label="Current / voltage">
          {num(m.current_a, 1, "A")} / {num(m.voltage_kv_ll, 2, "kV")}
        </Row>
      </Card>
      <Card title="Solver output" subtitle={`OpenDSS ${process.profile_id} · ${process.frequency_hz} Hz · ${process.primary_kv} kV`}>
        <Row label="Breaker in model">{process.breaker.toUpperCase()}</Row>
        <Row label="Energized">{yesNo(process.energized)}</Row>
        <Row label="Current">{num(process.current_a, 1, "A")}</Row>
        <Row label="Source / load voltage (L-L)">
          {num(process.source_voltage_kv_ll, 2, "kV")} / {num(process.load_voltage_kv_ll, 2, "kV")}
        </Row>
        <Row label="Load">{num(process.load_kw, 0, "kW")}</Row>
        <Row label="Converged" alert={!process.converged}>{yesNo(process.converged)}</Row>
        <Row label="Device input stale" alert={process.truth_stale}>{yesNo(process.truth_stale)}</Row>
        <Row label="Solve seq · solved (UTC)">
          {process.solve_seq} · {formatUtcTime(process.solved_at)}
        </Row>
        <Row label="Device revision / generation">
          {process.truth_revision} / {process.truth_generation}
        </Row>
      </Card>
    </div>
  );
}

// Physical state next to what the operator holds, so the gap is visible.
export function OperatorContrast({ field, operator }: { field: FieldState; operator: OperatorState | undefined }) {
  const point = (id: string) => operator?.points.find((p) => p.id === id);
  const rows: { label: string; field: string; id: string }[] = [
    { label: "Breaker position", field: field.device.position.toUpperCase(), id: "feeder-breaker-position" },
    { label: "Protection trip", field: field.device.protection_tripped ? "TRIPPED" : "NORMAL", id: "feeder-protection-trip" },
    { label: "Current", field: num(field.process.current_a, 1, "A"), id: "feeder-current" },
  ];
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="text-left text-[9px] uppercase tracking-wider text-slate-400">
          <th className="py-1">Quantity</th>
          <th className="py-1">Field</th>
          <th className="py-1">Control centre shows</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          const p = point(row.id);
          const condition = p ? pointCondition(p) : "never";
          return (
            <tr key={row.id} className="border-t border-slate-800/50 font-mono">
              <td className="py-1 font-sans text-slate-400">{row.label}</td>
              <td className="py-1 text-slate-100">{row.field}</td>
              <td className={`py-1 ${condition === "good" ? "text-slate-100" : "font-bold text-amber-300"}`}>
                {p ? formatPointValue(p.id, p.value) : "not reported"}
                {condition === "stale" && " ⚠ STALE"}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// The position change, a first solve's numbers, or the device's reason
// (a rejection cause or "no_op").
function change(entry: DeviceAuditEntry): string {
  const { detail } = entry;
  if (entry.operation === "first_solve" && typeof detail === "object") {
    return `revision ${String(detail.revision)} · solve ${String(detail.solve_seq)} · ${String(detail.current_a)} A`;
  }
  const parts: string[] = [];
  if (entry.before.position !== entry.after.position) parts.push(`${entry.before.position} → ${entry.after.position}`);
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
          Read directly from the breaker and physics engine, outside IEC 104. The control centre never reads
          these values: it sees only what IEC 104 delivers. Compare the field with its last received values below.
          Run lab steps to inject or clear faults and restore the device, or use Reset Lab.
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
      <Card title="Device audit" subtitle="Every accepted or rejected action with its source, newest first">
        {auditError ? <p role="alert" className="text-xs text-red-300">Audit unavailable: {errorMessage(auditError)}{audit ? " Showing the last answer." : ""}</p> : null}
        {audit ? <AuditTable audit={audit} /> : <p className="text-xs text-slate-400">Waiting for device audit…</p>}
      </Card>
    </section>
  );
}
