"use client";

import { useState, type ReactNode } from "react";
import type { DeviceAudit, DeviceAuditEntry, DeviceState, InstructorState, OperatorState } from "../lib/operator-api";
import { IEC104_PORT } from "../lib/firewall-sessions";
import { deviceStateSummary, formatPointValue, formatUtcTime, pointCondition } from "../lib/operator-format";
import {
  useFirewallSessions,
  useInstructorAudit,
  useInstructorFault,
  useInstructorReset,
  useInstructorState,
  useOperatorState,
} from "../lib/operator-queries";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { SessionControls } from "./instructor-sessions";

const AUDIT_SHOWN = 50;

function Row({ label, children, alert }: { label: string; children: ReactNode; alert?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-t border-slate-800/50 py-1 text-xs">
      <span className="text-slate-500">{label}</span>
      <span className={`font-mono ${alert ? "font-bold text-red-300" : "text-slate-200"}`}>{children}</span>
    </div>
  );
}

function Card({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-slate-800 bg-slate-950 p-4">
      <h2 className="text-[10px] font-bold uppercase tracking-wider text-slate-400">{title}</h2>
      <p className="mb-2 text-[10px] text-slate-600">{subtitle}</p>
      {children}
    </section>
  );
}

const yesNo = (value: boolean) => (value ? "YES" : "no");
const num = (value: number | null, digits: number, unit: string) => (value === null ? "—" : `${value.toFixed(digits)} ${unit}`);

export function TruthCards({ truth }: { truth: InstructorState }) {
  const { device, process } = truth;
  const m = device.measurements;
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card title="Breaker device truth" subtitle={`${device.device_id} · instructor API on the management network`}>
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
        <Row label="Truth stale" alert={process.truth_stale}>{yesNo(process.truth_stale)}</Row>
        <Row label="Solve seq · solved (UTC)">
          {process.solve_seq} · {formatUtcTime(process.solved_at)}
        </Row>
        <Row label="Truth revision / generation">
          {process.truth_revision} / {process.truth_generation}
        </Row>
      </Card>
    </div>
  );
}

// Truth next to what the operator holds, so the gap is visible.
export function OperatorContrast({ truth, operator }: { truth: InstructorState; operator: OperatorState | undefined }) {
  const point = (id: string) => operator?.points.find((p) => p.id === id);
  const rows: { label: string; truth: string; id: string }[] = [
    { label: "Breaker position", truth: truth.device.position.toUpperCase(), id: "feeder-breaker-position" },
    { label: "Protection trip", truth: truth.device.protection_tripped ? "TRIPPED" : "NORMAL", id: "feeder-protection-trip" },
    { label: "Current", truth: num(truth.process.current_a, 1, "A"), id: "feeder-current" },
  ];
  return (
    <table className="w-full text-xs">
      <thead>
        <tr className="text-left text-[9px] uppercase tracking-wider text-slate-500">
          <th className="py-1">Quantity</th>
          <th className="py-1">Truth</th>
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
              <td className="py-1 text-slate-100">{row.truth}</td>
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
  if (entries.length === 0) return <div className="text-sm text-slate-500">No device audit entries yet.</div>;
  return (
    <div className="max-h-96 overflow-y-auto">
      <table className="w-full text-[11px]">
        <thead>
          <tr className="text-left text-[9px] uppercase tracking-wider text-slate-500">
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
              <td className="px-1 py-1 text-slate-600">{entry.seq}</td>
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

function DeviceControls({ faultActive }: { faultActive: boolean | undefined }) {
  const fault = useInstructorFault();
  const reset = useInstructorReset();
  const [message, setMessage] = useState<{ text: string; ok: boolean } | null>(null);
  // Fault and reset answer with the device state after the action.
  const done = (action: string) => ({
    onSuccess: (state: DeviceState) => setMessage({ text: `${action}: ${deviceStateSummary(state)}.`, ok: true }),
    onError: (error: Error) => setMessage({ text: `${action} failed: ${errorMessage(error)}`, ok: false }),
  });
  const busy = fault.isPending || reset.isPending;
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        <button
          onClick={() => fault.mutate(true, done("Fault injected"))}
          disabled={busy || faultActive === true}
          className="rounded border border-red-700 bg-red-950/40 px-3 py-1.5 text-xs font-bold text-red-300 hover:bg-red-900/50 disabled:opacity-50"
        >
          Inject fault
        </button>
        <button
          onClick={() => fault.mutate(false, done("Fault cleared"))}
          disabled={busy || faultActive === false}
          className="rounded border border-slate-600 bg-slate-900 px-3 py-1.5 text-xs font-bold text-slate-200 hover:bg-slate-800 disabled:opacity-50"
        >
          Clear fault
        </button>
        <button
          onClick={() => reset.mutate(undefined, done("Device reset"))}
          disabled={busy}
          className="rounded border border-sky-700 bg-sky-950/40 px-3 py-1.5 text-xs font-bold text-sky-300 hover:bg-sky-900/50 disabled:opacity-50"
        >
          Reset device
        </button>
      </div>
      <p className="text-[11px] text-slate-500">
        A fault opens the breaker at once and latches lockout. Clearing it neither recloses nor unlatches; reset
        restores closed, no fault, no lockout.
      </p>
      {message && <p className={`text-xs ${message.ok ? "text-green-300" : "text-red-300"}`}>{message.text}</p>}
    </div>
  );
}

export function InstructorView() {
  const truthQuery = useInstructorState(2000);
  const audit = useInstructorAudit(3000);
  const operator = useOperatorState(2000).data;
  const sessions = useFirewallSessions(IEC104_PORT, 3000);
  const truth = truthQuery.data;
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-4 px-6 py-10">
      <header className="rounded-xl border-2 border-dashed border-fuchsia-800/70 bg-fuchsia-950/10 px-4 py-3">
        <p className="text-[10px] uppercase tracking-[0.3em] text-fuchsia-400">Instructor only</p>
        <h1 className="text-2xl font-bold text-white">Instructor truth — out of band</h1>
        <p className="mt-1 max-w-3xl text-sm text-slate-400">
          Read straight from the breaker device and the solver over the management network, not over IEC 104. The
          Control Centre never sees these values; compare them with it to show what the operator does not know.
        </p>
      </header>

      {truthQuery.error && !isRangeNotReady(truthQuery.error) && (
        <div role="alert" className="rounded-lg border border-red-800/70 bg-red-950/30 px-4 py-2 text-xs text-red-200">
          Truth unavailable: {errorMessage(truthQuery.error)}
          {truth ? " Showing the last answer." : ""}
        </div>
      )}
      {!truth && isRangeNotReady(truthQuery.error) && (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 px-4 py-3 text-sm text-slate-300">
          The truth page shows once the range is ready.
        </div>
      )}

      {truth && (
        <>
          <TruthCards truth={truth} />
          <Card title="Truth vs control centre" subtitle="The same quantities as the operator holds them">
            <OperatorContrast truth={truth} operator={operator} />
          </Card>
        </>
      )}

      <Card title="Fault injection" subtitle="Local protection trips only on an active fault">
        <DeviceControls faultActive={truth?.device.fault_active} />
      </Card>

      <Card title={`Firewall sessions · tcp/${IEC104_PORT}`} subtitle="containd conntrack, original direction">
        <SessionControls sessions={sessions.data?.sessions} sessionsError={sessions.error} />
      </Card>

      <Card title="Device audit" subtitle="Every accepted or rejected action, newest first">
        {audit.data ? (
          <AuditTable audit={audit.data} />
        ) : (
          <div className="text-xs text-slate-500">
            {audit.error ? `Audit unavailable: ${errorMessage(audit.error)}` : "Loading the audit…"}
          </div>
        )}
      </Card>
    </main>
  );
}
