"use client";

import type { ReactNode } from "react";
import { reportingText } from "../lib/operator-workstation";
import type { OperatorPoint } from "../lib/operator-api";
import {
  ageMs,
  formatAge,
  formatPointValue,
  formatUtcTime,
  pointCondition,
  pointLabel,
  qualityFlags,
  qualityDefinition,
  staleReasonText,
  type PointCondition,
} from "../lib/operator-format";

// Diagonal amber hatching: the one visual that means "stale" on every
// EU screen. It always comes with the word STALE and a ⚠ glyph, so the
// state never depends on colour alone.
export const STALE_HATCH =
  "bg-[repeating-linear-gradient(135deg,rgba(245,158,11,0.14)_0px,rgba(245,158,11,0.14)_6px,transparent_6px,transparent_12px)]";

export function ConditionBadge({ condition }: { condition: PointCondition }) {
  if (condition === "stale") {
    return (
      <span title={qualityDefinition(condition)} tabIndex={0} className="inline-flex items-center gap-1 rounded border border-dashed border-amber-500 bg-amber-950/60 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-amber-300">
        <span aria-hidden>⚠</span>STALE
      </span>
    );
  }
  if (condition === "never") {
    return (
      <span title={qualityDefinition(condition)} tabIndex={0} className="inline-flex items-center gap-1 rounded border border-dashed border-slate-600 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-slate-400">
        <span aria-hidden>∅</span>NO DATA
      </span>
    );
  }
  return (
    <span title={qualityDefinition(condition)} tabIndex={0} className="inline-flex items-center gap-1 rounded border border-slate-600 bg-slate-900 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-slate-200">
      <span aria-hidden>✓</span>GOOD
    </span>
  );
}

function Th({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <th className={`px-2 py-1.5 text-left text-[9px] font-bold uppercase tracking-wider text-slate-500 ${className}`}>
      {children}
    </th>
  );
}

function PointRow({ point, now }: { point: OperatorPoint; now: number }) {
  const condition = pointCondition(point);
  const stale = condition === "stale";
  const flags = qualityFlags(point.quality);
  const age = ageMs(point.received_at, now);
  return (
    <tr
      data-condition={condition}
      className={`border-t border-slate-800/70 align-top ${
        stale ? `border-dashed ${STALE_HATCH}` : ""
      }`}
    >
      <td className="px-2 py-2">
        <div className="text-xs font-medium text-slate-200">{pointLabel(point.id)}</div>
        <div className="font-mono text-[10px] text-slate-500">
          IOA {point.ioa} · {point.asdu ?? "not received"}
        </div>
      </td>
      <td className="px-2 py-2">
        <div
          className={`font-mono text-sm font-bold ${
            condition === "never" ? "text-slate-500" : stale ? "text-amber-200" : "text-slate-100"
          }`}
        >
          {formatPointValue(point.id, point.value, point.quality)}
        </div>
        {stale && <div className="text-[9px] font-bold uppercase tracking-wider text-amber-400">last known value</div>}
      </td>
      <td className="px-2 py-2">
        <ConditionBadge condition={condition} />
        <div className="mt-1 font-mono text-[10px] text-slate-400">{flags.length > 0 ? flags.join(" ") : "no flags"}</div>
      </td>
      <td className="px-2 py-2 text-xs text-slate-300">{reportingText(point)}</td>
      <td className="px-2 py-2 text-[11px] text-slate-300">{staleReasonText(point.stale_reason) || "—"}</td>
      <td className="px-2 py-2 font-mono text-[11px] text-slate-300">
        {point.source_time === null ? <span className="text-slate-500">untimed</span> : formatUtcTime(point.source_time)}
        {point.source_time_invalid === true && (
          <div className="text-[9px] font-bold uppercase text-amber-400">time IV · clock not synced</div>
        )}
      </td>
      <td className="px-2 py-2 font-mono text-[11px] text-slate-300">{formatUtcTime(point.received_at)}</td>
      <td className={`px-2 py-2 font-mono text-[11px] ${stale ? "font-bold text-amber-300" : "text-slate-400"}`}>
        {formatAge(age)}
      </td>
      <td className="px-2 py-2 font-mono text-[11px] text-slate-500">{point.cot ?? "—"}</td>
    </tr>
  );
}

// The monitored points as the control centre holds them. A stale point
// keeps its last value, received time and RTU time; only its quality,
// reason and growing age change.
export function PointTable({ points, now }: { points: OperatorPoint[]; now: number }) {
  if (points.length === 0) {
    return <div className="py-6 text-center text-sm text-slate-500">The control centre reports no monitored points.</div>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse">
        <thead>
          <tr>
            <Th>Point</Th>
            <Th>Value</Th>
            <Th>Quality</Th>
            <Th>Reporting</Th>
            <Th>Stale reason</Th>
            <Th>RTU time (UTC)</Th>
            <Th>Received (UTC)</Th>
            <Th>Receipt age (not status freshness)</Th>
            <Th>COT</Th>
          </tr>
        </thead>
        <tbody>
          {points.map((point) => (
            <PointRow key={point.id} point={point} now={now} />
          ))}
        </tbody>
      </table>
    </div>
  );
}
