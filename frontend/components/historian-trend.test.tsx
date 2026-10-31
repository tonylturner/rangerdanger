import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { HISTORY, historySample } from "../lib/historian-fixtures.test-util";
import { HistoryPlot } from "./historian-trend";

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
describe("historian trend", () => {
  it("keeps axis labels outside the scalable SVG and the plot at least 150px tall", () => {
    const html = renderToStaticMarkup(<HistoryPlot history={HISTORY} />);
    const svg = html.match(/<svg[\s\S]*?<\/svg>/)?.[0];
    expect(svg).toBeDefined();
    expect(svg).toContain('viewBox="0 0 840 150" preserveAspectRatio="none"');
    expect(svg).toContain('class="aspect-840/150 h-auto min-h-[150px] w-full overflow-visible"');
    expect(svg).not.toContain("<text");
    expect(html).toContain("pt-6 text-xs text-slate-300");
    expect(html).toContain('top-[10%] -translate-y-1/2">20.20</span>');
    expect(html).toContain('top-[90%] -translate-y-1/2">19.70</span>');
    expect(html).toContain('<span class="whitespace-nowrap">10:00:01.000 UTC</span>');
    expect(html).toContain('<span class="ml-auto whitespace-nowrap">10:00:10.000 UTC</span>');
    expect(html).toContain("col-start-2 flex min-w-0 flex-wrap justify-between");
    expect(svg).toContain('vector-effect="non-scaling-stroke"');
  });

  it("preserves plot-relative coordinates and gap bounds after separating the axes", () => {
    const html = renderToStaticMarkup(<HistoryPlot history={HISTORY} />);
    expect(html).toContain('cx="0"');
    expect(html).toContain('cx="840" cy="15"');
    expect(html).toContain('cy="135"');
    // The ingest gap starts at 5s, four ninths of the 1s–10s window.
    const gap = html.match(/<rect x="([^"]+)" y="0" width="([^"]+)" height="150"/);
    expect(gap).not.toBeNull();
    expect(Number(gap![1])).toBeCloseTo(840 * 4 / 9);
    expect(Number(gap![1]) + Number(gap![2])).toBeCloseTo(840);
  });

  it("contains wide snapshot rows horizontally and shows all 30 without a vertical clip", () => {
    const history = {
      ...HISTORY, gaps: [],
      segments: [{ cache_epoch: "0ba59ec6be7b5148", samples: Array.from({ length: 35 }, (_, i) => historySample(i + 1)) }],
    };
    const html = renderToStaticMarkup(<HistoryPlot history={history} />);
    expect(html).toContain('role="region" aria-label="Latest snapshots" tabindex="0" class="max-w-full overflow-x-auto"');
    expect(html).toContain('min-w-3xl');
    expect(html).toContain('wrap-anywhere');
    expect(html).not.toMatch(/max-h-|overflow-auto|overflow-y-/);
    expect(html.match(/<tbody>[\s\S]*?<\/tbody>/)?.[0].match(/<tr /g)).toHaveLength(30);
    const rows = text(html).slice(text(html).indexOf("CC epoch / seq"));
    expect(rows.indexOf("0ba59ec6be7b5148 / 35")).toBeLessThan(rows.indexOf("0ba59ec6be7b5148 / 6"));
    expect(rows).not.toContain("0ba59ec6be7b5148 / 5 ");
  });
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
    const history = { point: "oltc-1-tap", gaps: [], segments: [{ cache_epoch: "tap", samples: [historySample(1, { value: { value: -2, transient: true } })] }] };
    expect(text(renderToStaticMarkup(<HistoryPlot history={history} />))).toContain("-2 · TRANSIENT");
  });
  it("handles empty history without infinities in SVG attributes", () => {
    const html = renderToStaticMarkup(<HistoryPlot history={{ point: "oltc-1-bus-kv", segments: [], gaps: [] }} />);
    expect(html).not.toMatch(/NaN|Infinity/);
    expect(text(html)).toContain("0 cache-epoch segment(s)");
  });
});
