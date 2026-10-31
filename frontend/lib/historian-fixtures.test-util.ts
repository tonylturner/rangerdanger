import type { History, HistorySample } from "./historian-api";

export function historySample(seq: number, patch: Partial<HistorySample> = {}): HistorySample {
  const at = `2026-10-15T10:00:${String(seq).padStart(2, "0")}Z`;
  return {
    snapshot_seq: seq, value: 20.02,
    quality: { iv: false, nt: false, sb: false, bl: false, ov: false },
    stale_reason: null, source_time: null, source_time_invalid: null,
    cc_received_at: at, ingested_at: at, ...patch,
  };
}

export const HISTORY: History = {
  point: "oltc-1-bus-kv",
  segments: [
    { cache_epoch: "boot-a", samples: [historySample(1), historySample(2, { value: 19.7 }), historySample(3, { value: 19.7, stale_reason: "link_lost", quality: { iv: true, nt: true, sb: false, bl: false, ov: false } }), historySample(4)] },
    { cache_epoch: "boot-b", samples: [historySample(5), historySample(10, { value: 20.2, source_time: "2026-10-15T10:01:10Z", source_time_invalid: true })] },
  ],
  gaps: [{ from: "2026-10-15T10:00:05Z", to: "2026-10-15T10:00:10Z" }],
};
