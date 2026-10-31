import type { Reading } from "./observation-readings";

// The solver treats residual voltages below this boundary as de-energized.
export const LIVE_KV = 0.1;
export type VoltageState = "LIVE" | "DEAD" | "UNKNOWN";

export function voltageState(reading: Pick<Reading, "condition" | "value">): VoltageState {
  const { condition, value } = reading;
  if (condition !== "good" || typeof value !== "number" || !Number.isFinite(value) || value < 0) return "UNKNOWN";
  return value >= LIVE_KV ? "LIVE" : "DEAD";
}
