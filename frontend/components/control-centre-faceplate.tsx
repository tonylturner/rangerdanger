"use client";

import type { OperatorState } from "../lib/operator-api";
import { formatPointValue, pointCondition, pointLabel, qualityFlags } from "../lib/operator-format";
import { devicePoints, regulationBand, reportingText, type DeviceId, type VoltageBand } from "../lib/operator-workstation";
import { SupervisoryControls } from "./control-centre-command";
import { ConditionBadge, STALE_HATCH } from "./control-centre-points";

export function RegulationBand({ band }: { band: VoltageBand | null }) {
  if (!band) return <p className="text-xs text-amber-300">Current regulation band unavailable: setpoint or deadband is not GOOD.</p>;
  const span = Math.max(band.high - band.low, 0.2);
  const min = Math.min(band.low - span / 2, band.bus ?? band.low), max = Math.max(band.high + span / 2, band.bus ?? band.high);
  const x = (n: number) => 20 + (n - min) / (max - min) * 260;
  return <figure>
    <figcaption className="text-xs text-slate-300">Current band: {band.setpoint.toFixed(2)} ± {((band.high - band.low) / 2).toFixed(2)} kV</figcaption>
    <svg viewBox="0 0 300 48" preserveAspectRatio="none" className="h-12 w-full" role="img" aria-label={`Current band ${band.low.toFixed(2)} to ${band.high.toFixed(2)} kV; ${pointLabel("oltc-1-bus-kv")} ${band.bus ?? "unknown"}`}>
      <path d="M20 25 H280" stroke="#64748b" vectorEffect="non-scaling-stroke" />
      <rect x={x(band.low)} y="16" width={Math.max(1, x(band.high) - x(band.low))} height="18" fill="#475569" />
      <path d={`M${x(band.setpoint)} 12 V38`} stroke="#e2e8f0" vectorEffect="non-scaling-stroke" />
      {band.bus !== null && <path d={`M${x(band.bus)} 8 l-5 -7 h10 Z M${x(band.bus)} 8 V40`} fill="#38bdf8" stroke="#38bdf8" vectorEffect="non-scaling-stroke" />}
    </svg>
    <div className="flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-slate-300">
      <span>{band.low.toFixed(2)}–{band.high.toFixed(2)} kV</span>
      <span>Bus {band.bus?.toFixed(2) ?? "unknown"} kV</span>
    </div>
  </figure>;
}

export function DeviceFaceplate({ state, selected, unavailable }: { state: OperatorState; selected: DeviceId; unavailable: boolean }) {
  return <aside aria-label={`${selected} faceplate`} className="min-w-0 space-y-4 rounded-lg border border-slate-700 bg-slate-950 p-4">
    <h2 className="text-base font-semibold text-slate-100">{selected} · {selected === "T1" ? "Transformer / AVR" : selected === "CB1" || selected === "F2" ? "Feeder breaker" : "Ring main unit"}</h2>
    {selected === "F2" && <p className="text-xs text-slate-300">Indication only · no supervisory controls. F2 is an alternative feeder from the same T1 / bus20.</p>}
    {selected === "CB1" && <p className="text-xs text-slate-400">Remote/local authority is not telemetered for CB1.</p>}
    <SupervisoryControls link={state.link} points={state.points} unavailable={unavailable} selected={selected} />
    {selected === "T1" && <><RegulationBand band={regulationBand(state.points)} /><p className="text-xs text-slate-400">Configured delay only; no live countdown is telemetered.</p></>}
    <dl className="text-xs">
      {devicePoints(state.points, selected).map((p) => <div key={p.id} className={`grid gap-x-4 gap-y-1 border-t border-slate-800 py-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] ${pointCondition(p) !== "good" ? `border-dashed ${STALE_HATCH}` : ""}`}>
        <dt className="text-slate-400">{pointLabel(p.id)}</dt>
        <dd className="flex min-w-0 flex-wrap items-center gap-2 text-slate-100"><span className="font-mono">{formatPointValue(p.id, p.value, p.quality)}</span><ConditionBadge condition={pointCondition(p)} /><span className="break-words">{qualityFlags(p.quality).join(" ")} {p.stale_reason}</span></dd>
        <dd className="text-slate-400 sm:col-start-2">{reportingText(p)}</dd>
      </div>)}
    </dl>
  </aside>;
}
