"use client";

import type { OperatorPoint } from "../lib/operator-api";
import { formatPointValue, pointCondition, qualityFlags } from "../lib/operator-format";

// Every symbol is independently observed. No field/process input is accepted.
export function observedReading(points: OperatorPoint[], id: string, unavailable = false) {
  const p = points.find((point) => point.id === id);
  const condition = !p || p.value === null ? "unknown" : unavailable ? "stale" : pointCondition(p);
  const flags = p ? qualityFlags(p.quality) : [];
  const transient = typeof p?.value === "object" && p.value !== null && p.value.transient;
  const shortText = `${p ? formatPointValue(id, p.value) : "unknown"} · ${condition.toUpperCase()}${flags.length ? ` · ${flags.join(" ")}` : ""}`;
  const text = `${shortText}${p?.stale_reason ? ` · ${p.stale_reason}` : ""}${unavailable ? " · CC API unavailable" : ""}`;
  return { value: p?.value, condition, text, shortText, transient };
}

export function ControlCentreOneLine({ points, unavailable = false }: { points: OperatorPoint[]; unavailable?: boolean }) {
  const read = (id: string) => observedReading(points, id, unavailable);
  const voltage = (id: string) => {
    const r = read(id);
    return r.condition !== "good" || typeof r.value !== "number" ? { color: "#fbbf24", dash: "6 5", state: "UNKNOWN" }
      : r.value > 0 ? { color: "#38bdf8", dash: undefined, state: "LIVE" } : { color: "#94a3b8", dash: undefined, state: "DEAD" };
  };
  const section = (x1: number, y1: number, x2: number, y2: number, id: string) => {
    const v = voltage(id);
    return <line x1={x1} y1={y1} x2={x2} y2={y2} stroke={v.color} strokeWidth="3" strokeDasharray={v.dash} />;
  };
  const switchSymbol = (x: number, y: number, label: string, id: string) => {
    const r = read(id);
    const known = r.value === "open" || r.value === "closed";
    return <g aria-label={`${label}: ${r.text}`}>
      <title>{label}: {r.text}</title>
      <rect x={x - 16} y={y - 16} width="32" height="32" fill="#0f172a" stroke={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeWidth="2" strokeDasharray={r.condition !== "good" ? "5 3" : undefined} />
      {known ? <line x1={x - 11} y1={y + 10} x2={x + 11} y2={r.value === "closed" ? y - 10 : y + 10} stroke="#e2e8f0" strokeWidth="3" /> : <text x={x} y={y + 5} textAnchor="middle" fill="#fbbf24">?</text>}
      <text x={x} y={y - 25} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label}</text>
      <text x={x} y={y + 35} textAnchor="middle" fill={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="11">{r.shortText}</text>
    </g>;
  };
  const busLabel = (x: number, y: number, label: string, id: string) => <g aria-label={`${label}: ${read(id).text}`}>
    <text x={x} y={y} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label} · {voltage(id).state}</text>
    <text x={x} y={y + 18} textAnchor="middle" fill={voltage(id).color} fontSize="11">{read(id).shortText}</text>
  </g>;
  const tap = read("oltc-1-tap");
  return <figure className="space-y-3">
    <div className="overflow-x-auto">
      <svg viewBox="0 0 1060 410" role="img" aria-label="CC-observed CIGRE-derived teaching feeder" className="w-full min-w-[850px]">
        <text x="80" y="35" textAnchor="middle" fill="#cbd5e1" fontSize="13">110 kV source · UNOBSERVED</text>
        <line x1="80" y1="45" x2="80" y2="70" stroke="#94a3b8" strokeDasharray="6 5" />
        <g aria-label={`T1 OLTC: ${tap.text}`}>
          <circle cx="80" cy="90" r="20" fill="none" stroke={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <circle cx="80" cy="113" r="20" fill="none" stroke="#cbd5e1" />
          <text x="125" y="88" fill="#e2e8f0" fontSize="13">T1 110/20 kV · OLTC</text>
          <text x="125" y="108" fill={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="11">Tap {tap.shortText}</text>
          <text x="125" y="128" fill="#cbd5e1" fontSize="11">{read("oltc-1-auto").shortText}</text>
        </g>
        {section(80, 133, 80, 325, "oltc-1-bus-kv")}
        {section(80, 190, 310, 190, "oltc-1-bus-kv")}
        {switchSymbol(310, 190, "CB1", "feeder-breaker-position")}
        {section(326, 190, 570, 190, "feeder-voltage")}
        {busLabel(440, 245, "S1 residential", "feeder-voltage")}
        {switchSymbol(570, 190, "RMU1 · NC", "rmu-1-position")}
        {section(586, 190, 850, 190, "rmu-1-section-kv")}
        {busLabel(710, 245, "S2 commercial", "rmu-1-section-kv")}
        {switchSymbol(850, 190, "RMU2 tie · NO", "rmu-2-position")}
        {section(866, 190, 1000, 190, "rmu-2-source-kv")}
        {section(1000, 190, 1000, 325, "rmu-2-source-kv")}
        <line x1="80" y1="325" x2="305" y2="325" stroke="#94a3b8" strokeWidth="3" strokeDasharray="6 5" />
        <rect x="305" y="309" width="32" height="32" fill="#0f172a" stroke="#94a3b8" strokeDasharray="5 3" />
        <text x="321" y="331" textAnchor="middle" fill="#cbd5e1">?</text>
        <text x="321" y="365" textAnchor="middle" fill="#cbd5e1" fontSize="12">F2 head · UNOBSERVED</text>
        {section(337, 325, 1000, 325, "rmu-2-source-kv")}
        {busLabel(710, 365, "S3 industrial", "rmu-2-source-kv")}
      </svg>
    </div>
    <figcaption className="text-xs text-slate-400">Blue: observed voltage &gt; 0 kV. Grey: observed zero. Dashed amber: unknown or stale, never inferred from switch position. Tap TRANSIENT means motion; the displayed tap is the last completed step. F2 has no telemetry.</figcaption>
    <dl className="grid gap-2 text-xs text-slate-300 sm:grid-cols-2 lg:grid-cols-3">
      {["oltc-1-bus-kv", "feeder-protection-trip", "rmu-1-remote", "rmu-1-earth", "rmu-1-fault-passage", "rmu-2-remote", "rmu-2-earth", "rmu-2-fault-passage", "oltc-1-remote", "oltc-1-blocked", "oltc-1-at-lower", "oltc-1-at-upper", "plc-1-running", "plc-1-input-valid", "plc-1-setpoint-kv", "plc-1-deadband-pct", "plc-1-delay-s"].map((id) => <div key={id}><dt className="text-slate-400">{id}</dt><dd>{read(id).text}</dd></div>)}
    </dl>
  </figure>;
}
