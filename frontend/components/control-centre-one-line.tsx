"use client";

import type { OperatorPoint } from "../lib/operator-api";
import { formatPointValue, pointCondition, type PointCondition } from "../lib/operator-format";

// One-line of the EU feeder as the control centre sees it: 20 kV
// infeed, the feeder breaker, the line and the flat ~1 MW load. Every
// state drawn here comes from the CC's point cache. When a point is
// stale the diagram keeps its last value and marks it, so a cut link
// never reads as an open breaker or a dead feeder.

type Reading = { text: string; condition: PointCondition };

function reading(points: OperatorPoint[], id: string): Reading {
  const point = points.find((p) => p.id === id);
  if (!point) return { text: "not reported", condition: "never" };
  return { text: formatPointValue(id, point.value), condition: pointCondition(point) };
}

function suffix(condition: PointCondition): string {
  if (condition === "stale") return " ⚠ STALE";
  if (condition === "never") return " (no data)";
  return "";
}

const AMBER = "#f59e0b";
const LIVE = "#38bdf8";
const DEAD = "#475569";

export function ControlCentreOneLine({ points }: { points: OperatorPoint[] }) {
  const position = reading(points, "feeder-breaker-position");
  const trip = reading(points, "feeder-protection-trip");
  const current = reading(points, "feeder-current");
  const voltage = reading(points, "feeder-voltage");

  const positionPoint = points.find((p) => p.id === "feeder-breaker-position");
  const closed = positionPoint?.value === "closed";
  const open = positionPoint?.value === "open";
  const positionStale = position.condition !== "good";
  const tripped = points.find((p) => p.id === "feeder-protection-trip")?.value === true;

  // Line colour only claims what the CC knows: live when a good report
  // says closed, de-energised when a good report says open, otherwise
  // an amber dashed "unknown" line.
  const lineColour = positionStale ? AMBER : closed ? LIVE : DEAD;
  const lineDash = positionStale ? "6 5" : undefined;

  return (
    <figure className="space-y-2">
      <svg
        viewBox="0 0 560 190"
        role="img"
        aria-label={`Feeder one-line: breaker ${position.text}${suffix(position.condition)}`}
        className="w-full"
      >
        <defs>
          <pattern id="cc-stale-hatch" width="8" height="8" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="4" height="8" fill={AMBER} fillOpacity="0.35" />
          </pattern>
        </defs>

        {/* Infeed */}
        <circle cx="40" cy="80" r="20" fill="none" stroke={LIVE} strokeWidth="2" />
        <path d="M28 80 q6 -10 12 0 t12 0" fill="none" stroke={LIVE} strokeWidth="2" />
        <text x="40" y="122" textAnchor="middle" className="fill-slate-400 text-[10px]">20 kV grid</text>
        <text x="40" y="136" textAnchor="middle" className="fill-slate-600 text-[9px]">50 Hz infeed</text>

        {/* Busbar */}
        <line x1="60" y1="80" x2="110" y2="80" stroke={LIVE} strokeWidth="3" />
        <line x1="110" y1="45" x2="110" y2="115" stroke={LIVE} strokeWidth="5" />
        <text x="110" y="36" textAnchor="middle" className="fill-slate-500 text-[9px]">20 kV BUS</text>
        <line x1="110" y1="80" x2="196" y2="80" stroke={LIVE} strokeWidth="3" />

        {/* Breaker */}
        <rect
          x="196"
          y="62"
          width="36"
          height="36"
          rx="3"
          fill={closed ? (positionStale ? "url(#cc-stale-hatch)" : LIVE) : "#0f172a"}
          stroke={positionStale ? AMBER : closed ? LIVE : "#cbd5e1"}
          strokeWidth="2.5"
          strokeDasharray={positionStale ? "5 3" : undefined}
        />
        {open && <line x1="200" y1="94" x2="228" y2="66" stroke={positionStale ? AMBER : "#cbd5e1"} strokeWidth="2.5" />}
        {!closed && !open && (
          <text x="214" y="85" textAnchor="middle" className="fill-amber-300 text-[16px] font-bold">?</text>
        )}
        <text x="214" y="50" textAnchor="middle" className="fill-slate-400 text-[10px]">Feeder breaker</text>
        <text
          x="214"
          y="116"
          textAnchor="middle"
          className={`text-[11px] font-bold ${positionStale ? "fill-amber-300" : "fill-slate-100"}`}
        >
          {position.text}
        </text>
        {positionStale && (
          <text x="214" y="130" textAnchor="middle" className="fill-amber-400 text-[9px] font-bold">
            ⚠ STALE · last known
          </text>
        )}
        {tripped && (
          <text x="214" y="146" textAnchor="middle" className="fill-red-400 text-[9px] font-bold">
            PROTECTION TRIP{suffix(trip.condition)}
          </text>
        )}

        {/* Feeder line */}
        <line x1="232" y1="80" x2="470" y2="80" stroke={lineColour} strokeWidth="3" strokeDasharray={lineDash} />
        <text x="350" y="60" textAnchor="middle" className={`text-[11px] font-bold ${current.condition === "good" ? "fill-slate-100" : "fill-amber-300"}`}>
          I {current.text}
          {suffix(current.condition)}
        </text>
        <text x="350" y="104" textAnchor="middle" className={`text-[11px] font-bold ${voltage.condition === "good" ? "fill-slate-100" : "fill-amber-300"}`}>
          U {voltage.text}
          {suffix(voltage.condition)}
        </text>

        {/* Load */}
        <line x1="470" y1="80" x2="470" y2="112" stroke={lineColour} strokeWidth="3" strokeDasharray={lineDash} />
        <path d="M456 112 L484 112 L470 134 Z" fill="none" stroke={lineColour} strokeWidth="2.5" />
        <text x="470" y="152" textAnchor="middle" className="fill-slate-400 text-[10px]">Feeder load</text>
        <text x="470" y="166" textAnchor="middle" className="fill-slate-600 text-[9px]">flat ~1 MW, PF 1.0</text>
      </svg>
      <figcaption className="flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-slate-500">
        <span>
          <span className="font-bold text-sky-400">━</span> live (good report, closed)
        </span>
        <span>
          <span className="font-bold text-slate-500">━</span> de-energised (good report, open)
        </span>
        <span>
          <span className="font-bold text-amber-400">╍</span> unknown: last report stale
        </span>
      </figcaption>
    </figure>
  );
}
