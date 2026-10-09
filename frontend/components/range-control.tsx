"use client";

import { useCallback, useState } from "react";
import { ChevronDown, Loader2, RotateCw } from "lucide-react";
import { ApiError, type PackageSummary } from "../lib/api";
import {
  REQUEST_LABELS,
  TRANSITION_STEPS,
  isTransitioning,
  confirmationFor,
  requestKindFor,
  transitionPackage,
  transitionStepNumber,
  type RangeStatus,
} from "../lib/range";
import { errorMessage } from "../lib/utils";
import { packageTitle } from "./range-banner";
import { RangeConfirmDialog } from "./range-confirm-dialog";
import { usePackages, useRange, useRangeRequest } from "./range-context";
import { RangeSteps } from "./range-steps";

export function rangeRequestErrorText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409) return "Another range change is already running.";
    if (error.detail) return error.detail;
  }
  return errorMessage(error);
}

// The package the range runs, or would run if started: the record's
// package, else the backend default it reports as active.
export function currentPackageId(status: RangeStatus, packages: PackageSummary[] | undefined): string {
  if (isTransitioning(status.phase)) return transitionPackage(status);
  return status.package || packages?.find((p) => p.active)?.id || "";
}

type Confirming = { kind: "restart" | "switch"; packageId: string };

// Sidebar control for the one running range: shows its phase, lists
// the packages, and starts, restarts or switches it.
export function RangeControl() {
  const { status } = useRange();
  const packages = usePackages();
  const request = useRangeRequest();
  const [expanded, setExpanded] = useState(false);
  const [confirming, setConfirming] = useState<Confirming | null>(null);

  const submit = useCallback(
    (packageId: string) =>
      request.mutate(packageId, {
        onSuccess: () => {
          setConfirming(null);
          setExpanded(false);
        },
      }),
    [request],
  );

  const select = (packageId: string) => {
    if (!status) return;
    const kind = requestKindFor(status, packageId);
    if (kind === null) return;
    request.reset();
    const question = confirmationFor(kind, status.phase);
    if (question) setConfirming({ kind: question, packageId });
    else submit(packageId);
  };

  const cancel = useCallback(() => {
    setConfirming(null);
    request.reset();
  }, [request]);

  const requestError = request.error ? rangeRequestErrorText(request.error) : null;

  return (
    <>
      <RangeControlView
        status={status}
        packages={packages}
        expanded={expanded}
        pending={request.isPending}
        requestError={confirming ? null : requestError}
        onToggle={() => setExpanded((v) => !v)}
        onSelect={select}
      />
      {confirming && (
        <RangeConfirmDialog
          kind={confirming.kind}
          packageTitle={packageTitle(packages, confirming.packageId)}
          pending={request.isPending}
          error={requestError}
          onConfirm={() => submit(confirming.packageId)}
          onCancel={cancel}
        />
      )}
    </>
  );
}

const DOT: Record<"ready" | "busy" | "failed" | "none", string> = {
  ready: "bg-emerald-500",
  busy: "bg-sky-400 animate-pulse",
  failed: "bg-rose-500",
  none: "bg-slate-600",
};

function phaseSummary(status: RangeStatus): { dot: keyof typeof DOT; label: string } {
  const { phase } = status;
  if (isTransitioning(phase)) {
    return { dot: "busy", label: `Step ${transitionStepNumber(phase)} of ${TRANSITION_STEPS.length}` };
  }
  if (phase === "ready") return { dot: "ready", label: "Ready" };
  if (phase === "failed") return { dot: "failed", label: "Failed" };
  return { dot: "none", label: "Not running" };
}

export function RangeControlView({
  status,
  packages,
  expanded,
  pending,
  requestError,
  onToggle,
  onSelect,
}: {
  status: RangeStatus | undefined;
  packages: PackageSummary[] | undefined;
  expanded: boolean;
  pending: boolean;
  requestError: string | null;
  onToggle: () => void;
  onSelect: (packageId: string) => void;
}) {
  return (
    <div className="space-y-2">
      <div className="px-1 text-[9px] uppercase tracking-widest text-slate-600">Range</div>
      {status ? (
        <RangeBody
          status={status}
          packages={packages}
          expanded={expanded}
          pending={pending}
          onToggle={onToggle}
          onSelect={onSelect}
        />
      ) : (
        <div className="px-1 text-[11px] text-slate-600">Checking the range…</div>
      )}
      {requestError && <p className="px-1 text-[11px] leading-snug text-rose-300">{requestError}</p>}
    </div>
  );
}

function RangeBody({
  status,
  packages,
  expanded,
  pending,
  onToggle,
  onSelect,
}: {
  status: RangeStatus;
  packages: PackageSummary[] | undefined;
  expanded: boolean;
  pending: boolean;
  onToggle: () => void;
  onSelect: (packageId: string) => void;
}) {
  const currentId = currentPackageId(status, packages);
  const summary = phaseSummary(status);
  const step = isTransitioning(status.phase) ? status.phase : null;
  const busy = step !== null;
  return (
    <>
      <button
        type="button"
        onClick={onToggle}
        disabled={busy}
        aria-expanded={busy ? undefined : expanded}
        className="flex w-full items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-2.5 py-2 text-left transition-colors hover:border-slate-700 disabled:cursor-default disabled:hover:border-slate-800"
      >
        <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[summary.dot]}`} />
        <span className="min-w-0 flex-1">
          <span className="block break-words text-[12px] font-medium leading-snug text-slate-200">
            {packageTitle(packages, currentId) || "No package"}
          </span>
          <span className="block text-[10px] text-slate-500">{summary.label}</span>
        </span>
        {!busy && (
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-slate-500 transition-transform ${expanded ? "rotate-180" : ""}`} />
        )}
      </button>

      {step && (
        <div className="px-1">
          <RangeSteps step={step} />
        </div>
      )}

      {status.phase === "failed" && (
        <div className="space-y-2 rounded-md border border-rose-900/70 bg-rose-950/30 px-2.5 py-2">
          <p className="break-words text-[11px] leading-snug text-rose-200">{status.error || "The range did not start."}</p>
          <ActionButton label={REQUEST_LABELS.retry} pending={pending} onClick={() => onSelect(status.package)} />
        </div>
      )}

      {status.phase === "none" && currentId && (
        <ActionButton label={REQUEST_LABELS.start} pending={pending} onClick={() => onSelect(currentId)} />
      )}

      {status.phase === "ready" && status.error && (
        <p className="rounded-md border border-amber-900/60 bg-amber-950/30 px-2.5 py-2 text-[11px] leading-snug text-amber-200">
          The last change did not start: {status.error} The running range was not touched.
        </p>
      )}

      {expanded && !busy && (
        <PackageList status={status} packages={packages} currentId={currentId} pending={pending} onSelect={onSelect} />
      )}
    </>
  );
}

function PackageList({
  status,
  packages,
  currentId,
  pending,
  onSelect,
}: {
  status: RangeStatus;
  packages: PackageSummary[] | undefined;
  currentId: string;
  pending: boolean;
  onSelect: (packageId: string) => void;
}) {
  if (!packages) return <div className="px-1 text-[11px] text-slate-600">Loading packages…</div>;
  return (
    <ul className="space-y-1" aria-label="Range packages">
      {packages.map((pkg) => {
        const kind = requestKindFor(status, pkg.id);
        const current = pkg.id === currentId;
        return (
          <li key={pkg.id}>
            <button
              type="button"
              disabled={pending || kind === null}
              onClick={() => onSelect(pkg.id)}
              className={`w-full rounded-md border px-2.5 py-1.5 text-left transition-colors disabled:opacity-50 ${
                current
                  ? "border-sky-800/70 bg-sky-950/40 hover:border-sky-700"
                  : "border-slate-800 bg-slate-950/40 hover:border-slate-600"
              }`}
            >
              <span className={`block text-[12px] font-medium ${current ? "text-sky-300" : "text-slate-200"}`}>
                {pkg.title}
              </span>
              {kind && <span className="block text-[10px] text-slate-500">{REQUEST_LABELS[kind]}</span>}
            </button>
          </li>
        );
      })}
    </ul>
  );
}

function ActionButton({ label, pending, onClick }: { label: string; pending: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={pending}
      className="inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-sky-800/70 bg-sky-950/40 px-2.5 py-1.5 text-[12px] font-medium text-sky-300 transition-colors hover:border-sky-600 hover:bg-sky-900/40 disabled:opacity-50"
    >
      {pending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
      {pending ? "Requesting…" : label}
    </button>
  );
}
