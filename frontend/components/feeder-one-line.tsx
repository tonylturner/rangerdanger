"use client";

import { AUTHORITY_POINTS, deviceOffNormal, type DeviceId } from "../lib/operator-workstation";
import { pointLabel, qualityDefinition } from "../lib/operator-format";
import { SECTION_VOLTAGE, sectionSupplyPath, type SectionId } from "../lib/operator-supply-path";
import type { Readings } from "../lib/observation-readings";
import { LIVE_KV, voltageState, type VoltageState } from "../lib/electrical-state";

export function FeederOneLine({ read, physical = false, unavailable = false, selected = "T1", onSelect = () => {} }: {
  read: Readings; physical?: boolean; unavailable?: boolean; selected?: DeviceId; onSelect?: (id: DeviceId) => void;
}) {
  const qualityTitle = (id: string) => physical ? null : <title>{qualityDefinition(read(id).condition)}</title>;
  // Keep long reasons out of the narrow SVG annotation lanes. Full flags and
  // reasons remain visible in wrapping HTML below and in each accessible label.
  const drawingText = (id: string) => {
    const r = read(id);
    if (r.condition === "good") return r.shortText;
    if (physical && r.condition === "unknown") return "UNKNOWN · sim";
    const parts = r.shortText.split(" · ");
    const flag = (parts[r.transient ? 2 : 1] ?? "UNKNOWN").split(" (")[0];
    return `${parts[0]}${r.transient ? " · TRANSIENT" : ""} · ${flag}${physical ? " · sim" : ""}`;
  };
  const lineStyle = (state: VoltageState) => {
    return state === "UNKNOWN" ? { color: "#fbbf24", dash: "6 5", width: 3, state }
      : state === "LIVE" ? { color: "#38bdf8", dash: undefined, width: 3, state }
        : { color: "#fb7185", dash: "10 6", width: 5, state };
  };
  const voltage = (id: string) => lineStyle(voltageState(read(id)));
  const solverLine = (id: string) => {
    const r = read(id);
    return lineStyle(r.condition !== "good" || typeof r.value !== "boolean" ? "UNKNOWN" : r.value ? "LIVE" : "DEAD");
  };
  const position = (id: string) => {
    const value = read(id).value;
    return typeof value === "string" && ["closed", "open", "intermediate", "indeterminate"].includes(value) ? value : "unknown";
  };
  const section = (x1: number, y1: number, x2: number, y2: number, id: string) => {
    const v = voltage(id);
    return <line x1={x1} y1={y1} x2={x2} y2={y2} data-conductor={id} data-state={v.state} stroke={v.color} strokeWidth={v.width} strokeDasharray={v.dash} />;
  };
  const selectProps = (device: DeviceId) => ({
    role: "button", tabIndex: 0, "aria-pressed": selected === device,
    onClick: () => onSelect(device), onKeyDown: (e: React.KeyboardEvent<SVGGElement>) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(device); }
    }, className: "cursor-pointer outline-hidden focus-visible:[outline:2px_solid_#38bdf8]",
  });
  const measure = (x: number, y: number, id: string) => <g aria-label={`${pointLabel(id)}: ${read(id).text}`}>
    <text x={x} y={y} textAnchor="middle" fill="#cbd5e1" fontSize="12">{pointLabel(id)}</text>
    <text data-point={id} data-state={id.endsWith("-kv") || id === "feeder-voltage" ? voltage(id).state : undefined} x={x} y={y + 18} textAnchor="middle" fill={read(id).condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">{drawingText(id)}{qualityTitle(id)}</text>
  </g>;
  const authorityTag = (x: number, y: number, device: keyof typeof AUTHORITY_POINTS) => {
    const authority = AUTHORITY_POINTS[device];
    const r = read(authority.id);
    if (r.condition === "good" && r.value === true) return null;
    // A retained authority value cannot establish LOCAL or REMOTE.
    const state = r.condition === "good" && r.value === false ? "LOCAL" : r.condition === "stale" ? "STALE" : "UNKNOWN";
    const uncertain = state !== "LOCAL";
    const width = uncertain ? 164 : 60;
    const detail = [`${authority.label}: ${state}`, ...r.flags, r.staleReason, unavailable ? "cached snapshot · CC API unavailable" : null].filter(Boolean).join(" · ");
    return <g data-authority={authority.id} data-off-normal={state === "LOCAL" ? "true" : "false"} aria-label={detail}>
      <title>{detail}</title>
      <rect x={x - width / 2} y={y} width={width} height="20" rx="3" fill={uncertain ? "url(#cc-unknown)" : "#451a03"} stroke="#fbbf24" strokeDasharray={uncertain ? "5 3" : undefined} />
      <text x={x} y={y + 14} textAnchor="middle" fill="#fbbf24" fontSize="12">{uncertain ? `AUTHORITY ${state}` : state}{qualityTitle(authority.id)}</text>
    </g>;
  };
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
    const offNormal = deviceOffNormal(read, device);
    const color = r.condition !== "good" || offNormal ? "#fbbf24" : "#cbd5e1";
    const known = r.value === "open" || r.value === "closed";
    return <g {...selectProps(device)} data-device={device} data-off-normal={offNormal ? "true" : "false"} data-position={position(id)} data-quality={r.condition} aria-label={`${label}: ${r.text}`}>
      {device.startsWith("RMU") && <g aria-label={`${label} assembly: switch function and earth`}>
        <rect x={x - 78} y={y - 40} width="156" height="164" rx="6" fill="none" stroke={r.condition !== "good" || offNormal ? "#fbbf24" : "#64748b"} strokeDasharray={r.condition === "good" ? undefined : "6 5"} />
        <path d={`M${x} ${y + 20} v20 h-30 v40`} stroke="#64748b" fill="none" />
      </g>}
      {device.startsWith("RMU") && indicators(x - 30, device === "RMU1" ? "rmu-1" : "rmu-2")}
      {(device === "RMU1" || device === "RMU2") && authorityTag(x, y - 63, device)}
      {selected === device && <rect x={x - 27} y={y - 27} width="54" height="54" rx="4" fill="none" stroke="#e2e8f0" />}
      <title>{`${label}: ${r.text}`}</title>
      <rect x={x - 20} y={y - 20} width="40" height="40" fill={r.condition !== "good" ? "url(#cc-unknown)" : r.value === "closed" ? "#cbd5e1" : "#0f172a"} stroke={color} strokeWidth="2" strokeDasharray={r.condition !== "good" ? "5 3" : undefined} />
      {r.condition !== "good" && r.value === "closed" && <rect x={x - 10} y={y - 10} width="20" height="20" fill="#fbbf24" fillOpacity="0.6" />}
      {device.startsWith("RMU") && known && <path d={r.value === "closed" ? `M${x - 12} ${y} h24` : `M${x - 12} ${y + 10} l24 -20`} stroke={r.value === "closed" && r.condition === "good" ? "#0f172a" : "#cbd5e1"} strokeWidth="3" />}
      {!known && <text x={x} y={y + 5} textAnchor="middle" fill="#fbbf24">{r.value === "intermediate" ? "½" : r.value === "indeterminate" ? "×" : "?"}</text>}
      <text x={x} y={y - 25} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label}</text>
      <text x={x} y={y + 35} textAnchor="middle" fill={color} fontSize="12">{drawingText(id)}{qualityTitle(id)}</text>
    </g>;
  };
  const busLabel = (x: number, y: number, label: string, section: SectionId) => {
    const id = SECTION_VOLTAGE[section];
    const path = sectionSupplyPath(read, section);
    const pathText = path.state;
    const kv = read(id).value;
    return <g data-section={section} data-state={voltage(id).state} data-path={path.state} data-kv={typeof kv === "number" && Number.isFinite(kv) ? kv : ""} aria-label={`${label}: ${pointLabel(id)}: ${read(id).text}; supply ${pathText}: ${path.reason}`}>
      <title>{path.reason}</title>
      <text x={x} y={y} textAnchor="middle" fill="#e2e8f0" fontSize="13">{label} · {voltage(id).state}</text>
      <text x={x} y={y + 18} textAnchor="middle" fill="#cbd5e1" fontSize="12">{pointLabel(id)}</text>
      <text data-point={id} data-state={voltage(id).state} x={x} y={y + 36} textAnchor="middle" fill={voltage(id).color} fontSize="12">{drawingText(id)}{qualityTitle(id)}</text>
      <text x={x} y={y + 54} textAnchor="middle" fill={path.state === "unknown" || path.state === "path unknown" ? "#fbbf24" : "#cbd5e1"} fontSize="12" data-supply-path={path.state}>{pathText}</text>
    </g>;
  };
  const tap = read("oltc-1-tap");
  const f2 = read("f2-position");
  const f2Known = f2.condition === "good";
  const f2OffNormal = deviceOffNormal(read, "F2");
  const t1OffNormal = deviceOffNormal(read, "T1");
  const t1Color = t1OffNormal || tap.condition !== "good" ? "#fbbf24" : "#cbd5e1";
  const energization = read("s3-live");
  const unobserved = { color: "#94a3b8", dash: "6 5", state: "UNKNOWN" };
  const upstreamRun = physical ? solverLine("bus20-live") : voltage("oltc-1-bus-kv");
  const lowerRun = physical ? solverLine("s3-live") : voltage("rmu-2-source-kv");
  const provenance = "Simulation-observed · independent of IEC 104";
  const qualityNotes = [
    "feeder-breaker-position", "feeder-current", "feeder-voltage", "feeder-protection-trip", "feeder-lockout",
    "f2-position", "f2-protection-trip", "f2-lockout", "f2-current", "f2-p-mw",
    ...["rmu-1", "rmu-2"].flatMap((prefix) => ["position", "remote", "current", "source-kv", "section-kv", "earth", "fault-passage"].map((suffix) => `${prefix}-${suffix}`)),
    "oltc-1-tap", "oltc-1-auto", "oltc-1-remote", "oltc-1-current", "oltc-1-bus-kv", "oltc-1-blocked", "oltc-1-overcurrent-block",
    ...(!physical ? ["oltc-1-at-lower", "oltc-1-at-upper", "plc-1-running", "plc-1-input-valid"] : []),
  ].filter((id) => read(id).condition !== "good");
  const asserted = (id: string, value = true) => read(id).condition === "good" && read(id).value === value;
  const badge = (id: string, label: string, rose = false, source = id) =>
    <span key={id} data-point={id} data-state={label} data-condition="asserted"
      title={`${pointLabel(source)}: ${read(source).text}`}
      aria-label={`${pointLabel(source)}: ${label} · ${physical ? "sim" : "IEC 104 observation"}`}
      className={`whitespace-nowrap rounded-sm border px-2 py-0.5 text-xs font-bold ${rose ? "border-rose-400 bg-rose-950 text-rose-200" : "border-amber-400 bg-amber-950 text-amber-200"}`}>{label}</span>;
  const breakerBadges = (prefix: string) => <>
    {asserted(`${prefix}-protection-trip`) && badge(`${prefix}-protection-trip`, "TRIPPED", true)}
    {asserted(`${prefix}-lockout`) && badge(`${prefix}-lockout`, "LOCKED OUT", true)}
  </>;
  // HTML labels retain their type size as the drawing grows; coordinates follow
  // the same canvas, inside its bounded horizontal scroller.
  const at = (x: number, y: number) => ({ left: `${x / 1060 * 100}%`, top: `${y / 460 * 100}%` });
  const cluster = "absolute flex -translate-x-1/2 flex-col items-center gap-1";
  return <figure className="min-w-0 space-y-3">
    {physical && <p className="text-xs text-slate-300">{provenance}</p>}
    <div className="max-w-full overflow-x-auto">
      <div className="relative min-w-[1060px]">
      <svg viewBox="0 0 1060 460" role="group" aria-label={`${physical ? "Simulation-observed · independent of IEC 104" : "CC-observed"} CIGRE-derived teaching feeder · simplified functional one-line`} className="w-full min-w-[1060px]">
        <defs><pattern id="cc-unknown" width="8" height="8" patternUnits="userSpaceOnUse"><path d="M0 8 L8 0" stroke="#fbbf24" strokeOpacity="0.5" /></pattern></defs>
        <text x="16" y="35" textAnchor="start" fill="#cbd5e1" fontSize="13">{physical ? "110 kV nominal · sim source" : "110 kV nominal — not telemetered"}</text>
        <line x1="80" y1="45" x2="80" y2="70" stroke={physical ? upstreamRun.color : unobserved.color} strokeWidth={physical ? upstreamRun.width : 3} strokeDasharray={physical ? upstreamRun.dash : unobserved.dash} />
        <g {...selectProps("T1")} data-device="T1" data-off-normal={t1OffNormal ? "true" : "false"} data-tap={typeof tap.value === "object" && tap.value !== null ? tap.value.value : ""} data-quality={tap.condition} aria-label={`T1 OLTC: ${tap.text}`}>
          {selected === "T1" && <rect x="52" y="65" width="55" height="76" rx="4" stroke="#e2e8f0" fill="none" />}
          <circle cx="80" cy="90" r="20" fill="none" stroke={t1Color} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <circle cx="80" cy="113" r="20" fill="none" stroke={t1Color} strokeDasharray={tap.condition === "good" ? undefined : "5 3"} />
          <text x="125" y="88" fill="#e2e8f0" fontSize="13">T1 110/20 kV · OLTC</text>
          <text x="125" y="108" fill={tap.condition === "good" ? "#cbd5e1" : "#fbbf24"} fontSize="12">Tap {drawingText("oltc-1-tap")}{qualityTitle("oltc-1-tap")}</text>
          <text x="125" y="128" fill={t1OffNormal || read("oltc-1-auto").condition !== "good" ? "#fbbf24" : "#cbd5e1"} fontSize="12">{drawingText("oltc-1-auto")}{qualityTitle("oltc-1-auto")}</text>
          {authorityTag(179, 143, "T1")}
        </g>
        {measure(175, 245, "oltc-1-current")}
        {measure(175, 285, "oltc-1-bus-kv")}
        {measure(310, 110, "feeder-current")}
        {measure(400, 260, "feeder-voltage")}
        {measure(570, 95, "rmu-1-current")}
        {measure(850, 95, "rmu-2-current")}
        {measure(925, 35, "rmu-2-section-kv")}
        {section(80, 133, 80, 325, "oltc-1-bus-kv")}
        <text x="220" y="180" textAnchor="middle" fill="#e2e8f0" fontSize="14">F1 feeder</text>
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
        <line x1="80" y1="325" x2="305" y2="325" stroke={upstreamRun.color} strokeWidth={upstreamRun.width} strokeDasharray={upstreamRun.dash} />
        <g {...selectProps("F2")} aria-label={`F2 indication-only breaker: ${f2.text}`}>
          <title>{`F2 indication-only breaker: ${f2.text}`}</title>
          {selected === "F2" && <rect x="298" y="302" width="46" height="46" rx="4" fill="none" stroke="#e2e8f0" />}
          <rect data-device="F2" data-off-normal={f2OffNormal ? "true" : "false"} data-position={position("f2-position")} data-quality={f2.condition} x="305" y="309" width="32" height="32" fill={!f2Known ? "url(#cc-unknown)" : f2.value === "closed" ? "#cbd5e1" : "#0f172a"} stroke={!f2Known || f2OffNormal ? "#fbbf24" : "#cbd5e1"} strokeWidth="2" strokeDasharray={f2Known ? undefined : "5 3"} />
          <text x="321" y="331" textAnchor="middle" fill={f2Known && f2.value === "closed" ? "#0f172a" : "#cbd5e1"}>{f2.value === "closed" ? "●" : f2.value === "open" ? "○" : "?"}</text>
          <text x="321" y="365" textAnchor="middle" fill={!f2Known || f2OffNormal ? "#fbbf24" : "#cbd5e1"} fontSize="12">F2 · {drawingText("f2-position")}{qualityTitle("f2-position")}</text>
        </g>
        <line x1="337" y1="325" x2="1000" y2="325" data-f2-line="" data-state={lowerRun.state} stroke={lowerRun.color} strokeWidth={lowerRun.width} strokeDasharray={lowerRun.dash} aria-label={physical ? `F2 connection · solver energization: ${energization.text}` : `F2 connection · S3 received VT, not proof of path: ${read("rmu-2-source-kv").text}`} />
        {busLabel(710, 348, "S3 industrial", "S3")}
      </svg>
      {(["S1", "S2", "S3"] as const).map((section, i) => voltage(SECTION_VOLTAGE[section]).state === "DEAD" &&
        <div key={section} className={cluster} style={at([410, 700, 710][i], i === 2 ? 414 : 89)}>
          {badge(`${section}-no-supply`, "NO SUPPLY", true, SECTION_VOLTAGE[section])}
        </div>)}
      {/* One row below the VT value and above the F2 conductor: two latches
          must not grow down into the line-side VT annotation. */}
      <div data-breaker-badges="CB1" className="absolute flex h-6 w-[210px] -translate-x-1/2 flex-row items-center justify-center gap-1" style={at(400, 292)}>{breakerBadges("feeder")}</div>
      <div data-breaker-badges="F2" className={cluster} style={at(440, 340)}>{breakerBadges("f2")}</div>
      {(["rmu-1", "rmu-2"] as const).map((prefix, i) => <div key={prefix} className={cluster} style={at(i === 0 ? 690 : 925, 235)}>
        {asserted(`${prefix}-fault-passage`) && badge(`${prefix}-fault-passage`, "FAULT PASSED")}
        {asserted(`${prefix}-earth`) && badge(`${prefix}-earth`, "EARTHED")}
      </div>)}
      <div className={cluster} style={at(180, 166)}>
        {["oltc-1-at-lower", "oltc-1-at-upper"].filter((id) => asserted(id)).slice(0, 1).map((id) => badge(id, "AT LIMIT"))}
        {["oltc-1-blocked", "oltc-1-overcurrent-block"].filter((id) => asserted(id)).slice(0, 1).map((id) => badge(id, "BLOCKED"))}
        {["plc-1-running", "plc-1-input-valid"].filter((id) => asserted(id, false)).slice(0, 1).map((id) => badge(id, "PLC DOWN"))}
      </div>
      </div>
    </div>
    <p className="text-xs text-slate-300">F2 · {physical ? "sim" : "IEC 104"} · indication only: I <span title={physical ? undefined : qualityDefinition(read("f2-current").condition)}>{read("f2-current").shortText}</span> · P <span title={physical ? undefined : qualityDefinition(read("f2-p-mw").condition)}>{read("f2-p-mw").shortText}</span>. Alternative feeder from the same T1 / bus20.</p>
    {qualityNotes.length > 0 && <div aria-label="One-line quality exceptions" className="space-y-1 text-xs text-amber-200">
      <p>Observation quality exceptions · separate from plant state:</p>
      <ul className="grid gap-x-6 gap-y-1 sm:grid-cols-2">{qualityNotes.map((id) => <li key={id} className="min-w-0 wrap-anywhere">{pointLabel(id)}: {read(id).shortText}</li>)}</ul>
    </div>}
    <figcaption className="text-xs text-slate-400">Simplified functional one-line. Blue: voltage ≥ {LIVE_KV} kV, not proof of a supply path. Thick rose dashes + NO SUPPLY: valid voltage below {LIVE_KV} kV. TRIPPED / LOCKED OUT: protection latches. FAULT PASSED: latched FPI, not a fault location. EARTHED: earth closed. AT LIMIT / BLOCKED / PLC DOWN: control constraints. Amber: off-normal; dashed amber / hatching: uncertain data, never inferred loss of supply. RMU2 OPEN is normal (NOP); F2 is another connection to the same T1 bus. Filled switch: closed; hollow: open; ½ / ×: intermediate / indeterminate. Tap is the last completed step; TRANSIENT means motion. {physical ? "Simulation observations only; the lower F2 run follows solver energization separately from measured kV. Select a device for its equipment card." : "IEC 104 observations only; paths use valid received voltages and switch feedback. The lower F2 run follows S3 VT, not F2 position. Select a device for its faceplate."}</figcaption>
  </figure>;
}
