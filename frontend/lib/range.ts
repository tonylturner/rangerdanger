// The range lifecycle as GET /api/range reports it, and the client-side
// rules built on it: which step a transition is on, how often to poll,
// when the range a page shows has changed, and which cached queries
// belong to a range.
//
// One range runs at a time. Switching packages (or restarting the
// active one) replaces the whole range: every lab container restarts,
// terminals close and the firewall policy resets to the package
// default. Curriculum progress survives because browser storage is
// keyed by package (curriculum-storage.ts).

import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { errorMessage } from "./utils";

export const RANGE_PHASES = [
  "none",
  "preflight",
  "stopping",
  "starting",
  "configuring",
  "ready",
  "failed",
] as const;
export type RangePhase = (typeof RANGE_PHASES)[number];

// `target` is set only in preflight; from stopping on, `package` is
// already the package being brought up.
export type RangeStatus = {
  generation: number;
  phase: RangePhase;
  package: string;
  target: string;
  mode: string;
  error: string;
  // null in phase none.
  updated_at: string | null;
};

// The four steps a transition walks through, in order.
export const TRANSITION_STEPS = ["preflight", "stopping", "starting", "configuring"] as const;
export type TransitionStep = (typeof TRANSITION_STEPS)[number];

const STEP_LABELS: Record<TransitionStep, string> = {
  preflight: "Checking the package and its images",
  stopping: "Stopping the current range",
  starting: "Starting lab containers",
  configuring: "Configuring routes and firewall policy",
};

export function isRangePhase(value: unknown): value is RangePhase {
  return typeof value === "string" && (RANGE_PHASES as readonly string[]).includes(value);
}

export function isTransitioning(phase: RangePhase): phase is TransitionStep {
  return (TRANSITION_STEPS as readonly string[]).includes(phase);
}

// 1-based position of a transition step, for "step 2 of 4".
export function transitionStepNumber(step: TransitionStep): number {
  return TRANSITION_STEPS.indexOf(step) + 1;
}

export function transitionStepLabel(step: TransitionStep): string {
  return STEP_LABELS[step];
}

// The package a transition is bringing up.
export function transitionPackage(status: RangeStatus): string {
  return status.phase === "preflight" ? status.target : status.package;
}

// Fast while a transition runs so each step shows as it happens; slow
// otherwise, only to notice a change made from another tab.
export const TRANSITION_POLL_MS = 1000;
export const IDLE_POLL_MS = 10000;

export function rangePollInterval(phase: RangePhase | undefined): number {
  return phase !== undefined && isTransitioning(phase) ? TRANSITION_POLL_MS : IDLE_POLL_MS;
}

// What the browser remembers between two GET /api/range answers.
// serving is null until the first answer arrives.
export type RangeObservation = {
  serving: boolean | null;
  readyGeneration: number | null;
};

export const INITIAL_OBSERVATION: RangeObservation = { serving: null, readyGeneration: null };

// Decides whether the range the page shows has changed: it stopped
// serving (a transition reached stopping, or the range failed), it
// started serving (a new range is ready), or a different generation is
// ready (the transition happened while this tab was not polling).
// Either way cached data and component state describe a range that is
// gone. Range routes answer in ready and, because preflight leaves the
// old range untouched, in preflight after a ready range. The first
// answer is only a baseline.
export function observeRange(
  prev: RangeObservation,
  status: RangeStatus,
): { next: RangeObservation; changed: boolean } {
  const serving = status.phase === "ready" || (status.phase === "preflight" && (prev.serving ?? true));
  const readyGeneration = status.phase === "ready" ? status.generation : prev.readyGeneration;
  const next = { serving, readyGeneration };
  if (prev.serving === null) return { next, changed: false };
  const newGeneration =
    status.phase === "ready" && prev.readyGeneration !== null && prev.readyGeneration !== status.generation;
  return { next, changed: prev.serving !== serving || newGeneration };
}

// The query key of GET /api/range. It is the only cached query that
// outlives a range: everything else (packages, scenarios, workshop
// graph and status, firewall, traffic, captures) is read
// from the package or the range that served it, so a deny-list keeps
// queries added later scoped by default.
export const RANGE_QUERY_KEY = ["range"] as const;

export function isRangeScopedQuery(queryKey: QueryKey): boolean {
  return queryKey[0] !== RANGE_QUERY_KEY[0];
}

// Drops every range- or package-scoped query back to its initial state
// and refetches the ones in use, so no view keeps showing data from a
// range that is gone while the new answers load.
export function resetRangeScopedQueries(queryClient: QueryClient): Promise<void> {
  return queryClient.resetQueries({ predicate: (query) => isRangeScopedQuery(query.queryKey) });
}

// Range-bound routes answer 503 {"error":"range not ready","phase"}
// while no range is serving. This is an expected state, not a failure.
export class RangeNotReadyError extends Error {
  readonly phase: RangePhase | null;

  constructor(phase: RangePhase | null) {
    super(rangeNotReadyText(phase));
    this.name = "RangeNotReadyError";
    this.phase = phase;
  }
}

export function isRangeNotReady(error: unknown): error is RangeNotReadyError {
  return error instanceof RangeNotReadyError;
}

export const RANGE_NOT_READY = "range not ready";
// A range-bound request whose generation began stopping while it ran.
export const RANGE_STOPPING = "range is stopping";

// Recognises the two 503 bodies of docs/api-spec.md; any other 503
// stays an ordinary failure.
export function parseRangeNotReady(body: unknown): RangeNotReadyError | null {
  if (typeof body !== "object" || body === null) return null;
  const { error, phase } = body as { error?: unknown; phase?: unknown };
  if (error === RANGE_STOPPING) return new RangeNotReadyError("stopping");
  if (error !== RANGE_NOT_READY) return null;
  return new RangeNotReadyError(isRangePhase(phase) ? phase : null);
}

// Every API call reports a not-ready answer here, so the range status
// refreshes at once instead of waiting for its next poll.
type NotReadyListener = (error: RangeNotReadyError) => void;
const notReadyListeners = new Set<NotReadyListener>();

export function onRangeNotReady(listener: NotReadyListener): () => void {
  notReadyListeners.add(listener);
  return () => {
    notReadyListeners.delete(listener);
  };
}

export function reportRangeNotReady(error: RangeNotReadyError): void {
  notReadyListeners.forEach((listener) => listener(error));
}

export function rangeNotReadyText(phase: RangePhase | null): string {
  if (phase !== null && isTransitioning(phase)) {
    return `The range is being replaced (${transitionStepLabel(phase).toLowerCase()}). This works again once it is ready.`;
  }
  if (phase === "failed") return "The range did not start. Retry it from the Range control.";
  return "No range is running. Start it from the Range control.";
}

// One line for a command log: calm for a range that is not ready, an
// error otherwise.
export function logLineFor(error: unknown, prefix = ""): string {
  if (isRangeNotReady(error)) return `[RANGE] ${error.message}`;
  return `[ERROR] ${prefix}${errorMessage(error)}`;
}

// The request a student can make from the current state, if any.
export type RangeRequestKind = "start" | "restart" | "switch" | "retry";

export function requestKindFor(status: RangeStatus, packageId: string): RangeRequestKind | null {
  if (isTransitioning(status.phase)) return null;
  if (status.phase === "none") return "start";
  if (status.phase === "failed") return packageId === status.package ? "retry" : "switch";
  return packageId === status.package ? "restart" : "switch";
}

// Restart and switch throw away a running range's state, so they ask
// first; returns the question to ask, or null to go ahead. A failed
// range has nothing left to lose.
export function confirmationFor(kind: RangeRequestKind, phase: RangePhase): "restart" | "switch" | null {
  if (phase !== "ready") return null;
  return kind === "restart" || kind === "switch" ? kind : null;
}

export const REQUEST_LABELS: Record<RangeRequestKind, string> = {
  start: "Start range",
  restart: "Restart range",
  switch: "Switch range",
  retry: "Retry",
};

// What a restart or switch does, stated before the student agrees.
export function confirmationConsequences(kind: "restart" | "switch", packageTitle: string): string[] {
  return [
    "Every lab container restarts, so the range is unavailable for a few minutes.",
    "Open terminals close.",
    `The firewall policy resets to the ${packageTitle} default; a policy you built or applied is not kept.`,
    kind === "switch"
      ? "Your curriculum progress is kept for each package, so switching back picks up where you left off."
      : "Your curriculum progress is kept.",
  ];
}
