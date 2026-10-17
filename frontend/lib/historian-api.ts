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
export const getHistorianPoints = () => request<HistorianPoint[]>("/historian/points");
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
