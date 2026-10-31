"use client";

import type { ReactNode } from "react";
import type { PackageSummary } from "../lib/api";
import { operatorViewProblem, type OperatorView, type OperatorViewResolution } from "../lib/operator-view";

// Renders the page variant of the active package's operator view.
// Every page that dispatches names a variant for every view, so a new
// view is a compile error until each page handles it. While the active
// package loads nothing package-specific renders (and nothing polls);
// an unknown view or an unreadable package is an error panel, never
// another package's page.
export function OperatorViewSwitch({
  resolution,
  views,
}: {
  resolution: OperatorViewResolution;
  views: Record<OperatorView, (pkg: PackageSummary) => ReactNode>;
}) {
  if (resolution.kind === "known") return <>{views[resolution.view](resolution.pkg)}</>;
  const problem = operatorViewProblem(resolution);
  return (
    <main className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
      {problem === null ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900/60 px-4 py-3 text-sm text-slate-400">
          Loading the active package…
        </div>
      ) : (
        <OperatorViewProblemPanel message={problem} />
      )}
    </main>
  );
}

export function OperatorViewProblemPanel({ message }: { message: string }) {
  return (
    <div role="alert" className="rounded-lg border border-red-800/70 bg-red-950/30 px-4 py-3">
      <div className="text-[10px] font-bold uppercase tracking-wider text-red-400">Operator view unavailable</div>
      <p className="mt-1 text-sm text-red-200">{message}</p>
      <p className="mt-1 text-xs text-slate-400">
        The portal shows no operator screen rather than one that belongs to another package.
      </p>
    </div>
  );
}
