"use client";

import type { ReactNode } from "react";
import { AlertTriangle, Loader2, Power } from "lucide-react";
import type { PackageSummary } from "../lib/api";
import {
  TRANSITION_STEPS,
  isTransitioning,
  transitionPackage,
  transitionStepLabel,
  transitionStepNumber,
  type RangeStatus,
} from "../lib/range";
import { usePackages, useRange } from "./range-context";

export function packageTitle(packages: PackageSummary[] | undefined, id: string): string {
  return packages?.find((p) => p.id === id)?.title ?? id;
}

// A strip above every page while no range is serving, so a page whose
// requests answer 503 reads as "waiting for the range", not as broken.
export function RangeBanner() {
  const { status } = useRange();
  const packages = usePackages();
  if (!status) return null;
  return <RangeBannerView status={status} packages={packages} />;
}

export function RangeBannerView({
  status,
  packages,
}: {
  status: RangeStatus;
  packages: PackageSummary[] | undefined;
}) {
  const { phase } = status;
  if (isTransitioning(phase)) {
    const title = packageTitle(packages, transitionPackage(status));
    return (
      <Strip tone="sky" icon={<Loader2 className="h-4 w-4 animate-spin" />}>
        <span className="font-semibold text-sky-100">Bringing up {title}</span>
        <span className="text-sky-300">
          {" "}
          · Step {transitionStepNumber(phase)} of {TRANSITION_STEPS.length}: {transitionStepLabel(phase)}.
        </span>
        <span className="block text-xs text-slate-400">
          Pages reconnect on their own once the range is ready; lab actions wait until then.
        </span>
      </Strip>
    );
  }
  if (phase === "failed") {
    return (
      <Strip tone="rose" icon={<AlertTriangle className="h-4 w-4" />}>
        <span className="font-semibold text-rose-100">
          {packageTitle(packages, status.package)} did not start
        </span>
        {status.error && <span className="block break-words font-mono text-xs text-rose-300">{status.error}</span>}
        <span className="block text-xs text-slate-400">
          Retry it, or choose another package, from the Range control in the sidebar.
        </span>
      </Strip>
    );
  }
  if (phase === "none") {
    return (
      <Strip tone="slate" icon={<Power className="h-4 w-4" />}>
        <span className="font-semibold text-slate-200">No range is running.</span>
        <span className="text-slate-400"> Start one from the Range control in the sidebar.</span>
      </Strip>
    );
  }
  return null;
}

const TONES = {
  sky: "border-sky-900/70 bg-sky-950/40 text-sky-400",
  rose: "border-rose-900/70 bg-rose-950/40 text-rose-400",
  slate: "border-slate-800 bg-slate-900/60 text-slate-400",
} as const;

function Strip({
  tone,
  icon,
  children,
}: {
  tone: keyof typeof TONES;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <div role="status" aria-live="polite" className={`flex items-start gap-3 border-b px-6 py-2.5 ${TONES[tone]}`}>
      <span className="mt-0.5 shrink-0">{icon}</span>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}
