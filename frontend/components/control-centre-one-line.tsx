"use client";

import type { OperatorPoint } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { formatPointValue, pointCondition, qualityFlags } from "../lib/operator-format";

// Every symbol is independently observed. No field/process input is accepted.
export function observedReading(points: OperatorPoint[], id: string, unavailable = false) {
  const p = points.find((point) => point.id === id);
  const condition = !p || p.value === null ? "unknown" : pointCondition(p);
  const flags = p ? qualityFlags(p.quality) : [];
  const transient = typeof p?.value === "object" && p.value !== null && p.value.transient;
  const shortText = `${p ? formatPointValue(id, p.value) : "unknown"} · ${condition.toUpperCase()}${flags.length ? ` · ${flags.join(" ")}` : ""}`;
  const text = `${shortText}${p?.stale_reason ? ` · ${p.stale_reason}` : ""}${unavailable ? " · cached snapshot · CC API unavailable" : ""}`;
  return { value: p?.value, condition, text, shortText, transient };
}

export function ControlCentreOneLine({ points, unavailable = false, selected = "T1", onSelect = () => {} }: { points: OperatorPoint[]; unavailable?: boolean; selected?: DeviceId; onSelect?: (id: DeviceId) => void }) {
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
  const selectProps = (device: DeviceId) => ({
    role: "button", tabIndex: 0, "aria-pressed": selected === device,
    onClick: () => onSelect(device), onKeyDown: (e: React.KeyboardEvent<SVGGElement>) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(device); }
    }, className: "cursor-pointer outline-none focus-visible:[outline:2px_solid_#38bdf8]",
  });
  const measure = (x: number, y: number, id: string, label = "") => <text x={x} y={y} textAnchor="middle" fill={read(id).condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">{label}{read(id).shortText}</text>;
  const indicators = (x: number, prefix: string) => {
    const earth = read(`${prefix}-earth`), fault = read(`${prefix}-fault-passage`);
    return <g>
      <g aria-label={`Earth switch: ${earth.text}`}><title>{`Earth switch: ${earth.text}`}</title>
        {earth.value === undefined || earth.value === null ? <text x={x} y="290" fill="#fbbf24" textAnchor="middle">?</text> : <path d={`M${x} 270 v8 m-10 14 h20 m-16 5 h12 m-9 5 h6 M${x} 278 l${earth.value === true ? "0 14" : "8 8"}`} stroke={earth.condition !== "good" || earth.value === true ? "#fbbf24" : "#cbd5e1"} strokeDasharray={earth.condition !== "good" ? "3 3" : undefined} fill="none" />}
      </g>
      <g aria-label={`Fault passage: ${fault.text}`}><title>{`Fault passage: ${fault.text}`}</title><circle cx={x + 35} cy="282" r="8" fill={fault.condition !== "good" ? "url(#cc-unknown)" : fault.value === true ? "#fbbf24" : "none"} stroke={fault.condition !== "good" || fault.value === true ? "#fbbf24" : "#cbd5e1"} /><text x={x + 48} y="286" fill="#cbd5e1" fontSize="10">FPI</text></g>
    </g>;
  };
  const switchSymbol = (x: number, y: number, label: string, id: string, device: DeviceId) => {
    const r = read(id);
    const known = r.value === "open" || r.value === "closed";
    return <g {...selectProps(device)} aria-label={`${label}: ${r.text}`}>
      {selected === device && <rect x={x - 23} y={y - 23} width="46" height="46" rx="4" fill="none" stroke="#e2e8f0" />}
      <title>{`${label}: ${r.text}`}</title>
      <rect x={x - 16} y={y - 16} width="32" height="32" fill={r.condition !== "good" ? "url(#cc-unknown)" : r.value === "closed" ? "#cbd5e1" : "#0f172a"} stroke={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeWidth="2" strokeDasharray={r.condition !== "good" ? "5 3" : undefined} />
      {r.condition !== "good" && r.value === "closed" && <rect x={x - 10} y={y - 10} width="20" height="20" fill="#fbbf24" fillOpacity="0.6" />}
      {!known && <text x={x} y={y + 5} textAnchor="middle" fill="#fbbf24">{r.value === "intermediate" ? "½" : r.value === "indeterminate" ? "×" : "?"}</text>}
      <text x={x} y={y - 25} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label}</text>
      <text x={x} y={y + 35} textAnchor="middle" fill={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="11">{r.shortText}</text>
    </g>;
  };
  const busLabel = (x: number, y: number, label: string, id: string) => <g aria-label={`${label}: ${read(id).text}`}>
    <text x={x} y={y} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label} · {voltage(id).state}</text>
    <text x={x} y={y + 18} textAnchor="middle" fill={voltage(id).color} fontSize="11">{read(id).shortText}</text>
  </g>;
  const tap = read("oltc-1-tap");
  return <figure className="min-w-0 space-y-3">
    <div className="max-w-full overflow-x-auto">
      <svg viewBox="0 0 1060 410" role="group" aria-label="CC-observed CIGRE-derived teaching feeder" className="w-full min-w-[850px]">
        <defs><pattern id="cc-unknown" width="8" height="8" patternUnits="userSpaceOnUse"><path d="M0 8 L8 0" stroke="#fbbf24" strokeOpacity="0.5" /></pattern></defs>
        <text x="16" y="35" textAnchor="start" fill="#cbd5e1" fontSize="13">110 kV source · UNOBSERVED</text>
        <line x1="80" y1="45" x2="80" y2="70" stroke="#94a3b8" strokeDasharray="6 5" />
        <g {...selectProps("T1")} aria-label={`T1 OLTC: ${tap.text}`}>
          {selected === "T1" && <rect x="52" y="65" width="55" height="76" rx="4" stroke="#e2e8f0" fill="none" />}
          <circle cx="80" cy="90" r="20" fill="none" stroke={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <circle cx="80" cy="113" r="20" fill="none" stroke="#cbd5e1" />
          <text x="125" y="88" fill="#e2e8f0" fontSize="13">T1 110/20 kV · OLTC</text>
          <text x="125" y="108" fill={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="11">Tap {tap.shortText}</text>
          <text x="125" y="128" fill="#cbd5e1" fontSize="11">{read("oltc-1-auto").shortText}</text>
        </g>
        {measure(175, 150, "oltc-1-current")}
        {measure(150, 285, "oltc-1-bus-kv", "Bus ")}
        {measure(310, 120, "feeder-current")}
        {measure(570, 120, "rmu-1-current")}
        {measure(570, 140, "rmu-1-source-kv", "Source ")}
        {measure(850, 120, "rmu-2-current")}
        {measure(920, 245, "rmu-2-section-kv", "Section ")}
        {measure(920, 265, "rmu-2-source-kv", "Source ")}
        {indicators(540, "rmu-1")}
        {indicators(810, "rmu-2")}
        {section(80, 133, 80, 325, "oltc-1-bus-kv")}
        {section(80, 190, 310, 190, "oltc-1-bus-kv")}
        {switchSymbol(310, 190, "CB1", "feeder-breaker-position", "CB1")}
        {section(326, 190, 570, 190, "feeder-voltage")}
        {busLabel(440, 245, "S1 residential", "feeder-voltage")}
        {switchSymbol(570, 190, "RMU1 · NC", "rmu-1-position", "RMU1")}
        {section(586, 190, 850, 190, "rmu-1-section-kv")}
        {busLabel(710, 245, "S2 commercial", "rmu-1-section-kv")}
        {switchSymbol(850, 190, "RMU2 tie · NO", "rmu-2-position", "RMU2")}
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
    <figcaption className="text-xs text-slate-400">Select a device for its faceplate. Filled switch: closed; hollow: open; ½: intermediate; ×: indeterminate. Earth blade and FPI show received indications. Blue: observed voltage &gt; 0 kV. Grey: observed zero. Dashed amber: unknown or stale, never inferred from switch position. Tap TRANSIENT means motion; the displayed tap is the last completed step. F2 has no telemetry.</figcaption>
  </figure>;
}
