// Which operator screen the portal shows. The backend decides: every
// package declares an operator_view and GET /api/packages reports it
// for the active package. The browser never picks one itself and never
// falls back to another view: a value this build does not know is an
// error the student sees.

import type { PackageSummary } from "./api";
import { errorMessage } from "./utils";

export const US_OPERATOR_VIEW = "us-dnp3-substation";
export const EU_OPERATOR_VIEW = "eu-iec104";

export const OPERATOR_VIEWS = [US_OPERATOR_VIEW, EU_OPERATOR_VIEW] as const;
export type OperatorView = (typeof OPERATOR_VIEWS)[number];

export type OperatorViewResolution =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "unknown"; packageId: string; value: string }
  | { kind: "known"; view: OperatorView; pkg: PackageSummary };

export function isOperatorView(value: unknown): value is OperatorView {
  return typeof value === "string" && (OPERATOR_VIEWS as readonly string[]).includes(value);
}

// A package that is already known wins over a later failed refetch:
// the view of the active package does not change while it stays active.
export function resolveOperatorView(pkg: PackageSummary | undefined, error: unknown): OperatorViewResolution {
  if (pkg) {
    const value: unknown = pkg.operator_view;
    if (isOperatorView(value)) return { kind: "known", view: value, pkg };
    return { kind: "unknown", packageId: pkg.id, value: typeof value === "string" ? value : "" };
  }
  if (error) return { kind: "error", message: errorMessage(error) };
  return { kind: "loading" };
}

export function isView(resolution: OperatorViewResolution, view: OperatorView): boolean {
  return resolution.kind === "known" && resolution.view === view;
}

// US RTAC reads (/api/substation/*) only make sense for the US view;
// they stay off while the view is loading, unknown or another one.
export function usFeederPolling(resolution: OperatorViewResolution): boolean {
  return isView(resolution, US_OPERATOR_VIEW);
}

export function operatorNavLabel(resolution: OperatorViewResolution): string {
  return isView(resolution, EU_OPERATOR_VIEW) ? "Control Centre" : "Feeder HMI";
}

const MAP_HINT = "Click a node to inspect, open UI, or terminal.";

// The network map header: the US substation's own words, else the
// package title once it is known.
export function networkMapHeading(resolution: OperatorViewResolution): { title: string; subtitle: string } {
  if (isView(resolution, US_OPERATOR_VIEW)) {
    return { title: "Substation Network Map", subtitle: `Distribution co-op feeder topology. ${MAP_HINT}` };
  }
  if (resolution.kind === "known") return { title: "Network Map", subtitle: `${resolution.pkg.title} topology. ${MAP_HINT}` };
  return { title: "Network Map", subtitle: MAP_HINT };
}

// Text of the panel shown instead of an operator screen.
export function operatorViewProblem(resolution: OperatorViewResolution): string | null {
  switch (resolution.kind) {
    case "error":
      return `The active package could not be read: ${resolution.message}`;
    case "unknown":
      return resolution.value === ""
        ? `Package ${resolution.packageId} declares no operator view.`
        : `Package ${resolution.packageId} declares the operator view "${resolution.value}", which this portal does not know.`;
    default:
      return null;
  }
}
