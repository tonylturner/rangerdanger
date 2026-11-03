// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HISTORY, historySample } from "../lib/historian-fixtures.test-util";
import type { History, HistorianEvent } from "../lib/historian-api";
import { HistorianTrend, HistoryPlot, SectionHistoryPlot } from "./historian-trend";

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
const sectionHistory = (point: string, first: number, last: number): History => ({
  point, gaps: [],
  segments: [{
    cache_epoch: "section-epoch",
    samples: [
      historySample(1, { value: first, ingested_at: "2026-10-15T10:00:00Z" }),
      historySample(2, { value: last, ingested_at: "2026-10-15T10:00:10Z" }),
    ],
  }],
});
const sectionHistories = [
  {
    ...sectionHistory("rmu-1-source-kv", 20, 19.9),
    gaps: [{ from: "2026-10-15T10:00:02Z", to: "2026-10-15T10:00:08Z" }],
  },
  {
    ...sectionHistory("rmu-1-section-kv", 20, 0),
    segments: [
      {
        cache_epoch: "old-section-epoch",
        samples: [
          historySample(1, { value: 20, ingested_at: "2026-10-15T10:00:00Z" }),
          historySample(2, { value: 20, ingested_at: "2026-10-15T10:00:02Z", stale_reason: "link_lost", quality: { iv: true, nt: true, sb: false, bl: false, ov: false } }),
        ],
      },
      {
        cache_epoch: "section-epoch",
        samples: [
          historySample(3, { value: 0, ingested_at: "2026-10-15T10:00:05Z" }),
          historySample(4, { value: 0, ingested_at: "2026-10-15T10:00:10Z" }),
        ],
      },
    ],
  },
  sectionHistory("rmu-2-source-kv", 20, 20.1),
];
const sectionEvents: HistorianEvent[] = [{ point: "rmu-1-position", at: "2026-10-15T10:00:05Z", from: "closed", to: "open", quality: historySample(1).quality }];

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
  it("lets the overview expand from five snapshots to thirty and collapse again", () => {
    const history = {
      ...HISTORY, gaps: [],
      segments: [{ cache_epoch: "overview", samples: Array.from({ length: 35 }, (_, i) => historySample(i + 1)) }],
    };
    render(<HistoryPlot history={history} expandableSnapshots />);
    expect(screen.getByRole("caption").textContent).toBe("Latest 5 snapshots, newest first · full timestamps in UTC");
    expect(screen.getAllByRole("row")).toHaveLength(6);

    const toggle = screen.getByRole("button", { name: "Show last 30" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(screen.getByRole("caption").textContent).toBe("Latest 30 snapshots, newest first · full timestamps in UTC");
    expect(screen.getAllByRole("row")).toHaveLength(31);
    expect(screen.getByRole("button", { name: "Show last 5" }).getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "Show last 5" }));
    expect(screen.getAllByRole("row")).toHaveLength(6);
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
    expect(text(html)).toContain("No valid numeric observations");
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

  it("plots three section voltages with independent line styles and historian markers", () => {
    const html = renderToStaticMarkup(<SectionHistoryPlot histories={sectionHistories} events={sectionEvents} expandableSnapshots />);
    expect(html).toContain('aria-label="Section voltage series legend"');
    expect(html).toContain("S1 · RMU1 source VT");
    expect(html).toContain("S2 · RMU1 section VT (S2)");
    expect(html).toContain("S3 · RMU2 source VT");
    expect(html).toContain('data-history-run="rmu-1-source-kv"');
    expect(html.match(/data-history-run="rmu-1-source-kv"/g)).toHaveLength(2);
    expect(html.match(/data-history-run="rmu-1-section-kv"/g)).toHaveLength(2);
    expect(html).toContain('stroke-dasharray="8 4"');
    expect(html).toContain('stroke-dasharray="2 3"');
    expect(html).toContain('data-event-guide="true"');
    expect(html).not.toContain("CC receive time");
    expect(html).not.toContain("completed switching marker");
    expect(html).toContain("S2 snapshots · RMU1 section VT (same S2 bus measured at RMU2)");
    expect(html.match(/<tbody>[\s\S]*?<\/tbody>/)?.[0].match(/<tr /g)).toHaveLength(4);
  });

  it("defaults the voltage dashboard to sections but keeps Control Centre on one signal", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    const pointIds = ["oltc-1-bus-kv", "feeder-voltage", "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-section-kv", "rmu-2-source-kv"];
    client.setQueryData(["historian", "points"], pointIds.map((id, ioa) => ({ id, ioa: ioa + 1, kind: "float", asdu: "M_ME_NC_1", last: null })));
    client.setQueryData(["historian", "trend", "section-voltage"], { histories: sectionHistories, events: [], window: { since: "2026-10-15T09:45:10Z", until: "2026-10-15T10:00:10Z" } });
    client.setQueryData(["historian", "trend", "oltc-1-bus-kv"], { histories: [HISTORY], events: [], window: { since: "2026-10-15T09:45:10Z", until: "2026-10-15T10:00:10Z" } });
    const { unmount } = render(<QueryClientProvider client={client}><HistorianTrend voltageOnly expandableSnapshots /></QueryClientProvider>);
    const selector = screen.getByLabelText("Signal") as HTMLSelectElement;
    expect(selector.value).toBe("section-voltage");
    expect(screen.getByRole("option", { name: "Sections S1/S2/S3 · default" })).toBeTruthy();
    expect(screen.getByRole("img", { name: /S1, S2 and S3 section voltage trends/ })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Latest snapshots" })).toBeTruthy();
    unmount();

    render(<QueryClientProvider client={client}><HistorianTrend /></QueryClientProvider>);
    expect((screen.getByLabelText("Signal") as HTMLSelectElement).value).toBe("oltc-1-bus-kv");
    expect(screen.queryByRole("option", { name: "Sections S1/S2/S3 · default" })).toBeNull();
    expect(screen.queryByRole("img", { name: /S1, S2 and S3 section voltage trends/ })).toBeNull();
    expect(screen.queryByLabelText("Switching and protection events")).toBeNull();
    client.clear();
  });
});
