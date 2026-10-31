"use client";

import { useRef, useState } from "react";
import type { CommandOperation, CommandResult, ControlCentreLink, LinkState, OperatorPoint } from "../lib/operator-api";
import { commandErrorText, commandOutcome, formatPointValue, formatUtcTime, pointCondition, qualityDefinition } from "../lib/operator-format";
import { receivedAuthority, type DeviceId } from "../lib/operator-workstation";
import { useOperatorCommand } from "../lib/operator-queries";

export const CONTROL_PANELS = [
  { point: "feeder-breaker-command", label: "CB1 feeder breaker", feedback: "feeder-breaker-position", operations: ["open", "close"], wire: "C_DC_NA_1 · IOA 3001" },
  { point: "rmu-1-command", label: "RMU1 sectionaliser", feedback: "rmu-1-position", operations: ["open", "close"], wire: "C_DC_NA_1 · IOA 3101" },
  { point: "rmu-2-command", label: "RMU2 normally-open tie", feedback: "rmu-2-position", operations: ["open", "close"], wire: "C_DC_NA_1 · IOA 3201" },
  { point: "oltc-1-step", label: "T1 tap step", feedback: "oltc-1-tap", operations: ["lower", "raise"], wire: "C_RC_NA_1 · IOA 3301" },
  { point: "oltc-1-mode", label: "T1 auto / manual", feedback: "oltc-1-auto", operations: ["auto", "manual"], wire: "C_SC_NA_1 · IOA 3302" },
] as const;
export type ControlPanel = (typeof CONTROL_PANELS)[number];
export type CommandPanelState =
  | { phase: "idle" }
  | { phase: "armed" | "pending"; operation: CommandOperation }
  | { phase: "result"; operation: CommandOperation; result: CommandResult }
  | { phase: "error"; operation: CommandOperation; message: string };

const BUTTON = "rounded-sm border border-slate-600 bg-slate-900 px-3 py-1.5 text-xs font-bold text-slate-100 hover:bg-slate-800 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-sky-400 disabled:opacity-50";

export function CommandResultView({ result }: { result: CommandResult }) {
  const outcome = commandOutcome(result);
  const feedbackPoint = CONTROL_PANELS.find((panel) => panel.point === result.point)?.feedback ?? "";
  return <div role="status" className={`rounded-sm border p-3 text-xs ${outcome.tone === "ok" ? "border-green-800 text-green-300" : outcome.tone === "warn" ? "border-amber-700 text-amber-300" : "border-red-800 text-red-300"}`}>
    <p className="font-bold">{result.operation.toUpperCase()} {outcome.title}</p>
    {result.reason !== null && <p>{result.reason}</p>}
    {result.result === "unknown" && <p>It is not retried. Observe fresh feedback before deciding what to do next.</p>}
    <dl className="mt-2 grid grid-cols-2 gap-x-3 font-mono text-slate-300">
      <dt>Command</dt><dd>{result.point} · {result.id}</dd>
      <dt>Select requested (UTC)</dt><dd>{formatUtcTime(result.selected_at)}</dd>
      <dt>Executed (UTC)</dt><dd>{formatUtcTime(result.executed_at)}</dd>
      <dt>ACT-CON (UTC)</dt><dd>{formatUtcTime(result.con_at)}</dd>
      <dt>ACT-TERM (UTC)</dt><dd>{formatUtcTime(result.term_at)}</dd>
      <dt>Feedback</dt><dd>{result.feedback ? `IOA ${result.feedback.ioa}: ${formatPointValue(feedbackPoint, result.feedback.value)} at ${formatUtcTime(result.feedback.at)}` : "not received"}</dd>
    </dl>
  </div>;
}

export function CommandPanelView({ linkState, position, state, onArm, onExecute, onCancel, panel = CONTROL_PANELS[0], disabled = false, authority = "not established" }: {
  linkState: LinkState;
  position: OperatorPoint | undefined;
  state: CommandPanelState;
  onArm: (operation: CommandOperation) => void;
  onExecute: (operation: CommandOperation) => void;
  onCancel: () => void;
  panel?: ControlPanel;
  disabled?: boolean;
  authority?: ReturnType<typeof receivedAuthority>;
}) {
  const condition = position ? pointCondition(position) : "never";
  const ready = linkState === "started" && !!position && position.value !== null && !position.quality.iv && !position.quality.nt && !disabled;
  const busy = state.phase === "pending";
  return <section aria-label={panel.label} className="space-y-2 border-t border-slate-800 pt-3">
    <h3 className="text-sm font-bold text-slate-200">{panel.label}</h3>
    <p className="text-xs text-slate-400">{panel.wire} · select-before-operate via RTU</p>
    <p className="text-xs text-slate-300">Reported: {position ? formatPointValue(position.id, position.value) : "unknown"} · <span title={qualityDefinition(condition)} tabIndex={0}>{condition === "good" ? "GOOD" : condition === "stale" ? "STALE" : "UNKNOWN"}</span></p>
    {state.phase === "armed" ? <div className="space-y-2 rounded-sm border border-sky-800 bg-sky-950/30 p-2">
      <p className="text-xs text-sky-200">Local confirmation: {state.operation.toUpperCase()} on {panel.label}? No RTU reservation has been made.</p>
      <p className="text-xs text-sky-200">Received control authority: {authority}. This attempt is still subject to device interlocks.</p>
      <div className="flex flex-wrap gap-2">
        <button className={BUTTON} disabled={!ready} onClick={() => onExecute(state.operation)}>Execute {state.operation.toUpperCase()}</button>
        <button className={BUTTON} onClick={onCancel}>Cancel</button>
      </div>
    </div> : <div className="flex flex-wrap gap-2">{panel.operations.map((op) => <button key={op} className={BUTTON} disabled={!ready || busy} onClick={() => onArm(op)}>Confirm {op.toUpperCase()}…</button>)}</div>}
    {!ready && <p className="text-xs text-amber-300">Commands require a started link, completed integrity GI and feedback without IV/NT. The RTU checks interlocks; commands cannot be sent while unavailable or busy.</p>}
    {busy && <p role="status" className="text-xs text-slate-300">{state.operation.toUpperCase()}: select, execute and fresh feedback in progress…</p>}
    {state.phase === "result" && <CommandResultView result={state.result} />}
    {state.phase === "error" && <p role="alert" className="text-xs text-amber-300">{state.message}</p>}
  </section>;
}

// Selection only arms the browser. Exactly one POST performs wire SBO.
export function SupervisoryControls({ link, points, unavailable, selected }: { link: ControlCentreLink; points: OperatorPoint[]; unavailable: boolean; selected: DeviceId }) {
  const command = useOperatorCommand();
  const inFlight = useRef(false);
  const [active, setActive] = useState<string | null>(null);
  const [state, setState] = useState<CommandPanelState>({ phase: "idle" });
  const [previousSelection, setPreviousSelection] = useState(selected);
  // Discard only local arming before rendering a different device. In-flight
  // commands and their late outcomes must retain their original attribution.
  if (previousSelection !== selected) {
    setPreviousSelection(selected);
    if (state.phase === "armed") setState({ phase: "idle" });
  }
  const visible = CONTROL_PANELS.filter((p) => selected === "F2" ? false : selected === "T1" ? p.point.startsWith("oltc-") : selected === "CB1" ? p.point.startsWith("feeder-") : p.point.startsWith(selected === "RMU1" ? "rmu-1-" : "rmu-2-"));
  const execute = (panel: ControlPanel, operation: CommandOperation) => {
    const feedback = points.find((p) => p.id === panel.feedback);
    if (inFlight.current || unavailable || link.state !== "started" || !link.gi.last_complete_at || !feedback || feedback.value === null || feedback.quality.iv || feedback.quality.nt || active !== panel.point || state.phase !== "armed" || state.operation !== operation) return;
    inFlight.current = true;
    setState({ phase: "pending", operation });
    command.mutate({ point: panel.point, operation }, {
      onSuccess: (result) => setState({ phase: "result", operation, result }),
      onError: (error) => setState({ phase: "error", operation, message: commandErrorText(error) }),
      onSettled: () => { inFlight.current = false; },
    });
  };
  return <div className="space-y-3">{visible.map((panel) => <CommandPanelView
    key={panel.point} panel={panel} linkState={link.state} position={points.find((p) => p.id === panel.feedback)}
    authority={receivedAuthority(points, selected, unavailable || link.state !== "started")}
    state={active === panel.point && state.phase !== "result" && state.phase !== "error" ? state : { phase: "idle" }} disabled={unavailable || !link.gi.last_complete_at || state.phase === "pending"}
    onArm={(operation) => { setActive(panel.point); setState({ phase: "armed", operation }); }}
    onExecute={(operation) => execute(panel, operation)} onCancel={() => setState({ phase: "idle" })}
  />)}
    {state.phase === "pending" && <p role="status" className="text-xs text-slate-300">In flight: {active} · {state.operation}</p>}
    {(state.phase === "result" || state.phase === "error") && <section aria-label="Last command outcome" className="space-y-2 wrap-break-word">
      <h3 className="text-xs text-slate-300">Command outcome · {CONTROL_PANELS.find((p) => p.point === active)?.label}</h3>
      {state.phase === "result" ? <CommandResultView result={state.result} /> : <p role="alert" className="text-xs text-amber-300">{state.message}</p>}
    </section>}
  </div>;
}
