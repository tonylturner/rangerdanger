// What the control-centre screen says about a point, the link, a
// command and a journal entry. Pure functions so the wording that
// carries the lesson (a stale value stays visible and says why) is
// unit-tested.

import { ApiError } from "./api";
import type {
  CommandPhaseStatus,
  CommandResult,
  ControlCentreLink,
  DeviceState,
  LinkDownReason,
  OperatorPoint,
  PointQuality,
  PointValue,
  StaleReason,
} from "./operator-api";
import { isRangeNotReady } from "./range";
import { errorMessage } from "./utils";

// ── Points ───────────────────────────────────────────────────────

type PointMeta = {
  label: string;
  unit?: string;
  digits?: number;
  // Single points: the words for false and true.
  states?: [string, string];
};

// Display names for the monitored points of points.json. A point the
// CC reports that is not listed here shows under its id.
export const POINT_META: Record<string, PointMeta> = {
  "feeder-breaker-position": { label: "Feeder breaker position" },
  "feeder-protection-trip": { label: "Protection trip", states: ["NORMAL", "TRIPPED"] },
  "feeder-current": { label: "Feeder current", unit: "A", digits: 1 },
  "feeder-voltage": { label: "Feeder voltage (L-L)", unit: "kV", digits: 2 },
};

export function pointLabel(id: string): string {
  return POINT_META[id]?.label ?? id;
}

// good: last report on the current connection had IV and NT clear.
// stale: the CC keeps a value it can no longer vouch for.
// never: nothing received since the CC booted.
export type PointCondition = "good" | "stale" | "never";

export function pointCondition(point: OperatorPoint): PointCondition {
  if (point.value === null) return "never";
  if (point.quality.iv || point.quality.nt || point.stale_reason !== null) return "stale";
  return "good";
}

// Flags as text in IEC order; OV only exists for measurements.
export function qualityFlags(quality: PointQuality): string[] {
  const flags: string[] = [];
  if (quality.iv) flags.push("IV");
  if (quality.nt) flags.push("NT");
  if (quality.sb) flags.push("SB");
  if (quality.bl) flags.push("BL");
  if (quality.ov === true) flags.push("OV");
  return flags;
}

export const STALE_REASON_TEXT: Record<StaleReason, string> = {
  link_lost: "IEC 104 link to the RTU lost",
  device_unreachable: "RTU cannot reach the breaker (Modbus)",
  measurement_invalid: "Breaker reports measurements invalid",
  never_received: "Never received",
};

export function staleReasonText(reason: StaleReason | null): string {
  return reason === null ? "" : STALE_REASON_TEXT[reason] ?? reason;
}

// The value as the operator reads it. A stale value is formatted
// exactly like a good one: the screen marks it stale, it never
// replaces it. Only a point never received has no value.
export function formatPointValue(id: string, value: PointValue): string {
  if (value === null) return "not received";
  const meta = POINT_META[id];
  if (typeof value === "string") return value.toUpperCase();
  if (typeof value === "boolean") {
    const [off, on] = meta?.states ?? ["OFF", "ON"];
    return value ? on : off;
  }
  const text = value.toFixed(meta?.digits ?? 2);
  return meta?.unit ? `${text} ${meta.unit}` : text;
}

// ── Times ────────────────────────────────────────────────────────

// HH:MM:SS.mmm in UTC: the RTU stamps observations in UTC from the
// station clock, so the screen does not convert them.
export function formatUtcTime(iso: string | null): string {
  if (iso === null) return "—";
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return iso;
  return new Date(ms).toISOString().slice(11, 23);
}

export function ageMs(iso: string | null, now: number): number | null {
  if (iso === null) return null;
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? null : Math.max(0, now - ms);
}

export function formatAge(ms: number | null): string {
  if (ms === null) return "—";
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)} s`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ${String(s % 60).padStart(2, "0")} s`;
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")} min`;
}

// ── Link ─────────────────────────────────────────────────────────

export const LINK_STATE_TEXT: Record<ControlCentreLink["state"], string> = {
  started: "STARTED",
  connecting: "CONNECTING",
  disconnected: "DISCONNECTED",
};

export const GI_STATE_TEXT: Record<ControlCentreLink["gi"]["state"], string> = {
  none: "NOT RUN",
  running: "RUNNING",
  complete: "COMPLETE",
};

// ── Commands ─────────────────────────────────────────────────────

export type Tone = "ok" | "bad" | "warn";

export type CommandOutcome = { tone: Tone; title: string; explanation: string };

export function commandOutcome(result: CommandResult): CommandOutcome {
  switch (result.status) {
    case "completed":
      return {
        tone: "ok",
        title: "COMPLETED",
        explanation: `Select and execute confirmed; the RTU reported the breaker ${formatPointValue("feeder-breaker-position", result.feedback.value)} (COT 11).`,
      };
    case "rejected": {
      const phase = result.select.status === "negative" ? "select" : "execute";
      return {
        tone: "bad",
        title: "REJECTED",
        explanation: `The RTU answered the ${phase} with a negative confirmation. The breaker was not operated.`,
      };
    }
    case "failed":
      return {
        tone: "bad",
        title: "FAILED",
        explanation:
          "Execute was never sent: the link was lost or the select got no confirmation. The breaker was not commanded.",
      };
    case "unknown":
      return {
        tone: "warn",
        title: "UNKNOWN",
        explanation:
          "Execute was sent but the outcome is not proven (no confirmation, or no matching COT 11 feedback). It is not retried; check the breaker position.",
      };
  }
}

export function phaseStatusText(status: CommandPhaseStatus): string {
  return status === null ? "not sent" : status.toUpperCase();
}

export function phaseTone(status: CommandPhaseStatus | "matched" | "mismatched" | "missing"): Tone | null {
  if (status === "confirmed" || status === "matched") return "ok";
  if (status === "negative" || status === "mismatched") return "bad";
  if (status === "timeout" || status === "missing") return "warn";
  return null;
}

// A command the CC refused to start, or a request that never reached it.
export function commandErrorText(error: unknown): string {
  if (isRangeNotReady(error)) return error.message;
  if (error instanceof ApiError) {
    const detail = error.detail ? `: ${error.detail}` : "";
    switch (error.status) {
      case 400:
        return `Command refused as malformed${detail}.`;
      case 404:
        return `The active package has no control centre${detail}.`;
      case 409:
        return `Another command is still in progress${detail}. Nothing was sent.`;
      case 503:
        return `The control centre has no started link to the RTU${detail}. Nothing was sent.`;
      default:
        return `Control centre answered ${error.status}${detail}. The outcome is not known.`;
    }
  }
  return `The command request failed (${errorMessage(error)}). The outcome is not known.`;
}

// ── Journal ──────────────────────────────────────────────────────

function str(detail: Record<string, unknown>, key: string): string {
  const value = detail[key];
  if (value === null || value === undefined) return "—";
  return typeof value === "string" ? value : JSON.stringify(value);
}

function nestedStatus(detail: Record<string, unknown>, key: string): string {
  const value = detail[key];
  if (typeof value !== "object" || value === null) return "—";
  const status = (value as { status?: unknown }).status;
  return typeof status === "string" ? status : "—";
}

function journalValue(detail: Record<string, unknown>): string {
  const point = typeof detail.point === "string" ? detail.point : "";
  const value = detail.value;
  const valid = value === null || typeof value === "string" || typeof value === "boolean" || typeof value === "number";
  return valid ? formatPointValue(point, value as PointValue) : "—";
}

export const LINK_DOWN_REASON_TEXT: Record<LinkDownReason, string> = {
  t1_timeout: "nothing received from the RTU for t1",
  peer_closed: "the RTU end closed or reset the connection",
  connect_failed: "TCP connect or STARTDT failed",
  io_error: "the control centre closed it after a failed general interrogation",
};

function linkDownReasonText(reason: string): string {
  const text = (LINK_DOWN_REASON_TEXT as Record<string, string | undefined>)[reason];
  return text ? `${text} (${reason})` : reason;
}

export function journalSummary(kind: string, detail: Record<string, unknown>): string {
  switch (kind) {
    case "link_up":
      return `Link up #${str(detail, "connection_seq")} to ${str(detail, "peer")}`;
    case "link_down":
      return `Link down #${str(detail, "connection_seq")}: ${linkDownReasonText(str(detail, "reason"))}`;
    case "gi_started":
      return `General interrogation started on link #${str(detail, "connection_seq")}`;
    case "gi_complete":
      return `General interrogation complete on link #${str(detail, "connection_seq")}, ${str(detail, "points_good")} points good`;
    case "point_stale": {
      const reason = typeof detail.stale_reason === "string" ? detail.stale_reason : null;
      return `${pointLabel(str(detail, "point"))} STALE (${staleReasonText(reason as StaleReason | null) || "—"}), keeps ${journalValue(detail)}`;
    }
    case "point_good":
      return `${pointLabel(str(detail, "point"))} good: ${journalValue(detail)}`;
    case "command":
      return `Command ${str(detail, "operation").toUpperCase()} ${str(detail, "status").toUpperCase()} (select ${nestedStatus(detail, "select")}, execute ${nestedStatus(detail, "execute")}, feedback ${nestedStatus(detail, "feedback")})`;
    default:
      return `${kind} ${JSON.stringify(detail)}`;
  }
}

export function journalTone(kind: string, detail: Record<string, unknown>): Tone | null {
  if (kind === "link_down" || kind === "point_stale") return "warn";
  if (kind === "link_up" || kind === "gi_complete" || kind === "point_good") return "ok";
  if (kind === "command") return detail.status === "completed" ? "ok" : detail.status === "unknown" ? "warn" : "bad";
  return null;
}

// The journal's sequence range; both bounds are null while it is empty.
export function journalRangeText(oldest: number | null, latest: number | null): string {
  return oldest === null || latest === null ? "empty" : `seq ${oldest}–${latest}`;
}

// ── Instructor ───────────────────────────────────────────────────

// The device state a fault or reset answered with.
export function deviceStateSummary(state: DeviceState): string {
  const yes = (value: boolean) => (value ? "YES" : "no");
  return [
    `breaker ${state.position.toUpperCase()}`,
    `fault ${yes(state.fault_active)}`,
    `lockout ${yes(state.lockout)}`,
    `protection tripped ${yes(state.protection_tripped)}`,
    `revision ${state.revision}`,
  ].join(", ");
}

// ── Summary ──────────────────────────────────────────────────────

export type TelemetrySummary = { total: number; good: number; stale: number; never: number };

export function telemetrySummary(points: OperatorPoint[]): TelemetrySummary {
  const summary = { total: points.length, good: 0, stale: 0, never: 0 };
  for (const point of points) summary[pointCondition(point)] += 1;
  return summary;
}

export function telemetryText(summary: TelemetrySummary): string {
  if (summary.total === 0) return "no points";
  if (summary.good === summary.total) return `${summary.good}/${summary.total} good`;
  const parts = [`${summary.good}/${summary.total} good`];
  if (summary.stale > 0) parts.push(`${summary.stale} STALE`);
  if (summary.never > 0) parts.push(`${summary.never} not received`);
  return parts.join(" · ");
}
