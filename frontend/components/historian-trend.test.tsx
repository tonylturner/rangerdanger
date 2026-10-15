import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { HISTORY, historySample } from "../lib/historian-fixtures.test-util";
import { HistoryPlot } from "./historian-trend";

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
describe("historian trend", () => {
  it("plots separate runs while retaining stale quality and full UTC times in the table", () => {
    const html = renderToStaticMarkup(<HistoryPlot history={HISTORY} />);
    expect(html.match(/data-history-run="true"/g)).toHaveLength(4);
    expect(text(html)).toContain("19.70 kV IV NT link_lost");
    expect(text(html)).toContain("Ingest gap: 2026-10-15T10:00:05Z → 2026-10-15T10:00:10Z");
    expect(text(html)).toContain("2026-10-15T10:01:10Z · time IV");
    expect(text(html)).toContain("boot-b / 10");
    expect(html).not.toContain("NaN");
  });
  it("does not invent a zero line when every sample is unknown", () => {
    const history = { ...HISTORY, segments: [{ cache_epoch: "empty", samples: [historySample(1, { value: null })] }] };
    const html = renderToStaticMarkup(<HistoryPlot history={history} />);
    expect(text(html)).toContain("No good numeric observations");
    expect(text(html)).toContain("not received");
    expect(html).not.toContain("polyline");
  });
  it("shows completed tap separately from its transient flag", () => {
    const history = { point: "oltc-1-tap", gaps: [], segments: [{ cache_epoch: "tap", samples: [historySample(1, { value: -2, transient: true })] }] };
    expect(text(renderToStaticMarkup(<HistoryPlot history={history} />))).toContain("-2 · TRANSIENT");
  });
  it("handles empty history without infinities in SVG attributes", () => {
    const html = renderToStaticMarkup(<HistoryPlot history={{ point: "oltc-1-bus-kv", segments: [], gaps: [] }} />);
    expect(html).not.toMatch(/NaN|Infinity/);
    expect(text(html)).toContain("0 cache-epoch segment(s)");
  });
});
