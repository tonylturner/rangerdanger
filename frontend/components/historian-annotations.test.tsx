// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HISTORY, historySample } from "../lib/historian-fixtures.test-util";
import type { HistorianEvent } from "../lib/historian-api";
import { HistoryPlot, HistorianTrend } from "./historian-trend";
import { breakDescriptions, eventDescription, groupHistorianEvents } from "./historian-annotations";
import { pointLabel } from "../lib/operator-format";

const tap: HistorianEvent = { point: "oltc-1-tap", at: "2026-10-15T10:00:04.787Z", from: 1, to: 2, quality: historySample(1).quality };
afterEach(() => vi.unstubAllGlobals());

describe("historian annotations", () => {
  it("labels settled tap values and nearby invalid observations without claiming causality", () => {
    expect(eventDescription(tap)).toBe("T1 tap 1 → 2");
    expect(pointLabel(tap.point)).toBe("T1 completed tap");
    const history = { ...HISTORY, segments: [{ cache_epoch: "a", samples: [historySample(2), historySample(3, { stale_reason: "measurement_invalid" }), historySample(4)] }] };
    expect(breakDescriptions(history, [tap])[0].text).toBe("Measurement invalid 10:00:03–10:00:04 UTC, around T1 tap 1 → 2");
    expect(breakDescriptions(history, [tap])[0].text).toContain("around T1 tap 1 → 2");
    expect(breakDescriptions(history, [tap])[0].text).not.toMatch(/observed interval|time correlation|\.\d{3}/);
    expect(breakDescriptions(history, [{ ...tap, at: historySample(10).ingested_at }])[0].text).not.toContain("around");
  });
  it("separates true break widths and event guides from transparent touch targets", () => {
    const { container } = render(<HistoryPlot history={HISTORY} events={[tap, { ...tap, point: "feeder-protection-trip", from: false, to: true }]}
      window={{ since: historySample(0).ingested_at, until: "2026-10-15T10:15:00Z" }} />);
    const band = container.querySelector<HTMLElement>("[data-break-band]")!;
    const trigger = screen.getByRole("button", { name: /IEC 104 link to the RTU lost/ });
    expect(Number.parseFloat(band.style.width)).toBeCloseTo(100 / 900);
    expect(band.style.minWidth).toBe("2px");
    expect(band.className).not.toMatch(/min-w-6|border-dashed/);
    expect(trigger.style.width).toBe(band.style.width);
    expect(trigger.className).toContain("min-w-6");
    expect(trigger.className).toContain("bg-transparent");
    const guides = container.querySelectorAll<HTMLElement>("[data-event-guide]");
    expect(guides).toHaveLength(1);
    expect(Number.parseFloat(guides[0].style.left)).toBeCloseTo(4.787 / 900 * 100);
    expect(guides[0].className).toContain("top-0 bottom-0 w-px");
    expect(container.querySelector<HTMLTimeElement>("li time")?.textContent).toBe("10:00:04 UTC");
    expect(container.querySelector<HTMLTimeElement>("li time")?.dateTime).toBe(tap.at);
  });
  it("opens event tooltips on keyboard focus, mouse hover and touch click, and closes on Escape", async () => {
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: "10:00:04 UTC T1 tap 1 → 2" });
    fireEvent.focus(button);
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("T1 tap 1 → 2"));
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
    const button = screen.getByRole("button", { name: /IEC 104 link to the RTU lost.*around T1 tap/ });
    fireEvent.click(button);
    await waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("IEC 104 link to the RTU lost 10:00:03–10:00:04 UTC, around T1 tap 1 → 2"));
    expect(screen.getByText("Why the line breaks (1)")).toBeTruthy();
  });
  it.each(["mouse", "touch"])("pins the tooltip after %s pointerdown, pointerup and click without focus", async (pointerType) => {
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: "10:00:04 UTC T1 tap 1 → 2" });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.pointerDown(button, { pointerType });
    fireEvent.pointerUp(button, { pointerType });
    fireEvent.click(button);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe("10:00:04 UTC T1 tap 1 → 2");
    expect(button.getAttribute("aria-describedby")).toBe(tooltip.id);
    fireEvent.pointerLeave(button, { pointerType });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 200)); });
    expect(screen.getByRole("tooltip")).toBe(tooltip);
    fireEvent.pointerDown(document.body, { pointerType });
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("pins a click after pointerdown focuses the marker, and dismisses with blur or a second click", () => {
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: "10:00:04 UTC T1 tap 1 → 2" });
    fireEvent.pointerDown(button, { pointerType: "mouse" });
    fireEvent.focus(button);
    fireEvent.pointerUp(button, { pointerType: "mouse" });
    fireEvent.click(button);
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent.blur(button);
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(button);
    fireEvent.click(button);
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent.pointerDown(button, { pointerType: "mouse" });
    fireEvent.pointerUp(button, { pointerType: "mouse" });
    fireEvent.click(button);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
  it("keeps the tooltip within a 320 px viewport and allows hovering its content", async () => {
    vi.stubGlobal("innerWidth", 320);
    vi.stubGlobal("innerHeight", 600);
    render(<HistoryPlot history={HISTORY} events={[tap]} />);
    const button = screen.getByRole("button", { name: "10:00:04 UTC T1 tap 1 → 2" });
    vi.spyOn(button, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 550, 28, 28));
    fireEvent.pointerMove(button, { pointerType: "mouse" });
    const tooltip = screen.getByRole("tooltip");
    expect(tooltip.style.width).toBe("288px");
    expect(tooltip.style.left).toBe("16px");
    expect(tooltip.style.bottom).toBe("56px");
    expect(tooltip.style.maxHeight).toBe("528px");
    fireEvent.pointerLeave(button);
    fireEvent.pointerEnter(tooltip);
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 200)); });
    expect(screen.getByRole("tooltip")).toBe(tooltip);
    fireEvent.pointerLeave(tooltip);
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
  });
  it("keeps simultaneous event facts available from the overlapping marker tooltip", async () => {
    render(<HistoryPlot history={HISTORY} events={[tap, { ...tap, point: "feeder-protection-trip", from: false, to: true }]} />);
    fireEvent.click(screen.getByRole("button", { name: "10:00:04 UTC T1 tap 1 → 2; CB1 protection trip NORMAL → TRIPPED" }));
    await waitFor(() => {
      const tooltip = screen.getByRole("tooltip");
      expect(tooltip.textContent).toContain("T1 tap 1 → 2");
      expect(tooltip.textContent).toContain("CB1 protection trip NORMAL → TRIPPED");
    });
  });
  it("keeps every event newest first without removing chart markers, including an all-invalid window", () => {
    const events = Array.from({ length: 12 }, (_, i) => ({ ...tap, at: historySample(i + 1).ingested_at, from: i, to: i + 1 }));
    const history = { ...HISTORY, gaps: [], segments: [{ cache_epoch: "a", samples: [historySample(1, { value: null }), historySample(12, { value: null })] }] };
    const { container } = render(<HistoryPlot history={history} events={events} />);
    expect(screen.queryByText(/Newest 10/)).toBeNull();
    const list = screen.getByRole("heading", { name: "Events in this window" }).nextElementSibling!;
    expect(list.querySelectorAll("li")).toHaveLength(12);
    expect(list.firstChild?.textContent).toContain("11 → 12");
    expect(container.querySelectorAll("button")).toHaveLength(13);
    expect(container.innerHTML).not.toMatch(/NaN|Infinity|<polyline/);
  });
  it("groups one protection instant into one row and rule, not nearby independent switching", () => {
    const at = tap.at;
    const events: HistorianEvent[] = [
      { ...tap, point: "feeder-breaker-position", from: "closed", to: "open" },
      { ...tap, point: "feeder-protection-trip", from: false, to: true },
      { ...tap, point: "feeder-lockout", from: false, to: true },
      { ...tap, point: "rmu-1-fault-passage", from: false, to: true },
      { ...tap, at: new Date(Date.parse(at) + 251).toISOString(), point: "rmu-1-position", from: "closed", to: "open" },
    ];
    expect(groupHistorianEvents(events).map((g) => [g.events.length, g.protection])).toEqual([[4, true], [1, false]]);
    const { container } = render(<HistoryPlot history={HISTORY} events={events} />);
    const rows = screen.getByRole("list", { name: "Archived events" }).querySelectorAll("li");
    expect(rows).toHaveLength(2);
    expect(rows[0].textContent).toBe("10:00:05 UTC · RMU1 OPEN");
    expect(rows[1].textContent).toBe("10:00:04 UTC · protection · CB1 OPEN · CB1 TRIPPED · CB1 LOCKED OUT · RMU1 FAULT PASSED");
    expect(container.querySelectorAll("[data-event-guide]")).toHaveLength(2);
    expect(container.innerHTML).not.toContain("operator");
    const reset = { ...events[2], from: true, to: false };
    expect(groupHistorianEvents([reset])[0].protection).toBe(false);
  });

  it("groups the observed S2 fault spacings into one rule and row per protection instant", () => {
    const observed = [
      ["18:36:48.040", "feeder-breaker-position", "closed", "open"],
      ["18:36:48.0416", "feeder-protection-trip", false, true],
      ["18:36:48.0428", "feeder-lockout", false, true],
      ["18:36:48.0589", "oltc-1-blocked", true, false],
      ["18:36:48.0599", "oltc-1-overcurrent-block", true, false],
      ["18:36:46.519", "rmu-1-fault-passage", false, true],
      ["18:36:46.540", "oltc-1-blocked", false, true],
      ["18:36:46.541", "oltc-1-overcurrent-block", false, true],
      ["18:36:57.640", "feeder-protection-trip", true, false],
      ["18:36:57.641", "feeder-lockout", true, false],
    ] as const;
    const events: HistorianEvent[] = observed.map(([at, point, from, to]) => ({
      ...tap, at: `2026-11-03T${at}Z`, point, from, to,
    }));
    const groups = groupHistorianEvents(events);
    expect(groups.map((group) => [group.events.length, group.protection])).toEqual([[3, true], [5, true], [2, false]]);
    expect(groups.flatMap((group) => group.events)).toHaveLength(events.length);
    const { container } = render(<HistoryPlot history={{ ...HISTORY, segments: [], gaps: [] }} events={events}
      window={{ since: "2026-11-03T18:36:45Z", until: "2026-11-03T18:36:59Z" }} />);
    const rows = screen.getByRole("list", { name: "Archived events" }).querySelectorAll("li");
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toBe("18:36:57 UTC · CB1 PROTECTION RESET · CB1 LOCKOUT RESET");
    expect(rows[1].textContent).toContain("18:36:48 UTC · protection · CB1 OPEN · CB1 TRIPPED · CB1 LOCKED OUT");
    expect(rows[2].textContent).toContain("18:36:46 UTC · protection · RMU1 FAULT PASSED");
    expect(container.querySelectorAll("[data-event-guide]")).toHaveLength(3);
  });
  it("anchors the inclusive 250 ms tolerance to the first event, without chaining or mutating input", () => {
    const events = [651, 400, 250, 200, 0].map((offset) => ({
      ...tap, at: new Date(Date.parse(tap.at) + offset).toISOString(),
    }));
    const original = [...events];
    const groups = groupHistorianEvents(events);
    expect(groups.map((group) => group.events.map((event) => Date.parse(event.at) - Date.parse(tap.at)))).toEqual([[0, 200, 250], [400], [651]]);
    expect(events).toEqual(original);
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
    expect(history).toHaveLength(voltageOnly ? 3 : 1);
    for (const name of ["since", "until"]) expect(events[0].searchParams.get(name)).toBe(history[0].searchParams.get(name));
    await act(async () => { await client.invalidateQueries({ queryKey: ["historian", "trend"] }); });
    expect(fetchMock.mock.calls.filter(([path]) => path.includes("/events"))).toHaveLength(2);
    expect(fetchMock.mock.calls.filter(([path]) => path.includes("/history"))).toHaveLength(voltageOnly ? 6 : 2);
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
