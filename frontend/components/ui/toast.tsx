"use client";

import { useEffect } from "react";
import { Loader2, X } from "lucide-react";

export type ToastKind = "loading" | "success" | "error";

export interface ToastState {
  kind: ToastKind;
  title: string;
  detail?: string;
}

const STYLES: Record<ToastKind, { border: string; bg: string; icon: string }> = {
  loading: { border: "border-sky-700", bg: "bg-sky-950/80", icon: "text-sky-400" },
  success: { border: "border-emerald-700", bg: "bg-emerald-950/80", icon: "text-emerald-400" },
  error: { border: "border-rose-700", bg: "bg-rose-950/80", icon: "text-rose-400" },
};

export function Toast({
  toast,
  onDismiss,
}: {
  toast: ToastState | null;
  onDismiss: () => void;
}) {
  useEffect(() => {
    if (!toast || toast.kind === "loading") return;
    const t = setTimeout(onDismiss, toast.kind === "success" ? 4000 : 8000);
    return () => clearTimeout(t);
  }, [toast, onDismiss]);

  if (!toast) return null;
  const style = STYLES[toast.kind];

  return (
    <div className="pointer-events-none fixed bottom-6 right-6 z-50 flex justify-end">
      <div
        role="status"
        aria-live="polite"
        className={`pointer-events-auto flex min-w-[280px] max-w-md items-start gap-3 rounded-lg border ${style.border} ${style.bg} px-4 py-3 shadow-xl backdrop-blur-sm`}
      >
        <div className={`mt-0.5 shrink-0 ${style.icon}`}>
          {toast.kind === "loading" && <Loader2 className="h-5 w-5 animate-spin" />}
          {toast.kind === "success" && (
            // Wink raccoon for the celebratory beat.
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src="/rook-quarter-turn-wink-transparent-web.png"
              alt=""
              className="h-7 w-7"
            />
          )}
          {toast.kind === "error" && (
            // Aggressive raccoon for failures — pairs visually with
            // the wink-on-success above.
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src="/rook-forward-aggressive-transparent-web.png"
              alt=""
              className="h-7 w-7"
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold text-white">{toast.title}</div>
          {toast.detail && (
            <div className="mt-0.5 break-words text-xs text-slate-300">{toast.detail}</div>
          )}
        </div>
        {toast.kind !== "loading" && (
          <button
            onClick={onDismiss}
            aria-label="Dismiss"
            className="shrink-0 text-slate-400 transition-colors hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>
    </div>
  );
}
