"use client";

import type { OperatorPoint } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { formatPointValue, pointCondition, pointLabel, qualityFlags } from "../lib/operator-format";
import { SECTION_VOLTAGE, sectionSupplyPath, type SectionId } from "../lib/operator-supply-path";

// Every symbol is independently observed. No field/process input is accepted.
export function observedReading(points: OperatorPoint[], id: string, unavailable = false) {
  const p = points.find((point) => point.id === id);
  const condition = !p || p.value === null ? "unknown" : pointCondition(p);
  const flags = p ? qualityFlags(p.quality) : [];
  const transient = typeof p?.value === "object" && p.value !== null && p.value.transient;
  const shortText = `${p ? formatPointValue(id, p.value, p.quality) : "unknown"} · ${condition.toUpperCase()}${flags.length ? ` · ${flags.join(" ")}` : ""}`;
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
  const measure = (x: number, y: number, id: string) => <g aria-label={`${pointLabel(id)}: ${read(id).text}`}>
    <text x={x} y={y} textAnchor="middle" fill="#cbd5e1" fontSize="12">{pointLabel(id)}</text>
    <text x={x} y={y + 18} textAnchor="middle" fill={read(id).condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">{read(id).shortText}</text>
  </g>;
  const indicators = (x: number, prefix: string) => {
    const earth = read(`${prefix}-earth`), fault = read(`${prefix}-fault-passage`);
    return <g>
      <g aria-label={`Earth switch: ${earth.text}`}><title>{`Earth switch: ${earth.text}`}</title>
        {earth.value === undefined || earth.value === null ? <text x={x} y="290" fill="#fbbf24" textAnchor="middle">?</text> : <path d={`M${x} 270 v8 m-10 14 h20 m-16 5 h12 m-9 5 h6 M${x} 278 l${earth.value === true ? "0 14" : "8 8"}`} stroke={earth.condition !== "good" || earth.value === true ? "#fbbf24" : "#cbd5e1"} strokeDasharray={earth.condition !== "good" ? "3 3" : undefined} fill="none" />}
      </g>
      <g aria-label={`Fault passage: ${fault.text}`}><title>{`Fault passage: ${fault.text}`}</title><circle cx={x + 35} cy="282" r="8" fill={fault.condition !== "good" ? "url(#cc-unknown)" : fault.value === true ? "#fbbf24" : "none"} stroke={fault.condition !== "good" || fault.value === true ? "#fbbf24" : "#cbd5e1"} /><text x={x + 48} y="286" fill="#cbd5e1" fontSize="12">FPI</text></g>
    </g>;
  };
  const switchSymbol = (x: number, y: number, label: string, id: string, device: DeviceId) => {
    const r = read(id);
    const known = r.value === "open" || r.value === "closed";
    return <g {...selectProps(device)} aria-label={`${label}: ${r.text}`}>
      {device.startsWith("RMU") && <g aria-label={`${label} assembly: switch function and earth`}>
        <rect x={x - 78} y={y - 40} width="156" height="164" rx="6" fill="none" stroke={r.condition === "good" ? "#64748b" : "#fbbf24"} strokeDasharray={r.condition === "good" ? undefined : "6 5"} />
        <path d={`M${x} ${y + 20} v20 h-30 v40`} stroke="#64748b" fill="none" />
      </g>}
      {device.startsWith("RMU") && indicators(x - 30, device === "RMU1" ? "rmu-1" : "rmu-2")}
      {selected === device && <rect x={x - 27} y={y - 27} width="54" height="54" rx="4" fill="none" stroke="#e2e8f0" />}
      <title>{`${label}: ${r.text}`}</title>
      <rect x={x - 20} y={y - 20} width="40" height="40" fill={r.condition !== "good" ? "url(#cc-unknown)" : r.value === "closed" ? "#cbd5e1" : "#0f172a"} stroke={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeWidth="2" strokeDasharray={r.condition !== "good" ? "5 3" : undefined} />
      {r.condition !== "good" && r.value === "closed" && <rect x={x - 10} y={y - 10} width="20" height="20" fill="#fbbf24" fillOpacity="0.6" />}
      {device.startsWith("RMU") && known && <path d={r.value === "closed" ? `M${x - 12} ${y} h24` : `M${x - 12} ${y + 10} l24 -20`} stroke={r.value === "closed" && r.condition === "good" ? "#0f172a" : "#cbd5e1"} strokeWidth="3" />}
      {!known && <text x={x} y={y + 5} textAnchor="middle" fill="#fbbf24">{r.value === "intermediate" ? "½" : r.value === "indeterminate" ? "×" : "?"}</text>}
      <text x={x} y={y - 25} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label}</text>
      <text x={x} y={y + 35} textAnchor="middle" fill={r.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">{r.shortText}</text>
    </g>;
  };
  const busLabel = (x: number, y: number, label: string, section: SectionId) => {
    const id = SECTION_VOLTAGE[section];
    const path = sectionSupplyPath(points, section);
    const pathText = `${path.state}${path.state === "path unknown" ? " · F2 unobserved" : path.f2ContributionUnobserved ? " †" : ""}`;
    return <g aria-label={`${label}: ${pointLabel(id)}: ${read(id).text}; supply ${pathText}: ${path.reason}`}>
      <title>{path.reason}</title>
      <text x={x} y={y} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label} · {voltage(id).state}</text>
      <text x={x} y={y + 18} textAnchor="middle" fill="#cbd5e1" fontSize="12">{pointLabel(id)}</text>
      <text x={x} y={y + 36} textAnchor="middle" fill={voltage(id).color} fontSize="12">{read(id).shortText}</text>
      <text x={x} y={y + 54} textAnchor="middle" fill={path.state === "unknown" || path.state === "path unknown" ? "#fbbf24" : "#cbd5e1"} fontSize="12" data-supply-path={path.state}>{pathText}</text>
    </g>;
  };
  const tap = read("oltc-1-tap");
  return <figure className="min-w-0 space-y-3">
    <div className="max-w-full overflow-x-auto">
      <svg viewBox="0 0 1060 410" role="group" aria-label="CC-observed CIGRE-derived teaching feeder · simplified functional one-line" className="w-full min-w-[1060px]">
        <defs><pattern id="cc-unknown" width="8" height="8" patternUnits="userSpaceOnUse"><path d="M0 8 L8 0" stroke="#fbbf24" strokeOpacity="0.5" /></pattern></defs>
        <text x="16" y="35" textAnchor="start" fill="#cbd5e1" fontSize="13">110 kV nominal — not telemetered</text>
        <line x1="80" y1="45" x2="80" y2="70" stroke="#94a3b8" strokeDasharray="6 5" />
        <g {...selectProps("T1")} aria-label={`T1 OLTC: ${tap.text}`}>
          {selected === "T1" && <rect x="52" y="65" width="55" height="76" rx="4" stroke="#e2e8f0" fill="none" />}
          <circle cx="80" cy="90" r="20" fill="none" stroke={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <circle cx="80" cy="113" r="20" fill="none" stroke={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <text x="125" y="88" fill="#e2e8f0" fontSize="13">T1 110/20 kV · OLTC</text>
          <text x="125" y="108" fill={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">Tap {tap.shortText}</text>
          <text x="125" y="128" fill="#cbd5e1" fontSize="12">{read("oltc-1-auto").shortText}</text>
        </g>
        {measure(175, 150, "oltc-1-current")}
        {measure(175, 267, "oltc-1-bus-kv")}
        {measure(310, 110, "feeder-current")}
        {measure(400, 260, "feeder-voltage")}
        {measure(570, 110, "rmu-1-current")}
        {measure(850, 110, "rmu-2-current")}
        {measure(925, 35, "rmu-2-section-kv")}
        {section(80, 133, 80, 325, "oltc-1-bus-kv")}
        <text x="310" y="145" textAnchor="middle" fill="#e2e8f0" fontSize="14">F1 feeder</text>
        {section(80, 190, 310, 190, "oltc-1-bus-kv")}
        {switchSymbol(310, 190, "CB1", "feeder-breaker-position", "CB1")}
        {section(330, 190, 570, 190, "rmu-1-source-kv")}
        {/* Section labels use the upper annotation lane, clear of both RMU enclosures. */}
        {busLabel(410, 25, "S1 residential", "S1")}
        {switchSymbol(570, 190, "RMU1 · NC", "rmu-1-position", "RMU1")}
        {section(590, 190, 850, 190, "rmu-1-section-kv")}
        {busLabel(700, 25, "S2 commercial", "S2")}
        {switchSymbol(850, 190, "RMU2 · NOP", "rmu-2-position", "RMU2")}
        {section(870, 190, 1000, 190, "rmu-2-source-kv")}
        {section(1000, 190, 1000, 325, "rmu-2-source-kv")}
        <line x1="80" y1="325" x2="305" y2="325" stroke="#94a3b8" strokeWidth="3" strokeDasharray="6 5" />
        <rect x="305" y="309" width="32" height="32" fill="#0f172a" stroke="#94a3b8" strokeDasharray="5 3" />
        <text x="321" y="331" textAnchor="middle" fill="#cbd5e1">?</text>
        <text x="321" y="365" textAnchor="middle" fill="#cbd5e1" fontSize="12">F2 head · unobserved</text>
        {section(337, 325, 1000, 325, "rmu-2-source-kv")}
        {busLabel(710, 348, "S3 industrial", "S3")}
      </svg>
    </div>
    <figcaption className="text-xs text-slate-400"><span className="font-semibold text-slate-300">Simplified functional one-line.</span> Select a device for its faceplate. Filled switch: closed; hollow: open; ½: intermediate; ×: indeterminate. Real RMUs contain several functional units; these enclosures show the switch function, earth blade and FPI. RMU2 is the NOP (normal open point). Earth blade and FPI show received indications. Blue: observed voltage &gt; 0 kV, not proof of a supply path. Grey: observed zero. Dashed amber: unknown or stale, never inferred from switch position. Supply paths use only valid received voltages and switch feedback. † F2 contribution unobserved. Tap TRANSIENT means motion; the displayed tap is the last completed step. F1 connects through CB1; the lower branch returns through the unobserved F2 head. S3 colour follows its received kV only, not F2 state. F2 has no telemetry.</figcaption>
  </figure>;
}
