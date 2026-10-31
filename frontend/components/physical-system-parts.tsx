import type { ReactNode } from "react";
import type { DeviceState } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";

export const EQUIPMENT: { id: DeviceId; key: string; title: string }[] = [
  { id: "CB1", key: "breaker-1", title: "CB1 — Feeder breaker" },
  { id: "F2", key: "f2-breaker", title: "F2 — Alternative feeder · indication only" },
  { id: "T1", key: "oltc-1", title: "T1 — Transformer / OLTC" },
  { id: "RMU1", key: "rmu-1", title: "RMU1" },
  { id: "RMU2", key: "rmu-2", title: "RMU2 · NOP" },
];
export const PANEL = "min-w-0 rounded-lg border bg-slate-950 p-3";
export const ACTION = "rounded-sm border border-slate-600 px-3 py-2 text-xs text-slate-100 hover:bg-slate-800 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-sky-400";
export const yesNo = (value: boolean | null | undefined) => value === null || value === undefined ? "unknown" : value ? "YES" : "no";
export const num = (value: number | null | undefined, digits: number, unit = "") => value === null || value === undefined ? "unknown" : `${value.toFixed(digits)}${unit ? ` ${unit}` : ""}`;
export const position = (device: DeviceState | undefined) => device && "position" in device ? device.position.toUpperCase() : "UNKNOWN";
export function Row({ label, children, alert }: { label: string; children: ReactNode; alert?: boolean }) {
  return <div className="flex items-baseline justify-between gap-3 border-t border-slate-800/50 py-1 text-xs">
    <span className="min-w-0 text-slate-400">{label}</span>
    <span className={`min-w-0 text-right font-mono wrap-anywhere ${alert ? "text-amber-300" : "text-slate-200"}`}>{children}</span>
  </div>;
}
export function Group({ title, children }: { title: string; children: ReactNode }) {
  return <section className="min-w-0 space-y-1"><h4 className="mt-3 text-xs font-semibold text-slate-200">{title}</h4>{children}</section>;
}
