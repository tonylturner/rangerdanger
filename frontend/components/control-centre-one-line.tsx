"use client";

import type { OperatorPoint } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { controlCentreObservations } from "../lib/physical-readings";
import { FeederOneLine } from "./feeder-one-line";

// Retained for callers inspecting a single CC observation.
export function observedReading(points: OperatorPoint[], id: string, unavailable = false) {
  return controlCentreObservations(points, unavailable)(id);
}
export function ControlCentreOneLine({ points, ...props }: { points: OperatorPoint[]; unavailable?: boolean; selected?: DeviceId; onSelect?: (id: DeviceId) => void }) {
  return <FeederOneLine read={controlCentreObservations(points, props.unavailable)} {...props} />;
}
