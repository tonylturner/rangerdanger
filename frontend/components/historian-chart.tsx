import { historyRuns, numericSample, type History } from "../lib/historian-api";
import { formatPointValue } from "../lib/operator-format";
import type { VoltageBand } from "../lib/operator-workstation";

export function minuteTicks(xmin: number, xmax: number): number[] {
  if (!Number.isFinite(xmin) || !Number.isFinite(xmax) || xmax < xmin) return [];
  return Array.from({ length: Math.floor(xmax / 60_000) - Math.ceil(xmin / 60_000) + 1 }, (_, i) => (Math.ceil(xmin / 60_000) + i) * 60_000);
}

export function historyScale(history: History, band?: VoltageBand | null) {
  const runs = historyRuns(history);
  const values = runs.flat().map((sample) => numericSample(sample)!);
  const bounds = [...values, ...(band ? [band.low, band.high] : [])];
  const min = bounds.length ? Math.min(...bounds) : 0;
  const max = bounds.length ? Math.max(...bounds) : 1;
  // A live strip never inherits another section's zero. A 1 kV minimum
  // span makes a 0.4 kV tap step visible without magnifying tiny noise.
  const span = Math.max(1, max - min);
  const mid = (max + min) / 2;
  return { runs, values, min: mid - span / 2, mid, max: mid + span / 2 };
}

type Scale = ReturnType<typeof historyScale>;
export function StripAxis({ scale, label, compact = false }: { scale: Scale; label?: string; compact?: boolean }) {
  return <div className={`relative ${compact ? "h-[96px]" : "h-[150px]"} text-right text-xs text-slate-300`}>
    {label && <strong className="absolute left-0 top-0 text-slate-100">{label}</strong>}
    {[scale.max, scale.mid, scale.min].map((value, i) => <span key={i} data-y-label style={{ top: `${10 + i * 40}%` }} className="absolute right-0 -translate-y-1/2">{value.toFixed(2)}</span>)}
  </div>;
}

export function MinuteAxis({ xmin, xmax }: { xmin: number; xmax: number }) {
  return <div aria-label="Time in UTC" className="@container relative col-start-2 h-6 min-w-0 text-xs text-slate-300">
    {minuteTicks(xmin, xmax).map((at) => {
      const percent = (at - xmin) / (xmax - xmin || 1) * 100;
      const minute = at / 60_000;
      // At 320px only five-minute labels fit; the minute grid never changes.
      const visibility = minute % 15 === 0 ? "block" : minute % 5 === 0 ? "block @min-[28rem]:hidden @min-[60rem]:block" : minute % 3 === 0 ? "hidden @min-[28rem]:block" : "hidden @min-[60rem]:block";
      return <time key={at} dateTime={new Date(at).toISOString()} style={{ left: `${percent}%`, transform: `translateX(-${percent}%)` }} className={`absolute top-1 whitespace-nowrap ${visibility}`}>{new Date(at).toISOString().slice(11, 16)}</time>;
    })}
  </div>;
}

export function StripPlot({ history, scale, xmin, xmax, label, color = "#38bdf8", band, compact = false }: {
  history: History; scale: Scale; xmin: number; xmax: number; label: string; color?: string; band?: VoltageBand | null; compact?: boolean;
}) {
  const x = (at: string | number) => ((typeof at === "number" ? at : Date.parse(at)) - xmin) / (xmax - xmin || 1) * 840;
  const y = (value: number) => 135 - (value - scale.min) / (scale.max - scale.min) * 120;
  return <svg viewBox="0 0 840 150" preserveAspectRatio="none" role="img" aria-label={label} className={`${compact ? "h-[96px]" : "h-[150px]"} w-full overflow-hidden`}>
    {band && <rect x="0" y={y(band.high)} width="840" height={Math.max(1, y(band.low) - y(band.high))} fill="#64748b" fillOpacity="0.3"><title>Current PLC band, not historical setpoints</title></rect>}
    {minuteTicks(xmin, xmax).map((at) => <line key={at} data-minute-grid x1={x(at)} x2={x(at)} y1="0" y2="150" stroke="#334155" vectorEffect="non-scaling-stroke" />)}
    {[scale.min, scale.mid, scale.max].map((value) => <line key={value} data-y-grid x1="0" x2="840" y1={y(value)} y2={y(value)} stroke="#475569" vectorEffect="non-scaling-stroke" />)}
    {history.gaps.map((gap, i) => <rect key={i} x={Math.max(0, x(gap.from))} y="0" width={Math.max(0, Math.min(840, x(gap.to)) - Math.max(0, x(gap.from)))} height="150" fill="#fbbf24" fillOpacity="0.15"><title>{`Ingest gap ${gap.from} to ${gap.to}`}</title></rect>)}
    {scale.runs.map((run, i) => <g key={i} data-history-run={history.point}>
      <polyline points={run.map((sample) => `${x(sample.ingested_at)},${y(numericSample(sample)!)}`).join(" ")} fill="none" stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke" />
      {run.map((sample) => <circle key={sample.snapshot_seq} cx={x(sample.ingested_at)} cy={y(numericSample(sample)!)} r="1" fill={color} stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke"><title>{`${sample.ingested_at}: ${formatPointValue(history.point, sample.value, sample.quality)}`}</title></circle>)}
    </g>)}
  </svg>;
}
