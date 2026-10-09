"use client";

import { useEffect, useId } from "react";
import { AlertTriangle } from "lucide-react";
import { confirmationConsequences, REQUEST_LABELS } from "../lib/range";

// Asks before a restart or switch, stating what the student loses.
export function RangeConfirmDialog({
  kind,
  packageTitle,
  pending,
  error,
  onConfirm,
  onCancel,
}: {
  kind: "restart" | "switch";
  packageTitle: string;
  pending: boolean;
  error: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const titleId = useId();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !pending) onCancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel, pending]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/70 p-4 backdrop-blur-sm">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="w-full max-w-md rounded-xl border border-slate-700 bg-slate-900 p-5 shadow-2xl"
      >
        <div className="flex items-start gap-3">
          <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-amber-400" />
          <div className="min-w-0">
            <h2 id={titleId} className="text-base font-semibold text-white">
              {kind === "switch" ? `Switch the range to ${packageTitle}?` : `Restart ${packageTitle}?`}
            </h2>
            <ul className="mt-3 list-disc space-y-1.5 pl-4 text-sm text-slate-300 marker:text-slate-600">
              {confirmationConsequences(kind, packageTitle).map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            {error && <p className="mt-3 text-xs text-rose-300">{error}</p>}
          </div>
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            autoFocus
            onClick={onCancel}
            disabled={pending}
            className="rounded-md border border-slate-700 px-3 py-1.5 text-sm font-medium text-slate-200 transition-colors hover:bg-slate-800 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={pending}
            className="rounded-md border border-amber-700/60 bg-amber-950/40 px-3 py-1.5 text-sm font-medium text-amber-300 transition-colors hover:border-amber-600 hover:bg-amber-900/40 disabled:opacity-50"
          >
            {pending ? "Requesting…" : REQUEST_LABELS[kind]}
          </button>
        </div>
      </div>
    </div>
  );
}
