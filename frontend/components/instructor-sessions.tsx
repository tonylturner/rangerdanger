"use client";

import { useState } from "react";
import {
  IEC104_PORT,
  controlCentreMatchText,
  cutFailureText,
  findControlCentreSession,
  isControlCentreSession,
  killResultText,
  matchControlCentreSession,
  sessionTuple,
  tupleText,
} from "../lib/firewall-sessions";
import type { FirewallSession, SessionTuple } from "../lib/operator-api";
import { useSessionKill } from "../lib/operator-queries";
import { errorMessage } from "../lib/utils";

export function SessionsTable({
  sessions,
  pending,
  onKill,
}: {
  sessions: FirewallSession[];
  pending: boolean;
  onKill: (tuple: SessionTuple) => void;
}) {
  if (sessions.length === 0) {
    return <div className="py-3 text-sm text-slate-500">containd lists no tcp/{IEC104_PORT} sessions.</div>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[11px]">
        <thead>
          <tr className="text-left text-[9px] uppercase tracking-wider text-slate-500">
            <th className="px-2 py-1">Flow (original direction)</th>
            <th className="px-2 py-1">State</th>
            <th className="px-2 py-1">Timeout</th>
            <th className="px-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {sessions.map((session, i) => {
            const tuple = sessionTuple(session);
            return (
              <tr key={tuple ? tupleText(tuple) : `row-${i}`} className="border-t border-slate-800/60 font-mono">
                <td className="px-2 py-1.5 text-slate-200">
                  {tuple
                    ? tupleText(tuple)
                    : `${session.proto} ${session.src}:${session.sport ?? "?"} → ${session.dst}:${session.dport ?? "?"}`}
                  {isControlCentreSession(session) && (
                    <span className="ml-2 rounded border border-teal-700 px-1 font-sans text-[9px] font-bold text-teal-300">
                      CONTROL CENTRE
                    </span>
                  )}
                </td>
                <td className="px-2 py-1.5 text-slate-400">{session.state ?? "—"}</td>
                <td className="px-2 py-1.5 text-slate-400">{session.timeout} s</td>
                <td className="px-2 py-1.5 text-right">
                  <button
                    onClick={() => tuple && onKill(tuple)}
                    disabled={!tuple || pending}
                    className="rounded border border-rose-800 bg-rose-950/40 px-2 py-0.5 font-sans text-[10px] font-medium text-rose-300 hover:bg-rose-900/50 disabled:opacity-50"
                  >
                    Kill
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

type Outcome = { text: string; ok: boolean };

export function SessionControls({ sessions, sessionsError }: { sessions: FirewallSession[] | undefined; sessionsError: unknown }) {
  const kill = useSessionKill();
  const [cutting, setCutting] = useState(false);
  const [result, setResult] = useState<Outcome | null>(null);
  const busy = cutting || kill.isPending;

  const killRow = (tuple: SessionTuple) => {
    setResult(null);
    kill.mutate(tuple, {
      onSuccess: () => setResult({ text: killResultText(tuple, null), ok: true }),
      onError: (error) => setResult({ text: killResultText(tuple, error), ok: false }),
    });
  };

  // The tuple comes from a fresh read at the moment of the cut, not
  // from the polled table, which can be seconds old.
  const cutControlCentre = async () => {
    setResult(null);
    setCutting(true);
    let tuple: SessionTuple | null = null;
    try {
      tuple = await findControlCentreSession();
      await kill.mutateAsync(tuple);
      setResult({ text: killResultText(tuple, null), ok: true });
    } catch (error) {
      setResult({ text: cutFailureText(tuple, error), ok: false });
    } finally {
      setCutting(false);
    }
  };

  const listed = sessions === undefined ? null : matchControlCentreSession(sessions);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <button
          onClick={() => void cutControlCentre()}
          disabled={busy}
          className="rounded border border-rose-700 bg-rose-950/50 px-3 py-1.5 text-xs font-bold text-rose-200 hover:bg-rose-900/60 disabled:opacity-50"
        >
          Cut control-centre session
        </button>
        <span className="font-mono text-[11px] text-slate-400">
          {listed === null ? "Looking up the control centre's session…" : controlCentreMatchText(listed)}
        </span>
      </div>
      <p className="text-[11px] text-slate-500">
        The cut reads containd&apos;s sessions again and deletes the control centre → RTU entry only when exactly one
        matches. Deleting it sends no reset: the flow only stays cut when the active policy denies the control centre →
        RTU traffic; under an allowing policy the next packet re-creates it.
      </p>
      {result && (
        <div
          role="status"
          className={`rounded border px-3 py-2 text-xs ${
            result.ok ? "border-green-800/60 bg-green-950/20 text-green-300" : "border-red-800/60 bg-red-950/20 text-red-300"
          }`}
        >
          {result.text}
        </div>
      )}
      {sessionsError !== null && sessionsError !== undefined ? (
        <div className="text-xs text-red-300">Firewall sessions unavailable: {errorMessage(sessionsError)}</div>
      ) : sessions === undefined ? (
        <div className="text-xs text-slate-500">Loading firewall sessions…</div>
      ) : (
        <SessionsTable sessions={sessions} pending={busy} onKill={killRow} />
      )}
    </div>
  );
}
