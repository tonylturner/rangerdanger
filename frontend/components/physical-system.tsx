"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import type { FieldState, OperatorState } from "../lib/operator-api";
import { physicalAbnormalities } from "../lib/physical-abnormalities";
import { physicalComparison, comparisonCounts } from "../lib/physical-comparison";
import { physicalSystemObservations } from "../lib/observation-readings";
import { useFieldState } from "../lib/operator-queries";
import type { DeviceId } from "../lib/operator-workstation";
import { sectionSupplyPath, SECTION_VOLTAGE, type SectionId } from "../lib/operator-supply-path";
import { isRangeNotReady } from "../lib/range";
import { errorMessage } from "../lib/utils";
import { FeederOneLine } from "./feeder-one-line";
import { PhysicalAudit } from "./physical-system-audit";
import { PhysicalComparison } from "./physical-system-comparison";
import { PhysicalDiagnostics } from "./physical-system-diagnostics";
import { PhysicalEquipment } from "./physical-system-equipment";
import { PhysicalOverview } from "./physical-system-overview";
import { ACTION, PANEL } from "./physical-system-parts";

export const PHYSICAL_DESCRIPTION = "Live electrical and device conditions from the simulation, independent of SCADA communications. Compare these values with the Control Centre to identify missing, delayed, or incorrect observations.";

type Selection = { selected?: DeviceId; onSelect?: (id: DeviceId) => void; revealSelected?: boolean };
export function PhysicalSystem({ operator, operatorUnavailable = false, now, ...selection }: Selection & { operator: OperatorState | undefined; operatorUnavailable?: boolean; now: number }) {
  const state = useFieldState(2000);
  return <PhysicalSystemScreen field={state.data} stateError={state.error} operator={operator} operatorUnavailable={operatorUnavailable} now={now} audit={<PhysicalAudit />} {...selection} />;
}
export function PhysicalSystemScreen({ field, stateError, operator, operatorUnavailable = false, now, selected = "T1", onSelect = () => {}, revealSelected = false, audit }: Selection & {
  field: FieldState | undefined; stateError: unknown; operator: OperatorState | undefined; operatorUnavailable?: boolean; now: number; audit?: ReactNode;
}) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const root = useRef<HTMLElement>(null);
  const available = !!field;
  // Depend on selection and availability, never the polled snapshot identity.
  useEffect(() => {
    if (!available || !revealSelected) return;
    setExpanded((old) => ({ ...old, [selected]: true }));
    root.current?.querySelector(`#physical-equipment-${selected}`)?.scrollIntoView({ block: "nearest" });
  }, [selected, revealSelected, available]);
  const toggle = (id: string) => setExpanded((old) => ({ ...old, [id]: !old[id] }));
  const showDevice = (id: DeviceId) => {
    onSelect(id);
    setExpanded((old) => ({ ...old, [id]: true }));
    root.current?.querySelector(`#physical-equipment-${id}`)?.scrollIntoView({ block: "nearest" });
  };
  const unavailable = stateError !== null && stateError !== undefined;
  const read = field ? physicalSystemObservations(field, unavailable) : null;
  const abnormalities = field ? physicalAbnormalities(field) : [];
  const rows = field ? physicalComparison(field, operator, operatorUnavailable, unavailable) : [];
  return <section ref={root} aria-label="Physical System state" className="min-w-0 space-y-4">
    {unavailable && !isRangeNotReady(stateError) && <p role="alert" className="text-sm text-amber-300">Physical system unavailable: {errorMessage(stateError)}{field ? " Showing retained observations, not current conditions." : ""}</p>}
    {!field || !read ? <p className="text-sm text-slate-400">{isRangeNotReady(stateError) ? "Physical system state shows once the range is ready." : "Waiting for physical system state…"}</p> : <>
      <section aria-label="Physical abnormalities" className={`${PANEL} border-slate-700 text-xs`}>
        {(field.process.truth_stale || unavailable) && <p className="mb-2 text-amber-300">⚠ Retained simulation observations · not current conditions</p>}
        {abnormalities.length ? <ul className="flex flex-wrap gap-x-4 gap-y-2">{abnormalities.map((a) => <li key={a.id} className={a.tone === "fault" ? "text-red-300" : a.tone === "warning" ? "text-amber-300" : "text-slate-300"}>
          {a.device ? <button type="button" className="rounded text-left hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400" onClick={() => showDevice(a.device!)}>{a.tone === "info" ? "ⓘ" : "⚠"} {a.text}</button> : <>{a.tone === "info" ? "ⓘ" : "⚠"} {a.text}</>}
        </li>)}</ul> : <p className="text-slate-400">No active physical abnormalities</p>}
      </section>
      <PhysicalOverview field={field} operator={operator} unavailable={operatorUnavailable} fieldUnavailable={unavailable} counts={comparisonCounts(rows)} onDevice={showDevice} onSwitching={() => { showDevice("RMU1"); setExpanded((old) => ({ ...old, RMU2: true })); }} onComparison={() => {
        setExpanded((old) => ({ ...old, comparison: true }));
        root.current?.querySelector("#physical-comparison")?.scrollIntoView({ block: "start" });
      }} />
      <PhysicalComparison rows={rows} now={now} allOpen={!!expanded.comparison} onAllToggle={() => toggle("comparison")} />
      <section aria-label="Electrical network overview" className={`${PANEL} border-slate-700`}>
        <h2 className="mb-3 text-sm font-semibold text-slate-200">Electrical network overview</h2>
        <FeederOneLine read={read} physical selected={selected} onSelect={showDevice} />
        <div className="mt-3 grid min-w-0 gap-3 text-xs sm:grid-cols-3">{(["S1", "S2", "S3"] as SectionId[]).map((id) => {
          const live = read(`${id.toLowerCase()}-live`), path = sectionSupplyPath(read, id);
          return <div key={id} className="min-w-0 text-slate-300 [overflow-wrap:anywhere]">
            <h3 className="font-semibold">{id} · physical energization</h3>
            <p className="font-mono">{live.condition === "good" ? live.value ? "ENERGIZED" : "NOT ENERGIZED" : "UNKNOWN"} · solver bus live</p>
            <p className="font-mono">Measured kV: {read(SECTION_VOLTAGE[id]).shortText}</p>
            <p className="font-mono">Physical topology path: {field.process.truth_stale || unavailable ? "unknown" : path.state}</p>
            <p className="text-slate-400">{path.reason}</p>
          </div>;
        })}</div>
        <p className="mt-2 text-xs text-slate-400">Physical topology paths use simulation switch states; CC paths are inferred from IEC observations. No source is substituted for the other. Energization is the solver&apos;s live flag, not a calculation from the displayed kV.</p>
      </section>
      <PhysicalEquipment field={field} expanded={expanded} onToggle={(id) => { if (!expanded[id]) onSelect(id); toggle(id); }} unavailable={unavailable} />
      <section aria-label="Advanced diagnostics" className={`${PANEL} border-slate-700`}>
        <button type="button" className={ACTION} aria-expanded={!!expanded.diagnostics} aria-controls="physical-diagnostics" onClick={() => toggle("diagnostics")}>{expanded.diagnostics ? "−" : "+"} Advanced diagnostics</button>
        {expanded.diagnostics && <div id="physical-diagnostics"><PhysicalDiagnostics field={field} audit={audit} /></div>}
      </section>
    </>}
  </section>;
}
