"use client";

import { useState, type ReactNode } from "react";
import type { CommandOperation, CommandResult, LinkState, OperatorPoint } from "../lib/operator-api";
import {
  commandErrorText,
  commandOutcome,
  formatPointValue,
  formatUtcTime,
  phaseStatusText,
  phaseTone,
  pointCondition,
  type Tone,
} from "../lib/operator-format";
import { useOperatorCommand } from "../lib/operator-queries";

const TONE_TEXT: Record<Tone, string> = {
  ok: "text-green-400",
  bad: "text-red-400",
  warn: "text-amber-300",
};

const TONE_BOX: Record<Tone, string> = {
  ok: "border-green-800/60 bg-green-950/20",
  bad: "border-red-800/60 bg-red-950/20",
  warn: "border-amber-700/70 bg-amber-950/20",
};

const TONE_GLYPH: Record<Tone, string> = { ok: "✓", bad: "✗", warn: "?" };

function PhaseRow({ label, status, tone, at }: { label: string; status: string; tone: Tone | null; at: ReactNode }) {
  return (
    <tr className="border-t border-slate-800/60">
      <td className="py-1 pr-3 text-slate-500">{label}</td>
      <td className={`py-1 pr-3 font-bold ${tone ? TONE_TEXT[tone] : "text-slate-500"}`}>
        {tone && <span aria-hidden>{TONE_GLYPH[tone]} </span>}
        {status}
      </td>
      <td className="py-1 font-mono text-slate-400">{at}</td>
    </tr>
  );
}

// The CC's full answer to one command: outcome, then every phase.
export function CommandResultView({ operation, result }: { operation: CommandOperation; result: CommandResult }) {
  const outcome = commandOutcome(result);
  const feedbackValue = formatPointValue("feeder-breaker-position", result.feedback.value);
  return (
    <div role="status" className={`rounded border px-3 py-2 text-xs ${TONE_BOX[outcome.tone]}`}>
      <div className="flex flex-wrap items-baseline gap-2">
        <span className={`font-bold tracking-wider ${TONE_TEXT[outcome.tone]}`}>
          {TONE_GLYPH[outcome.tone]} {operation.toUpperCase()} {outcome.title}
        </span>
        <span className="font-mono text-[10px] text-slate-500">id {result.id}</span>
      </div>
      <p className="mt-1 text-slate-300">{outcome.explanation}</p>
      <table className="mt-2 w-full text-[11px]">
        <tbody>
          <PhaseRow
            label="Select"
            status={phaseStatusText(result.select.status)}
            tone={phaseTone(result.select.status)}
            at={formatUtcTime(result.select.at)}
          />
          <PhaseRow
            label="Execute"
            status={phaseStatusText(result.execute.status)}
            tone={phaseTone(result.execute.status)}
            at={formatUtcTime(result.execute.at)}
          />
          <PhaseRow
            label="Terminated"
            status={result.execute.terminated_at === null ? "NO ACT-TERM" : "ACT-TERM"}
            tone={result.execute.terminated_at === null ? null : "ok"}
            at={formatUtcTime(result.execute.terminated_at)}
          />
          <PhaseRow
            label="Feedback"
            status={`${result.feedback.status.toUpperCase()}${result.feedback.value === null ? "" : ` · ${feedbackValue}`}`}
            tone={phaseTone(result.feedback.status)}
            at={formatUtcTime(result.feedback.received_at)}
          />
        </tbody>
      </table>
      {result.error !== null && <p className="mt-2 font-mono text-[11px] text-red-300">{result.error}</p>}
    </div>
  );
}

export type CommandPanelState =
  | { phase: "idle" }
  | { phase: "armed"; operation: CommandOperation }
  | { phase: "pending"; operation: CommandOperation }
  | { phase: "result"; operation: CommandOperation; result: CommandResult }
  | { phase: "error"; operation: CommandOperation; message: string };

export function CommandPanelView({
  linkState,
  position,
  state,
  onArm,
  onExecute,
  onCancel,
}: {
  linkState: LinkState;
  position: OperatorPoint | undefined;
  state: CommandPanelState;
  onArm: (operation: CommandOperation) => void;
  onExecute: (operation: CommandOperation) => void;
  onCancel: () => void;
}) {
  const linkUp = linkState === "started";
  const busy = state.phase === "pending";
  const condition = position ? pointCondition(position) : "never";
  return (
    <div className="space-y-3">
      <div>
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Breaker control</div>
        <div className="font-mono text-[10px] text-slate-600">
          Double command C_DC_NA_1 · IOA 3001 · select-before-operate via the RTU
        </div>
      </div>

      <div className="text-xs text-slate-400">
        Position reported:{" "}
        <span className={`font-mono font-bold ${condition === "good" ? "text-slate-100" : "text-amber-300"}`}>
          {position ? formatPointValue(position.id, position.value) : "not reported"}
        </span>
        {condition === "stale" && <span className="ml-1 font-bold text-amber-400">⚠ STALE</span>}
      </div>

      {state.phase === "armed" ? (
        <div className="rounded border border-sky-800/70 bg-sky-950/30 p-2">
          <p className="text-xs text-sky-200">
            Select then execute <span className="font-bold">{state.operation.toUpperCase()}</span> on the feeder breaker?
          </p>
          <div className="mt-2 flex gap-2">
            <button
              onClick={() => onExecute(state.operation)}
              disabled={!linkUp}
              className="rounded border border-sky-600 bg-sky-900/60 px-3 py-1 text-xs font-bold text-sky-100 hover:bg-sky-800/70 disabled:opacity-50"
            >
              Execute {state.operation.toUpperCase()}
            </button>
            <button
              onClick={onCancel}
              className="rounded border border-slate-700 px-3 py-1 text-xs text-slate-300 hover:bg-slate-900"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <div className="flex gap-2">
          <button
            onClick={() => onArm("open")}
            disabled={!linkUp || busy}
            className="rounded border border-slate-600 bg-slate-900 px-3 py-1.5 text-xs font-bold text-slate-100 hover:bg-slate-800 disabled:opacity-50"
          >
            OPEN…
          </button>
          <button
            onClick={() => onArm("close")}
            disabled={!linkUp || busy}
            className="rounded border border-slate-600 bg-slate-900 px-3 py-1.5 text-xs font-bold text-slate-100 hover:bg-slate-800 disabled:opacity-50"
          >
            CLOSE…
          </button>
        </div>
      )}

      {!linkUp && (
        <p className="text-[11px] text-amber-300">
          No started IEC 104 link to the RTU: commands cannot be sent. The positions shown are the last ones received.
        </p>
      )}

      {state.phase === "pending" && (
        <div role="status" className="rounded border border-slate-700 bg-slate-900 px-3 py-2 text-xs text-slate-300">
          {state.operation.toUpperCase()}: select, execute and feedback in progress (up to 10 s)…
        </div>
      )}
      {state.phase === "result" && <CommandResultView operation={state.operation} result={state.result} />}
      {state.phase === "error" && (
        <div role="alert" className={`rounded border px-3 py-2 text-xs ${TONE_BOX.bad}`}>
          <span className="font-bold text-red-400">✗ {state.operation.toUpperCase()} NOT DONE</span>
          <p className="mt-1 text-slate-300">{state.message}</p>
        </div>
      )}
    </div>
  );
}

export function BreakerCommandPanel({ linkState, position }: { linkState: LinkState; position: OperatorPoint | undefined }) {
  const command = useOperatorCommand();
  const [state, setState] = useState<CommandPanelState>({ phase: "idle" });
  const execute = (operation: CommandOperation) => {
    setState({ phase: "pending", operation });
    command.mutate(operation, {
      onSuccess: (result) => setState({ phase: "result", operation, result }),
      onError: (error) => setState({ phase: "error", operation, message: commandErrorText(error) }),
    });
  };
  return (
    <CommandPanelView
      linkState={linkState}
      position={position}
      state={state}
      onArm={(operation) => setState({ phase: "armed", operation })}
      onExecute={execute}
      onCancel={() => setState({ phase: "idle" })}
    />
  );
}
