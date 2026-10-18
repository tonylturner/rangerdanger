"use client";

import type { OperatorPoint } from "../lib/operator-api";
import type { DeviceId } from "../lib/operator-workstation";
import { controlCentreObservations } from "../lib/observation-readings";
import { FeederOneLine } from "./feeder-one-line";

export function ControlCentreOneLine({ points, ...props }: { points: OperatorPoint[]; unavailable?: boolean; selected?: DeviceId; onSelect?: (id: DeviceId) => void }) {
  return <FeederOneLine read={controlCentreObservations(points, props.unavailable)} {...props} />;
}
