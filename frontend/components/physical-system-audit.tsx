"use client";
import { useState } from "react";
import type { DeviceAudit, DeviceAuditEntry, FieldDeviceKey } from "../lib/operator-api";
import { formatUtcTime } from "../lib/operator-format";
import { useFieldAudit } from "../lib/operator-queries";
import { errorMessage } from "../lib/utils";

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
  const entries = audit.entries.slice(-50).reverse();
  if (entries.length === 0) return <div className="text-sm text-slate-400">No device audit entries yet.</div>;
  return (
    <div className="max-h-96 overflow-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wider text-slate-400">
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

export function PhysicalAudit() {
  const [device, setDevice] = useState<FieldDeviceKey>("breaker-1");
  const audit = useFieldAudit(3000, device);
  return <section aria-label="Device audit" className="min-w-0 space-y-3">
    <h3 className="text-sm font-semibold text-slate-200">Device audit</h3>
    <p className="text-xs text-slate-400">Accepted and rejected actions with their source, newest first. Control authority is separate from command acceptance.</p>
    <label className="flex flex-wrap items-center gap-2 text-sm text-slate-300">Audit device
      <select aria-label="Audit device" className="max-w-full rounded-sm border border-slate-600 bg-slate-900 p-2" value={device} onChange={(e) => setDevice(e.target.value as FieldDeviceKey)}>
        {(["breaker-1", "f2-breaker", "oltc-1", "rmu-1", "rmu-2"] as const).map((id) => <option key={id}>{id}</option>)}
      </select>
    </label>
    {audit.error && <p role="alert" className="text-xs text-red-300">Audit unavailable: {errorMessage(audit.error)}{audit.data ? " Showing the last answer." : ""}</p>}
    {audit.data ? <AuditTable audit={audit.data} /> : <p className="text-xs text-slate-400">Waiting for device audit…</p>}
  </section>;
}
