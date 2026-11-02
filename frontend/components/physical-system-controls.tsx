"use client";

import { useRef, useState } from "react";
import { sendFieldAction } from "../lib/api";
import type { DeviceState } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { errorMessage } from "../lib/utils";
import { ACTION, PANEL } from "./physical-system-parts";

type FieldAction = Parameters<typeof sendFieldAction>[0];
type Choice = { label: string; body: FieldAction; confirmation?: string; enabled?: boolean };

function ActionGroup({ choices, disabled }: { choices: Choice[]; disabled: boolean }) {
  const busy = useRef(false);
  const [pending, setPending] = useState<string | null>(null);
  const [result, setResult] = useState<{ text: string; error: boolean } | null>(null);
  const execute = async (choice: Choice) => {
    if (busy.current || disabled || choice.enabled === false) return;
    if (choice.confirmation && !window.confirm(choice.confirmation)) return;
    busy.current = true;
    setPending(choice.label);
    setResult(null);
    try {
      await sendFieldAction(choice.body);
      setResult({ text: `${choice.label}: simulation action completed. Live state refreshes automatically.`, error: false });
    } catch (e) {
      setResult({ text: `${choice.label}: ${errorMessage(e)}`, error: true });
    } finally {
      busy.current = false;
      setPending(null);
    }
  };
  return <>
    <div className="flex flex-wrap gap-2">{choices.map((choice) => <button
      key={choice.label} type="button" disabled={disabled || pending !== null || choice.enabled === false}
      className={`${ACTION} disabled:cursor-not-allowed disabled:opacity-50`}
      onClick={() => void execute(choice)}
    >{choice.label}</button>)}</div>
    <div aria-live="polite" aria-atomic="true" className="mt-1 text-xs wrap-anywhere">
      {pending && <p role="status" className="text-sky-300">{pending} · pending…</p>}
      {result && <p role={result.error ? "alert" : "status"} className={result.error ? "text-rose-300" : "text-slate-300"}>{result.text}</p>}
    </div>
  </>;
}

export function PhysicalSystemControls({ unavailable }: { unavailable: boolean }) {
  return <section aria-label="Process simulation controls" className={`${PANEL} space-y-2 border-slate-700`}>
    <h2 className="text-sm font-semibold text-slate-200">Process</h2>
    <p className="text-xs text-slate-400">Simulation (field) actions · out-of-band, not IEC 104 operator commands. Clearing a fault does not reset lockout or FPI latches.</p>
    <ActionGroup disabled={unavailable} choices={[
      { label: "Inject fault S1", body: { device: "process", action: "fault_s1" }, confirmation: "Inject a simulated fault on S1? This can trip protection and interrupt supply." },
      { label: "Inject fault S2", body: { device: "process", action: "fault_s2" }, confirmation: "Inject a simulated fault on S2? This can trip protection and interrupt supply." },
      { label: "Clear fault", body: { device: "process", action: "clear_fault" } },
    ]} />
    {unavailable && <p className="text-xs text-amber-300">Actions unavailable while simulation state is missing or retained.</p>}
  </section>;
}

export function PhysicalEquipmentControls({ id, device, unavailable }: { id: DeviceId; device: DeviceState; unavailable: boolean }) {
  const resetExplanation = id === "F2"
    ? "Reset lockout leaves the breaker OPEN. F2 has no close control in the Control Centre."
    : "Reset lockout leaves the breaker OPEN. Close it from the Control Centre.";
  let choice: Choice;
  if ((id === "CB1" || id === "F2") && device.kind === "breaker") {
    choice = {
      label: "Reset lockout", body: { device: id === "CB1" ? "feeder-breaker" : "f2-breaker", action: "reset_lockout" },
      enabled: device.lockout === true,
      confirmation: `Reset ${id} lockout? This clears the trip latch. ${resetExplanation}`,
    };
  } else if ((id === "RMU1" || id === "RMU2") && device.kind === "rmu") {
    choice = {
      label: "Reset FPI", body: { device: id === "RMU1" ? "rmu-1" : "rmu-2", action: "reset_fault_passage" },
      enabled: device.fault_passage === true,
      confirmation: `Reset ${id} fault passage? This clears the latched fault indication, not the process fault.`,
    };
  } else {
    return null;
  }
  return <section aria-label={`${id} simulation controls`} className="my-2 space-y-1">
    <p className="text-xs text-slate-400">Simulation (field) action · not an IEC 104 command</p>
    <ActionGroup disabled={unavailable} choices={[choice]} />
    {device.kind === "breaker" && <p className="text-xs text-slate-400">{resetExplanation}</p>}
    {unavailable && <p className="text-xs text-amber-300">Actions unavailable while simulation state is retained.</p>}
  </section>;
}
