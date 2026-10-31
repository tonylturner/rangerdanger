import type { FieldState, OperatorState } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { agreementText, scadaQualityText, type ComparisonCounts } from "../lib/physical-comparison";
import { ACTION, num, position } from "./physical-system-parts";

export function PhysicalOverview({ field, operator, unavailable, fieldUnavailable, counts, onDevice, onSwitching, onComparison }: {
  field: FieldState; operator: OperatorState | undefined; unavailable: boolean; fieldUnavailable: boolean; counts: ComparisonCounts; onDevice: (id: DeviceId) => void; onSwitching: () => void; onComparison: () => void;
}) {
  const cb1 = field.devices["breaker-1"], rmu1 = field.devices["rmu-1"], rmu2 = field.devices["rmu-2"], t1 = field.devices["oltc-1"];
  const oltc = t1?.kind === "oltc" ? t1 : undefined, m = oltc?.measurements;
  const band = operator?.points.find((p) => p.id === "oltc-1-bus-kv")?.limits;
  const freshness = fieldUnavailable ? "STALE · field API unavailable" : field.process.truth_stale ? "STALE · device input" : m && !m.valid ? "INVALID · retained measurements" : "Simulation measurements";
  const tiles = [
    { label: "Feeder breaker", value: position(cb1), detail: cb1?.kind === "breaker" ? `${cb1.protection_tripped ? "Protection TRIPPED" : cb1.protection.picked_up ? "Protection picked up" : "Protection normal"}${cb1.lockout ? " · LOCKOUT" : ""}` : "Protection unknown", action: () => onDevice("CB1") },
    { label: "Switching", value: `RMU1 ${position(rmu1)} · RMU2 ${position(rmu2)}`, detail: rmu2 && "position" in rmu2 ? rmu2.position === "open" ? "RMU2 · NOP" : "RMU2 · off-normal" : "RMU2 · unknown", action: onSwitching },
    { label: "T1/OLTC", value: oltc ? `Completed tap ${oltc.tap}` : "Completed tap unknown", detail: oltc ? `${oltc.auto ? "AUTO" : "MANUAL"} · ${oltc.moving ? "moving" : "stationary"}${oltc.tap !== oltc.target_tap ? ` · target ${oltc.target_tap}` : ""}` : "Mode unknown", action: () => onDevice("T1") },
    { label: "Bus voltage", value: num(m?.voltage_kv_ll, 2, "kV"), detail: `20 kV nominal${band ? ` · CC alarm limits ${band.low.toFixed(1)}–${band.high.toFixed(1)} kV` : ""} · 110 kV source measured ${num(m?.source_kv_ll, 2, "kV")} · ${freshness}`, action: () => onDevice("T1") },
    { label: "T1 load", value: num(m?.p_mw, 2, "MW"), detail: `I ${num(m?.current_a, 1, "A")} · PF ${num(m?.pf, 3)} · ${freshness}`, action: () => onDevice("T1") },
  ];
  return <section aria-label="System overview" className="space-y-2">
    <h2 className="text-sm font-semibold text-slate-200">System overview</h2>
    <div className="grid min-w-0 gap-2 sm:grid-cols-2 xl:grid-cols-3">
      {tiles.map((tile) => <button type="button" key={tile.label} className={`${ACTION} min-w-0 space-y-1 bg-slate-950 text-left wrap-anywhere`} onClick={tile.action}>
        <h3 className="text-xs text-slate-400">{tile.label}</h3><p className="font-mono text-sm text-slate-100">{tile.value}</p><p className="text-xs text-slate-300">{tile.detail}</p>
      </button>)}
      <button type="button" className={`${ACTION} min-w-0 space-y-1 bg-slate-950 text-left wrap-anywhere`} onClick={onComparison}>
        <h3 className="text-xs text-slate-400">SCADA agreement</h3>
        <p className="font-mono text-xs text-slate-300">SCADA data quality: {scadaQualityText(operator, unavailable)}</p>
        <p className={`font-mono text-xs ${counts.MISMATCH ? "text-red-300" : "text-slate-300"}`}>Physical agreement: {agreementText(counts)}</p>
      </button>
    </div>
  </section>;
}
