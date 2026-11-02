import { request } from "./api";
import type { PointKind, PointValue, PointQuality, StaleReason } from "./operator-api";

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
export type HistorianEvent = { point: string; at: string; from: PointValue; to: PointValue; quality: PointQuality };
export type HistorianEvents = { events: HistorianEvent[]; gaps: History["gaps"] };
export const getHistorianPoints = () => request<HistorianPoint[]>("/historian/points");
export function getHistorianEvents(since: string, until: string) {
  return request<HistorianEvents>(`/historian/events?${new URLSearchParams({ since, until })}`);
}
export function getHistorianHistory(point: string, since: string, until: string) {
  const params = new URLSearchParams({ point, since, until });
  return request<History>(`/historian/history?${params}`);
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

const MAJOR_STATUS = new Set([
  "oltc-1-tap", "oltc-1-auto", "oltc-1-remote", "oltc-1-blocked", "oltc-1-overcurrent-block",
  "plc-1-running", "feeder-breaker-position", "f2-position", "rmu-1-position", "rmu-2-position",
  "feeder-protection-trip", "f2-protection-trip", "rmu-1-earth", "rmu-2-earth",
  "rmu-1-fault-passage", "rmu-2-fault-passage",
]);
export function majorHistorianEvents(events: HistorianEvent[]): HistorianEvent[] {
  return events.filter((event) => MAJOR_STATUS.has(event.point));
}

export type HistoryBreak = { from: string; to: string; reasons: string[] };
function breakReason(sample: HistorySample): string {
  if (sample.stale_reason) return sample.stale_reason;
  const flags = Object.entries(sample.quality).filter(([, value]) => value).map(([flag]) => flag.toUpperCase());
  if (flags.length) return `Quality ${flags.join(" ")}`;
  if (typeof sample.value === "object" && sample.value?.transient) return "Tap moving";
  return "Unknown or non-numeric observation";
}

// Invalid intervals end at the next good observation in the same epoch,
// never across feed silence. The final sample is an observation, not a
// claim that the outage has ended.
export function historyBreaks(history: History): HistoryBreak[] {
  const breaks: HistoryBreak[] = [];
  for (const segment of history.segments) {
    let current: HistoryBreak | undefined;
    let previous: HistorySample | undefined;
    for (const sample of segment.samples) {
      const t = Date.parse(sample.ingested_at);
      if (!Number.isFinite(t)) continue;
      if (previous && history.gaps.some((gap) => Date.parse(gap.from) < t && Date.parse(gap.to) > Date.parse(previous!.ingested_at))) current = undefined;
      if (numericSample(sample) === null) {
        if (!current) {
          current = { from: sample.ingested_at, to: sample.ingested_at, reasons: [] };
          breaks.push(current);
        }
        current.to = sample.ingested_at;
        const reason = breakReason(sample);
        if (!current.reasons.includes(reason)) current.reasons.push(reason);
      } else if (current) {
        current.to = sample.ingested_at;
        current = undefined;
      }
      previous = sample;
    }
  }
  return breaks;
}
