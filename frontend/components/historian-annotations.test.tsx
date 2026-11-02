// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HISTORY, historySample } from "../lib/historian-fixtures.test-util";
import type { HistorianEvent } from "../lib/historian-api";
import { HistoryPlot, HistorianTrend } from "./historian-trend";
import { breakDescriptions, eventDescription } from "./historian-annotations";

const tap: HistorianEvent = { point: "oltc-1-tap", at: historySample(4).ingested_at, from: 1, to: 2, quality: historySample(1).quality };
afterEach(() => vi.unstubAllGlobals());

describe("historian annotations", () => {
  it("labels settled tap values and nearby invalid observations without claiming causality", () => {
    expect(eventDescription(tap)).toBe("T1 completed tap 1 → 2");
    const history = { ...HISTORY, segments: [{ cache_epoch: "a", samples: [historySample(2), historySample(3, { stale_reason: "measurement_invalid" }), historySample(4)] }] };
    expect(breakDescriptions(history, [tap])[0].text).toContain("Measurement invalid 10:00:03.000–10:00:04.000 UTC");
    expect(breakDescriptions(history, [tap])[0].text).toContain("near T1 completed tap 1 → 2 (time correlation, not proven cause)");
    expect(breakDescriptions(history, [{ ...tap, at: historySample(10).ingested_at }])[0].text).not.toContain("near");
  });
  it("opens event tooltips on keyboard focus, mouse hover and touch click, and closes on Escape", async () => {
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: "10:00:04.000 UTC T1 completed tap 1 → 2" });
    fireEvent.focus(button);
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("T1 completed tap 1 → 2"));
    fireEvent.keyDown(button, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    fireEvent.blur(button);
    fireEvent.pointerMove(button, { pointerType: "mouse" });
    await waitFor(() => expect(screen.getByRole("tooltip")).toBeTruthy());
    fireEvent.pointerLeave(button);
    fireEvent.pointerMove(document.body, { clientX: 1000, clientY: 1000 });
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
    fireEvent.pointerDown(button, { pointerType: "touch" });
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByRole("tooltip")).toBeTruthy());
  });
  it("makes line-break reasons focusable and touch accessible too", async () => {
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: /IEC 104 link to the RTU lost.*near T1 completed tap/ });
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("observed interval"));
    expect(screen.getByText("Why the line breaks (1)")).toBeTruthy();
  });
  it("keeps simultaneous event facts available from the overlapping marker tooltip", async () => {
    render(<HistoryPlot history={HISTORY} events={[tap, { ...tap, point: "feeder-protection-trip", from: false, to: true }]} />);
    fireEvent.click(screen.getByRole("button", { name: "10:00:04.000 UTC Protection trip NORMAL → TRIPPED" }));
    await waitFor(() => {
      const tooltip = screen.getByRole("tooltip");
      expect(tooltip.textContent).toContain("T1 completed tap 1 → 2");
      expect(tooltip.textContent).toContain("Protection trip NORMAL → TRIPPED");
    });
  });
  it("caps the newest-first list at ten without removing chart markers, including an all-invalid window", () => {
    const events = Array.from({ length: 12 }, (_, i) => ({ ...tap, at: historySample(i + 1).ingested_at, from: i, to: i + 1 }));
    const history = { ...HISTORY, gaps: [], segments: [{ cache_epoch: "a", samples: [historySample(1, { value: null }), historySample(12, { value: null })] }] };
    const { container } = render(<HistoryPlot history={history} events={events} />);
    expect(screen.getByText("Newest 10 of 12 events. All markers remain on the chart.")).toBeTruthy();
    const list = screen.getByRole("heading", { name: "Events in this window" }).nextElementSibling!;
    expect(list.querySelectorAll("li")).toHaveLength(10);
    expect(list.firstChild?.textContent).toContain("11 → 12");
    expect(container.querySelectorAll("button")).toHaveLength(13);
    expect(container.innerHTML).not.toMatch(/NaN|Infinity|<polyline/);
  });
  it.each([false, true])("uses one extra event read with exactly the history window (voltageOnly=%s)", async (voltageOnly) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const fetchMock = vi.fn().mockImplementation(async (path: string) => new Response(JSON.stringify(
      path.includes("/points") ? [] : path.includes("/events") ? { events: [tap], gaps: [] } : HISTORY,
    )));
    vi.stubGlobal("fetch", fetchMock);
    const { unmount } = render(<QueryClientProvider client={client}><HistorianTrend voltageOnly={voltageOnly} /></QueryClientProvider>);
    await waitFor(() => expect(screen.getByRole("heading", { name: "Events in this window" })).toBeTruthy());
    const urls = fetchMock.mock.calls.map(([path]) => new URL(path, "http://localhost"));
    const history = urls.filter((url) => url.pathname.endsWith("/history"));
    const events = urls.filter((url) => url.pathname.endsWith("/events"));
    expect(events).toHaveLength(1);
    expect(history).toHaveLength(1);
    for (const name of ["since", "until"]) expect(events[0].searchParams.get(name)).toBe(history[0].searchParams.get(name));
    await act(async () => { await client.invalidateQueries({ queryKey: ["historian", "trend"] }); });
    expect(fetchMock.mock.calls.filter(([path]) => path.includes("/events"))).toHaveLength(2);
    expect(fetchMock.mock.calls.filter(([path]) => path.includes("/history"))).toHaveLength(2);
    await act(async () => unmount());
    client.clear();
  });
  it("reports an event-read failure rather than claiming there were no events", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (path: string) =>
      path.includes("/events") ? new Response('{"error":"unavailable"}', { status: 502 }) :
        new Response(JSON.stringify(path.includes("/points") ? [] : HISTORY))));
    const { unmount } = render(<QueryClientProvider client={client}><HistorianTrend /></QueryClientProvider>);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Historian unavailable"));
    expect(screen.queryByText("No major status changes observed.")).toBeNull();
    unmount();
    client.clear();
  });
});
