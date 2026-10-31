// What the control-centre screen says about a point, the link, a
// command and a journal entry. Pure functions so the wording that
// carries the lesson (a stale value stays visible and says why) is
// unit-tested.

import { ApiError, type WorkshopControlCentre } from "./api";
import type {
  CommandResult,
  ControlCentreLink,
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
  "feeder-current": { label: "CB1 current", unit: "A", digits: 1 },
  "feeder-voltage": { label: "CB1 line-side VT", unit: "kV", digits: 2 },
  "f2-position": { label: "F2 breaker position" },
  "f2-protection-trip": { label: "F2 protection trip", states: ["NORMAL", "TRIPPED"] },
  "f2-current": { label: "F2 current", unit: "A", digits: 1 },
  "f2-p-mw": { label: "F2 active power P", unit: "MW", digits: 2 },
  "oltc-1-tap": { label: "T1 completed tap", digits: 0 },
  "oltc-1-auto": { label: "OLTC mode", states: ["MANUAL", "AUTO"] },
  "oltc-1-bus-kv": { label: "20 kV busbar (T1 secondary)", unit: "kV", digits: 2 },
  "oltc-1-current": { label: "T1 20 kV terminal current", unit: "A", digits: 1 },
  "oltc-1-remote": { label: "OLTC authority", states: ["LOCAL", "REMOTE"] },
  "oltc-1-at-lower": { label: "OLTC lower limit", states: ["CLEAR", "AT LIMIT"] },
  "oltc-1-at-upper": { label: "OLTC upper limit", states: ["CLEAR", "AT LIMIT"] },
  "oltc-1-blocked": { label: "OLTC blocked", states: ["CLEAR", "BLOCKED"] },
  "plc-1-running": { label: "OpenPLC running", states: ["STOPPED", "RUNNING"] },
  "plc-1-input-valid": { label: "OpenPLC input", states: ["INVALID", "VALID"] },
  "plc-1-setpoint-kv": { label: "OpenPLC setpoint", unit: "kV", digits: 2 },
  "plc-1-deadband-pct": { label: "OpenPLC deadband", unit: "%", digits: 1 },
  "plc-1-delay-s": { label: "Configured delay", unit: "s", digits: 0 },
};

for (const prefix of ["feeder", "oltc-1"]) {
  const location = prefix === "feeder" ? "CB1" : "T1 20 kV terminal";
  POINT_META[`${prefix}-p-mw`] = { label: `${location} active power P`, unit: "MW", digits: 2 };
  POINT_META[`${prefix}-q-mvar`] = { label: `${location} reactive power Q`, unit: "Mvar", digits: 2 };
  POINT_META[`${prefix}-pf`] = { label: `${location} power factor PF`, digits: 3 };
}
POINT_META["oltc-1-overcurrent-block"] = { label: "OLTC overcurrent block", states: ["CLEAR", "BLOCKED"] };
for (const id of ["rmu-1", "rmu-2"]) {
  POINT_META[`${id}-position`] = { label: `${id.toUpperCase()} position` };
  POINT_META[`${id}-remote`] = { label: `${id.toUpperCase()} authority`, states: ["LOCAL", "REMOTE"] };
  POINT_META[`${id}-earth`] = { label: `${id.toUpperCase()} earth switch`, states: ["OPEN", "CLOSED"] };
  POINT_META[`${id}-fault-passage`] = { label: `${id.toUpperCase()} fault passage`, states: ["CLEAR", "LATCHED"] };
  const location = id === "rmu-1" ? "RMU1" : "RMU2";
  POINT_META[`${id}-current`] = { label: `${location} current`, unit: "A", digits: 1 };
  POINT_META[`${id}-source-kv`] = { label: `${location} ${id === "rmu-1" ? "S1" : "S3"} side`, unit: "kV", digits: 2 };
  POINT_META[`${id}-section-kv`] = { label: `${location} S2 bus VT`, unit: "kV", digits: 2 };
}

export function pointLabel(id: string): string {
  return POINT_META[id]?.label ?? id;
}

// GOOD is the full IEC quality predicate: no flags and no stale reason.
// stale: the CC keeps a value it can no longer vouch for.
// never: nothing received since the CC booted.
export type PointCondition = "good" | "stale" | "never";

export const QUALITY_DEFINITIONS = {
  GOOD: "IEC 104 data quality: the value is valid and current. It does not say the plant is in its normal state.",
  STALE: "IEC 104 data quality: the retained value is not established as valid and current; inspect its flags and stale reason.",
  UNKNOWN: "IEC 104 data quality: no value has been received; the plant state is not established.",
  INVALID: "IEC 104 data quality: the source marks the value invalid; it cannot establish the plant state.",
} as const;
export function qualityDefinition(condition: "good" | "stale" | "unknown" | "never" | "invalid"): string {
  return QUALITY_DEFINITIONS[condition === "never" ? "UNKNOWN" : condition.toUpperCase() as keyof typeof QUALITY_DEFINITIONS];
}

export function pointCondition(point: OperatorPoint): PointCondition {
  if (point.value === null) return "never";
  if (qualityFlags(point.quality).length > 0 || point.stale_reason !== null) return "stale";
  return "good";
}

// Flags as text in IEC order; float measurements and step positions carry OV.
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
  device_unreachable: "RTU cannot reach the device (Modbus)",
  measurement_invalid: "Device reports measurements invalid",
  never_received: "Never received",
  heartbeat: "PLC heartbeat stopped",
  not_updated: "Cyclic reporting watchdog expired",
};

export function staleReasonText(reason: StaleReason | null): string {
  return reason === null ? "" : STALE_REASON_TEXT[reason] ?? reason;
}

// The value as the operator reads it. A stale value is formatted
// exactly like a good one: the screen marks it stale, it never
// replaces it. PF IV is the exception: its wire zero is a placeholder,
// not a power factor. Only a point never received has no value.
export function formatPointValue(id: string, value: PointValue, quality?: PointQuality): string {
  if (id.endsWith("-pf") && (quality?.iv || value === null)) return "—";
  if (value === null) return "not received";
  if (typeof value === "object") return `${value.value}${value.transient ? " · TRANSIENT" : ""}`;
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

// The control centre's link and GI as GET /api/workshop/status reports
// them.
export function controlCentreLinkText(cc: WorkshopControlCentre): string {
  if (cc.link === null) return "NOT ANSWERING";
  const gi = cc.gi === null ? "" : ` · GI ${GI_STATE_TEXT[cc.gi]}`;
  return `${LINK_STATE_TEXT[cc.link]}${gi}`;
}

// online: link started and GI complete; degraded: on its way there;
// offline: no link, or the control centre does not answer.
export function controlCentreHealth(cc: WorkshopControlCentre | undefined): "online" | "degraded" | "offline" {
  if (!cc || cc.link === null || cc.link === "disconnected") return "offline";
  if (cc.link === "connecting") return "degraded";
  return cc.gi === "complete" ? "online" : "degraded";
}

// ── Commands ─────────────────────────────────────────────────────

export type Tone = "ok" | "bad" | "warn";

export type CommandOutcome = { tone: Tone; title: string; explanation: string | null };

export function commandOutcome(command: CommandResult): CommandOutcome {
  return {
    tone: command.result === "completed" ? "ok" : command.result === "unknown" ? "warn" : "bad",
    title: command.result.toUpperCase(),
    explanation: command.reason,
  };
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
        return `Control centre refused the command${detail}. Nothing was sent.`;
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

function journalValue(detail: Record<string, unknown>): string {
  const point = typeof detail.point === "string" ? detail.point : "";
  const value = detail.value;
  const valid = value === null || typeof value === "string" || typeof value === "boolean" || typeof value === "number" || (typeof value === "object" && value !== null && "value" in value && "transient" in value);
  return valid ? formatPointValue(point, value as PointValue) : "—";
}

export const LINK_DOWN_REASON_TEXT: Record<LinkDownReason, string> = {
  t1_timeout: "nothing received from the RTU for t1",
  peer_closed: "the RTU end closed or reset the connection",
  connect_failed: "TCP connect or STARTDT failed",
  io_error: "the control centre closed it after a failed general interrogation",
  operator_reconnect: "an operator requested a fresh session",
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
      return `General interrogation (${str(detail, "trigger")}) started on link #${str(detail, "connection_seq")}`;
    case "gi_complete":
      return `General interrogation (${str(detail, "trigger")}) complete on link #${str(detail, "connection_seq")}, ${str(detail, "points_good")} points good`;
    case "status_event":
      return `${pointLabel(str(detail, "id"))}: ${formatPointValue(str(detail, "id"), detail.value as PointValue)} · ${str(detail, "asdu")} COT ${str(detail, "cot")} · RTU ${detail.source_time ?? "untimed"}${detail.source_time_invalid ? " · time IV" : ""}`;
    case "point_stale": {
      const reason = typeof detail.stale_reason === "string" ? detail.stale_reason : null;
      return `${pointLabel(str(detail, "point"))} STALE (${staleReasonText(reason as StaleReason | null) || "—"}), keeps ${journalValue(detail)}`;
    }
    case "point_good":
      return `${pointLabel(str(detail, "point"))} good: ${journalValue(detail)}`;
    case "command":
      // The contract freezes POST results, not journal command details.
      // Preserve the complete record instead of inventing phase semantics.
      return `Command ${str(detail, "point")} ${str(detail, "operation").toUpperCase()}: ${JSON.stringify(detail)}`;
    default:
      return `${kind} ${JSON.stringify(detail)}`;
  }
}

export function journalTone(kind: string): Tone | null {
  if (kind === "link_down" || kind === "point_stale") return "warn";
  if (kind === "link_up" || kind === "gi_complete" || kind === "point_good") return "ok";
  return null;
}

// The journal's sequence range; both bounds are null while it is empty.
export function journalRangeText(oldest: number | null, latest: number | null): string {
  return oldest === null || latest === null ? "empty" : `seq ${oldest}–${latest}`;
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
