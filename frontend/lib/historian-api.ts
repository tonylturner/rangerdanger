import { request } from "./api";
import type { JournalEntry, OperatorJournal, PointKind, PointValue, PointQuality, StaleReason } from "./operator-api";

// Historian preserves the CC step object, including its transient bit.
export type HistorySample = {
  snapshot_seq: number;
  value: PointValue;
  quality: PointQuality;
  stale_reason: StaleReason | null;
  source_time: string | null;
  source_time_invalid: boolean | null;
  cc_received_at: string | null;
  ingested_at: string;
};
export type HistorianPoint = { id: string; ioa: number; kind: PointKind; asdu: string | null; last: HistorySample | null };
export type History = {
  point: string;
  segments: { cache_epoch: string; samples: HistorySample[] }[];
  gaps: { from: string; to: string }[];
};
export type TrendEventMarker = {
  seq: number;
  at: string;
  point: string;
  label: string;
  active: boolean;
  critical: boolean;
  completedCommand: boolean;
};
export type TrendCommandNotice = {
  seq: number;
  at: string;
  label: string;
  result: "rejected" | "failed" | "unknown";
};
export type TrendJournalEvents = {
  cacheEpoch: string;
  markers: TrendEventMarker[];
  commandNotices: TrendCommandNotice[];
};
export const getHistorianPoints = () => request<HistorianPoint[]>("/historian/points");
export function getHistorianHistory(point: string, since: string, until: string) {
  const params = new URLSearchParams({ point, since, until });
  return request<History>(`/historian/history?${params}`);
}

const SWITCH_EVENT_TEXT: Record<string, { label: string; value: (value: unknown) => { text: string; active: boolean; critical: boolean } | null }> = {
  "feeder-breaker-position": { label: "CB1", value: positionEvent },
  "f2-position": { label: "F2", value: positionEvent },
  "rmu-1-position": { label: "RMU1", value: positionEvent },
  "rmu-2-position": { label: "RMU2", value: positionEvent },
  "feeder-protection-trip": { label: "CB1", value: booleanEvent("TRIPPED", "PROTECTION RESET") },
  "f2-protection-trip": { label: "F2", value: booleanEvent("TRIPPED", "PROTECTION RESET") },
  "feeder-lockout": { label: "CB1", value: booleanEvent("LOCKED OUT", "LOCKOUT RESET") },
  "f2-lockout": { label: "F2", value: booleanEvent("LOCKED OUT", "LOCKOUT RESET") },
  "rmu-1-fault-passage": { label: "FPI RMU1", value: faultPassageEvent },
  "rmu-2-fault-passage": { label: "FPI RMU2", value: faultPassageEvent },
};
const SWITCH_COMMANDS: Record<string, string> = {
  "feeder-breaker-command": "CB1",
  "rmu-1-command": "RMU1",
  "rmu-2-command": "RMU2",
};

function positionEvent(value: unknown) {
  if (value === "open" || value === "closed") return { text: value.toUpperCase(), active: value === "open", critical: false };
  return null;
}

function booleanEvent(activeText: string, clearText: string, critical = true) {
  return (value: unknown) => typeof value === "boolean"
    ? { text: value ? activeText : clearText, active: value, critical: value && critical }
    : null;
}

function faultPassageEvent(value: unknown) {
  if (value === true) return { text: "", active: true, critical: false };
  if (value === false) return { text: "RESET", active: false, critical: false };
  return null;
}

function detailString(entry: JournalEntry, key: string): string | null {
  const value = entry.detail[key];
  return typeof value === "string" ? value : null;
}

function commandMatchesStatus(command: JournalEntry, status: JournalEntry): boolean {
  if (command.kind !== "command" || detailString(command, "result") !== "completed") return false;
  const feedback = command.detail.feedback;
  if (typeof feedback !== "object" || feedback === null || Array.isArray(feedback)) return false;
  const fields = feedback as Record<string, unknown>;
  const at = fields.at;
  return typeof fields.ioa === "number" &&
    fields.ioa === status.detail.ioa &&
    typeof at === "string" &&
    Number.isFinite(Date.parse(at)) &&
    Date.parse(at) === Date.parse(status.at) &&
    fields.value === status.detail.value;
}

function commandLabel(entry: JournalEntry): string | null {
  const point = detailString(entry, "point");
  const label = point === null ? undefined : SWITCH_COMMANDS[point];
  const operation = detailString(entry, "operation");
  if (!label || !operation) return null;
  return `${label} ${operation.toUpperCase()}`;
}

// Journal timestamps are CC receive times, while historian snapshots use
// ingest time. Only status observations are switching markers; command
// outcomes are joined by feedback IOA, value and receive timestamp.
export function voltageTrendJournalEvents(journal: OperatorJournal): TrendJournalEvents {
  const statusEvents = journal.entries.filter((entry) => entry.kind === "status_event");
  const markers: TrendEventMarker[] = [];
  for (const entry of statusEvents) {
    const point = detailString(entry, "id");
    const spec = point === null ? undefined : SWITCH_EVENT_TEXT[point];
    const at = Number.isFinite(Date.parse(entry.at)) ? entry.at : null;
    if (!point || !spec || !at) continue;
    const value = spec.value(entry.detail.value);
    if (!value) continue;
    markers.push({
      seq: entry.seq,
      at,
      point,
      label: value.text ? `${spec.label} ${value.text}` : spec.label,
      active: value.active,
      critical: value.critical,
      completedCommand: journal.entries.some((command) => commandMatchesStatus(command, entry)),
    });
  }
  const commandNotices: TrendCommandNotice[] = [];
  for (const entry of journal.entries) {
    if (entry.kind !== "command") continue;
    const label = commandLabel(entry), at = Number.isFinite(Date.parse(entry.at)) ? entry.at : null;
    const result = detailString(entry, "result");
    if (!label || !at || (result !== "rejected" && result !== "failed" && result !== "unknown")) continue;
    commandNotices.push({ seq: entry.seq, at, label, result });
  }
  return { cacheEpoch: journal.cache_epoch, markers, commandNotices: commandNotices.slice(-5).reverse() };
}

export function numericSample(sample: HistorySample): number | null {
  const q = sample.quality;
  if (sample.stale_reason !== null || q.iv || q.nt || q.sb || q.bl || q.ov) return null;
  if (typeof sample.value === "number" && Number.isFinite(sample.value)) return sample.value;
  if (typeof sample.value === "object" && sample.value !== null) return sample.value.transient || !Number.isFinite(sample.value.value) ? null : sample.value.value;
  if (typeof sample.value === "boolean") return sample.value ? 1 : 0;
  return null;
}

// Never bridge an epoch, explicit ingest gap, invalid sample, or an
// unknown. Use ingest time on the x axis: station-clock skew is evidence,
// not a reason to reorder samples or erase a gap.
export function historyRuns(history: History): HistorySample[][] {
  const runs: HistorySample[][] = [];
  for (const segment of history.segments) {
    let run: HistorySample[] = [];
    for (const sample of segment.samples) {
      const t = Date.parse(sample.ingested_at);
      if (numericSample(sample) === null || !Number.isFinite(t)) {
        if (run.length) runs.push(run);
        run = [];
        continue;
      }
      const previous = run.at(-1);
      const previousTime = previous ? Date.parse(previous.ingested_at) : t;
      const crossesGap = history.gaps.some((gap) => Date.parse(gap.from) < t && Date.parse(gap.to) > previousTime);
      if (previous && (crossesGap || t <= previousTime)) {
        runs.push(run);
        run = [];
      }
      run.push(sample);
    }
    if (run.length) runs.push(run);
  }
  return runs;
}
