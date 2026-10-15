"use client";

import type { OperatorJournal } from "../lib/operator-api";
import { formatUtcTime, journalRangeText, journalSummary, journalTone, type Tone } from "../lib/operator-format";

const SHOWN = 60;

const KIND_STYLE: Record<Tone, string> = {
  ok: "border-green-800/60 text-green-400",
  bad: "border-red-800/60 text-red-400",
  warn: "border-dashed border-amber-600 text-amber-300",
};

// The CC's own event journal, newest first: link and GI transitions,
// point quality changes and commands.
export function ControlCentreJournal({ journal }: { journal: OperatorJournal | undefined }) {
  if (!journal) return <div className="py-4 text-center text-sm text-slate-500">Loading the journal…</div>;
  const entries = journal.entries.slice(-SHOWN).reverse();
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-x-4 text-[10px] text-slate-500">
        <span>{journalRangeText(journal.oldest_seq, journal.latest_seq)}</span>
        <span>cache epoch {journal.cache_epoch}</span>
        {journal.entries.length > SHOWN && <span>showing the latest {SHOWN}</span>}
      </div>
      {entries.length === 0 ? (
        <div className="py-4 text-center text-sm text-slate-500">No journal entries yet.</div>
      ) : (
        <ol className="max-h-80 space-y-0.5 overflow-y-auto font-mono text-[11px]">
          {entries.map((entry) => {
            const tone = journalTone(entry.kind);
            return (
              <li key={entry.seq} className="flex min-w-0 items-start gap-2 border-t border-slate-800/50 py-1">
                <span className="w-10 shrink-0 text-right text-slate-600">{entry.seq}</span>
                <span className="w-24 shrink-0 text-slate-500">{formatUtcTime(entry.at)}</span>
                <span
                  className={`w-24 shrink-0 rounded border px-1 text-center text-[10px] font-bold ${
                    tone ? KIND_STYLE[tone] : "border-slate-700 text-slate-400"
                  }`}
                >
                  {entry.kind}
                </span>
                <span className="min-w-0 break-words text-slate-300">{journalSummary(entry.kind, entry.detail)}</span>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
