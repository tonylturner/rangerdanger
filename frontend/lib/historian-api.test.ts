import { afterEach, describe, expect, it, vi } from "vitest";
import { getHistorianHistory, getHistorianPoints, historyRuns, numericSample } from "./historian-api";
import { HISTORY, historySample } from "./historian-fixtures.test-util";

afterEach(() => vi.unstubAllGlobals());
describe("historian proxy", () => {
  it("URL-encodes the selected point and UTC window as a read only request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(HISTORY)));
    vi.stubGlobal("fetch", fetchMock);
    const since = "2026-10-15T09:45:00Z", until = "2026-10-15T10:00:00Z";
    await expect(getHistorianHistory("oltc-1-bus-kv", since, until)).resolves.toEqual(HISTORY);
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://localhost");
    expect(url.pathname).toMatch(/\/api\/historian\/history$/);
    expect([...url.searchParams]).toEqual([["point", "oltc-1-bus-kv"], ["since", since], ["until", until]]);
    expect(fetchMock.mock.calls[0][1]?.method).toBeUndefined();
  });
  it("reads the historian point catalogue", async () => {
    const points = [{ id: HISTORY.point, ioa: 2301, kind: "float", asdu: "M_ME_NC_1", last: historySample(1) }];
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(points)));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getHistorianPoints()).resolves.toEqual(points);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/historian\/points$/);
  });
});

describe("history integrity", () => {
  it("separates epochs, ingest gaps, and retained stale samples", () => {
    expect(historyRuns(HISTORY).map((run) => run.map((s) => s.snapshot_seq))).toEqual([[1, 2], [4], [5], [10]]);
    expect(HISTORY.segments[0].samples[2].value).toBe(19.7);
  });
  it.each(["iv", "nt", "sb", "bl", "ov"] as const)("breaks a line for quality %s, not with a zero", (flag) => {
    const s = historySample(1);
    expect(numericSample({ ...s, quality: { ...s.quality, [flag]: true } })).toBeNull();
  });
  it("does not draw unknown or moving tap samples as settled values", () => {
    for (const value of [null, "open", Infinity, NaN] as const) expect(numericSample(historySample(1, { value }))).toBeNull();
    expect(numericSample(historySample(1, { value: { value: 1, transient: true } }))).toBeNull();
    expect(numericSample(historySample(1, { value: { value: -2, transient: false } }))).toBe(-2);
    expect(numericSample(historySample(1, { value: 0 }))).toBe(0);
    expect(numericSample(historySample(1, { value: false }))).toBe(0);
  });
  it("keeps station-clock skew and time-IV without reordering ingest time", () => {
    const last = historyRuns(HISTORY).at(-1)![0];
    expect(last.source_time_invalid).toBe(true);
    expect(last.snapshot_seq).toBe(10);
    expect(numericSample(last)).toBe(20.2);
  });
  it("breaks rather than drawing back in time across malformed ingest ordering", () => {
    const sample = historySample(1);
    const history = { ...HISTORY, segments: [{ cache_epoch: "one", samples: [historySample(2), sample] }], gaps: [] };
    expect(historyRuns(history)).toHaveLength(2);
  });
});
