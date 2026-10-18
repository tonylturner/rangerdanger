import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "../lib/api";
import type { CommandResult, OperatorPoint } from "../lib/operator-api";
import { commandErrorText } from "../lib/operator-format";
import { FEEDER_STATE, FRESH_STATE, JOURNAL, EMPTY_JOURNAL, NOW, STALE_STATE, commandResult } from "../lib/operator-fixtures.test-util";
import { RangeNotReadyError } from "../lib/range";
import { CommandPanelView, CommandResultView, CONTROL_PANELS, type CommandPanelState } from "./control-centre-command";
import { ControlCentreOneLine } from "./control-centre-one-line";
import { controlCentreObservations } from "../lib/observation-readings";
import { AbnormalSummary, ControlCentreScreen, ObservationStatus } from "./control-centre-view";
import { ControlCentreMiniStatus } from "./runner-operator-status";
import { DeviceFaceplate, RegulationBand } from "./control-centre-faceplate";
import { devicePoints } from "../lib/operator-workstation";

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
function render(node: ReactNode): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  client.clear();
  return html;
}
function screen(props: Partial<Parameters<typeof ControlCentreScreen>[0]> = {}) {
  return render(<ControlCentreScreen initialTab="Points" state={FEEDER_STATE} stateError={null} stateUpdatedAt={NOW - 1000} journal={JOURNAL} now={NOW} {...props} />);
}

describe("ControlCentreScreen schema 3", () => {
  it("shows all 42 points, per-device controls and snapshot identity", () => {
    const html = screen();
    const out = text(html);
    expect(FEEDER_STATE.points).toHaveLength(42);
    expect(out).toContain("42/42 good");
    expect(html.match(/data-condition="good"/g)).toHaveLength(42);
    for (const label of ["STARTED", "GI ok", "T1 110/20 kV", "CB1", "RMU1", "RMU2", "S1 residential", "S2 commercial", "S3 industrial", "F2 · CLOSED · GOOD", "snapshot 120", "Trends"]) expect(out).toContain(label);
    expect(out).toContain("Confirm RAISE");
    expect(out).toContain("Confirm MANUAL");
  });

  it("keeps last values on link loss, including age and reason", () => {
    const html = screen({ state: STALE_STATE, journal: EMPTY_JOURNAL });
    const out = text(html);
    expect(out).toContain("SCADA VIEW LOST — retained values are not current");
    expect(html.match(/data-condition="stale"/g)).toHaveLength(4);
    for (const value of ["CLOSED", "28.9 A", "19.94 kV", "IV NT", "29 s", "0/4 good · 4 STALE"]) expect(out).toContain(value);
    expect(out).not.toContain("0.0 A");
    expect(out).not.toContain("0.00 kV");
    expect(html).toContain("CB1: CLOSED · STALE · IV NT · link_lost");
  });

  it("disables every control when CC API fails and marks the entire cached snapshot without rewriting IEC flags", () => {
    const html = screen({ stateError: new ApiError(502, "control centre unreachable") });
    expect(text(html)).toContain("Showing the last answer");
    expect(html).toContain("CB1: CLOSED · GOOD · cached snapshot · CC API unavailable");
    expect(html.match(/disabled=""/g)).toHaveLength(5);
  });

  it("does not command a cached range while a not-ready answer is being handled", () => {
    const html = screen({ stateError: new RangeNotReadyError("starting") });
    expect(text(html)).toContain("Showing the last answer; controls disabled.");
    expect(html.match(/disabled=""/g)).toHaveLength(5);
  });

  it("waits for a ready range", () => {
    const out = text(screen({ state: undefined, stateError: new RangeNotReadyError("starting") }));
    expect(out).toContain("The control centre shows once the range is ready.");
    expect(out).not.toContain("Control centre API unavailable");
  });

  it("shows journal newest first and preserves null empty bounds", () => {
    const out = text(screen({ initialTab: "Events" }));
    expect(out.indexOf("Feeder breaker position STALE")).toBeLessThan(out.indexOf("Link down #3"));
    expect(out).toContain("seq 1–4");
    expect(text(screen({ journal: EMPTY_JOURNAL, initialTab: "Events" }))).toContain("No journal entries yet.");
  });
});

describe("CC schematic layout", () => {
  it.each([1440, 1920])("orders status, schematic, selected controls, secondary rows and alarms at %ipx", (viewport) => {
    // DOM-order contract at both sides of the existing desktop breakpoint;
    // SSR does not measure pixels or execute media queries.
    const html = render(<div style={{ width: viewport }}><ControlCentreScreen state={FEEDER_STATE} stateError={null} stateUpdatedAt={NOW} journal={JOURNAL} now={NOW} /></div>);
    const order = [
      'aria-label="Observation status"', 'aria-label="Feeder schematic"',
      'aria-label="T1 faceplate"', "Confirm RAISE", "Current band:", '<dl class="text-xs">',
      'aria-label="Abnormal conditions"', 'aria-label="Operator records"', "<summary>Link diagnostics</summary>",
    ].map((marker) => html.indexOf(marker));
    expect(html).toContain(`width:${viewport}px`);
    expect(order.every((index) => index >= 0)).toBe(true);
    expect(order).toEqual([...order].sort((a, b) => a - b));
    expect(html).toContain("<details class=\"text-xs text-slate-400\"><summary>Link diagnostics</summary>");
  });

  it("shows only a current-connection GI and counts current observations, not electrical validity", () => {
    const state = structuredClone(FEEDER_STATE);
    const status = (unavailable = false) => text(render(<ObservationStatus state={state} unavailable={unavailable} />));
    expect(status()).toContain("Points current 42/42");
    state.points[0].quality = { ...state.points[0].quality, iv: true };
    state.points[0].stale_reason = "measurement_invalid";
    expect(status()).toContain("Points current 42/42");
    state.points[0].quality.nt = true;
    expect(status()).toContain("Points current 41/42");
    expect(status(true)).toContain("Points current 0/42");
    expect(status(true)).toContain("(cached)");
    state.link.gi.state = "running";
    expect(status()).not.toContain("not completed");
    state.link.since = "2099-01-01T00:00:00Z";
    expect(status()).toContain("Current-connection GI (UTC): not completed");
    expect(status()).toContain("Points current 0/42");
    expect(text(render(<ObservationStatus state={STALE_STATE} unavailable={false} />))).toContain("Points current 0/4");
  });
  it("reserves upper annotation lanes clear of the RMUs and right-hand ring line", () => {
    const html = render(<ControlCentreOneLine points={FEEDER_STATE.points} />);
    const labels = [...html.matchAll(/<text x="(\d+)" y="(\d+)"[^>]*>([^<]*)<\/text>/g)];
    const position = (prefix: string) => {
      const label = labels.find((m) => m[3].startsWith(prefix));
      expect(label).toBeDefined();
      return { x: Number(label![1]), y: Number(label![2]) };
    };
    const s1 = position("S1 residential"), s2 = position("S2 commercial");
    // Geometry contract, not a browser text-bounds measurement: section
    // captions sit over their spans, above the RMU enclosures (y=150).
    expect(s1.x).toBeGreaterThan(330);
    expect(s1.x).toBeLessThan(492);
    expect(s2.x).toBeGreaterThan(648);
    expect(s2.x).toBeLessThan(772);
    // Each section now has a location, voltage and separate supply-path row.
    expect(s1.y + 54).toBeLessThan(150);
    expect(s2.y + 54).toBeLessThan(150);
    const section = position("RMU2 S2 bus VT");
    expect(section.x).toBeGreaterThan(772);
    expect(section.x).toBeLessThan(1000);
    expect(section.y + 18).toBeLessThan(150);
    // The S3-side measurement lives in the lower annotation lane, not
    // beside the right-hand vertical ring conductor (x=1000).
    const source = position("RMU2 S3 side");
    expect(source.x).toBeGreaterThan(648);
    expect(source.x).toBeLessThan(772);
    expect(source.y).toBeGreaterThan(325);
    expect(source.y + 36).toBeLessThan(410);
    // F1 is beside the incoming conductor, not crowded under CB1 current.
    const f1 = position("F1 feeder");
    expect(f1.x).toBeGreaterThan(80);
    expect(f1.x).toBeLessThan(270);
    expect(f1.y).toBeGreaterThan(165);
    expect(f1.y).toBeLessThan(190);
    // T1 current has its own lower-left lane, below the switch feedback,
    // clear of the transformer selection outline (bottom y=141).
    const t1Current = position("T1 20 kV terminal current");
    const busbar = position("20 kV busbar (T1 secondary)");
    expect(t1Current.y).toBeGreaterThan(235);
    expect(t1Current.y + 36).toBeLessThan(busbar.y);
    expect(busbar.y + 18).toBeLessThan(325);
  });

  it("keeps regulation labels in fixed-size HTML outside the height-bounded graphic", () => {
    const html = render(<RegulationBand band={{ setpoint: 20.4, low: 20.2, high: 20.6, bus: 20.23 }} />);
    const svg = html.match(/<svg[\s\S]*?<\/svg>/)?.[0];
    expect(svg).toContain('class="h-12 w-full"');
    expect(svg).toContain('preserveAspectRatio="none"');
    expect(svg).not.toContain("<text");
    expect(html).toContain('text-xs text-slate-300"><span>20.20–20.60 kV</span><span>Bus 20.23 kV</span>');
    expect(svg).toContain('aria-label="Current band 20.20 to 20.60 kV; 20 kV busbar (T1 secondary) 20.23"');
    expect(text(render(<RegulationBand band={{ setpoint: 20.4, low: 20.2, high: 20.6, bus: null }} />))).toContain("Bus unknown");
    expect(text(render(<RegulationBand band={null} />))).toContain("Current regulation band unavailable");
  });

  it("shows every T1 point and its reporting without a height-limited nested scroll", () => {
    const html = render(<DeviceFaceplate state={FEEDER_STATE} selected="T1" unavailable={false} />);
    const list = html.match(/<dl[\s\S]*?<\/dl>/)?.[0];
    expect(list).toBeDefined();
    expect(list).not.toMatch(/max-h-|overflow-/);
    expect(list?.match(/<dt /g)).toHaveLength(devicePoints(FEEDER_STATE.points, "T1").length);
    expect(list?.match(/✓/g)).toHaveLength(devicePoints(FEEDER_STATE.points, "T1").length);
    expect(text(list!)).toContain("OLTC lower limit CLEAR ✓ GOOD on change · timed event; GI untimed");
    expect(text(list!)).toContain("Configured delay 30 s");
  });

  it("stacks until wide desktop and does not stretch the schematic to faceplate height", () => {
    const html = screen();
    expect(html).toContain("grid min-w-0 items-start gap-4 min-[1920px]:grid-cols-[minmax(0,1fr)_300px]");
    expect(html).not.toContain("xl:grid-cols-[minmax(0,2fr)");
  });

  it.each([1280, 1440, 1536, 1920])("fits the SVG with at least 12px labels at %ipx", (viewport) => {
    const html = render(<ControlCentreOneLine points={FEEDER_STATE.points} />);
    expect(html).toContain('viewBox="0 0 1060 410"');
    expect(html).toContain('class="w-full min-w-[1060px]"');
    const fonts = [...html.matchAll(/font-size="(\d+)"/g)].map((m) => Number(m[1]));
    // Match the shell's 12rem sidebar, max-w-[1600px], px-6, gap-4 and
    // the schematic panel's p-3 + border. This checks sizing arithmetic,
    // not a browser layout measurement.
    const content = Math.min(viewport - 192, 1600) - 48;
    const svgWidth = content - (viewport >= 1920 ? 300 + 16 : 0) - 26;
    expect(Math.min(...fonts) * Math.max(1060, svgWidth) / 1060).toBeGreaterThanOrEqual(12);
  });

  it("contains phone overflow in the schematic's scroll box, not the page", () => {
    const html = screen();
    expect(html).toContain("w-screen min-w-0");
    expect(html).toContain('<figure class="min-w-0 space-y-3"><div class="max-w-full overflow-x-auto">');
    expect(html).toContain('class="w-full min-w-[1060px]"');
  });
});

describe("CC one-line observation integrity", () => {
  const points = (id: string, patch: Partial<OperatorPoint>) => FEEDER_STATE.points.map((p) => p.id === id ? { ...p, ...patch } : p);
  it("left-aligns the full 110 kV source label inside the SVG", () => {
    const html = render(<ControlCentreOneLine points={FEEDER_STATE.points} />);
    const label = html.match(/<text\b[^>]*>110 kV nominal — not telemetered<\/text>/)?.[0];
    expect(label).toBeDefined();
    expect(label).toContain('x="16"');
    expect(label).toContain('text-anchor="start"');
  });

  it("does not infer section voltage from breaker or RMU position", () => {
    const p = points("feeder-breaker-position", { value: "open" });
    const out = text(render(<ControlCentreOneLine points={p} />));
    expect(out).toContain("S1 residential · LIVE");
    const zero = p.map((point) => ["feeder-voltage", "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-section-kv"].includes(point.id) ? { ...point, value: 0 } : point);
    expect(text(render(<ControlCentreOneLine points={zero} />))).toContain("S1 residential · DEAD");
    expect(text(render(<ControlCentreOneLine points={zero} />))).toContain("S2 commercial · DEAD");
    const missing = p.filter((point) => point.id !== "rmu-1-source-kv");
    expect(text(render(<ControlCentreOneLine points={missing} />))).toContain("S1 residential · UNKNOWN");
  });

  it("keeps mixed stale, unknown, substituted and transient states on their own symbols", () => {
    const mixed = FEEDER_STATE.points.map((p) => {
      if (p.id === "rmu-1-position") return { ...p, quality: { ...p.quality, iv: true, nt: true }, stale_reason: "device_unreachable" as const };
      if (p.id === "rmu-2-position") return { ...p, value: null };
      if (p.id === "oltc-1-tap") return { ...p, value: { value: -3, transient: true }, quality: { ...p.quality, sb: true } };
      if (p.id === "plc-1-running") return { ...p, quality: { ...p.quality, iv: true }, stale_reason: "heartbeat" as const };
      return p;
    });
    const html = render(<ControlCentreOneLine points={mixed} />);
    expect(html).toContain("CB1: CLOSED · GOOD");
    expect(html).toContain("RMU1 · NC: CLOSED · STALE · IV NT · device_unreachable");
    expect(html).toContain("RMU2 · NOP: not received · UNKNOWN");
    expect(html).toContain("T1 OLTC: -3 · TRANSIENT · STALE · SB");
    expect(controlCentreObservations(mixed)("plc-1-running").text).toContain("IV · heartbeat");
    expect(controlCentreObservations(mixed)("oltc-1-tap").transient).toBe(true);
  });
});

function panel(state: CommandPanelState, definition = CONTROL_PANELS[0] as (typeof CONTROL_PANELS)[number], patch: Partial<Parameters<typeof CommandPanelView>[0]> = {}) {
  return render(<CommandPanelView panel={definition} linkState="started" position={FEEDER_STATE.points.find((p) => p.id === definition.feedback)} state={state} onArm={() => {}} onExecute={() => {}} onCancel={() => {}} {...patch} />);
}

describe("select-then-execute controls", () => {
  it.each(CONTROL_PANELS)("requires explicit select/execute for $point", (definition) => {
    const idle = panel({ phase: "idle" }, definition);
    expect(text(idle)).not.toContain("Execute");
    for (const operation of definition.operations) {
      expect(text(idle)).toContain(`Confirm ${operation.toUpperCase()}`);
      const armed = text(panel({ phase: "armed", operation }, definition));
      expect(armed).toContain(`Local confirmation: ${operation.toUpperCase()}`);
      expect(armed).toContain(`Execute ${operation.toUpperCase()}`);
      expect(armed).toContain("Cancel");
    }
  });

  it.each(["completed", "rejected", "failed", "unknown"] as const)("shows typed %s result and exact reason", (status) => {
    const result = commandResult(status);
    const out = text(panel({ phase: "result", operation: "open", result }));
    expect(out).toContain(`OPEN ${status.toUpperCase()}`);
    if (result.reason !== null) expect(out).toContain(result.reason);
    expect(out).toContain("Select requested (UTC) 12:00:01.000");
    expect(out).toContain("ACT-TERM (UTC)");
    if (status === "unknown") expect(out).toContain("It is not retried");
    if (status === "completed") expect(out).toContain("IOA 1001: OPEN");
  });

  it("renders completed feedback without a reason line or an error when the CC reason is null", () => {
    const result = commandResult("completed");
    expect(result.reason).toBeNull();
    const html = render(<CommandResultView result={result} />);
    expect(text(html)).toContain("OPEN COMPLETED");
    expect(text(html)).toContain("IOA 1001: OPEN at 12:00:01.600");
    expect(html.match(/<p\b/g)).toHaveLength(1);
    expect(html).not.toContain("null");
    expect(html).not.toContain('role="alert"');
    expect(text(html)).not.toContain("command outcome is unknown");
  });

  it("renders step and mode feedback without treating it as a double point", () => {
    const result: CommandResult = { ...commandResult("completed"), point: "oltc-1-step", operation: "raise", feedback: { ioa: 1301, value: { value: 2, transient: false }, at: "2026-10-09T12:00:01.600Z" } };
    expect(text(panel({ phase: "result", operation: "raise", result }, CONTROL_PANELS[3]))).toContain("IOA 1301: 2");
    const mode: CommandResult = { ...result, point: "oltc-1-mode", operation: "auto", feedback: { ioa: 1303, value: true, at: "2026-10-09T12:00:01.600Z" } };
    expect(text(panel({ phase: "result", operation: "auto", result: mode }, CONTROL_PANELS[4]))).toContain("IOA 1303: AUTO");
  });

  it("disables selection and execute on link loss, stale/unknown feedback, or busy", () => {
    const position = FEEDER_STATE.points[0];
    for (const patch of [{ linkState: "connecting" as const }, { position: undefined }, { disabled: true }, { position: { ...position, quality: { ...position.quality, iv: true } } }]) {
      expect(panel({ phase: "idle" }, CONTROL_PANELS[0], patch).match(/disabled=""/g)).toHaveLength(2);
      expect(panel({ phase: "armed", operation: "close" }, CONTROL_PANELS[0], patch)).toContain('disabled=""');
    }
    expect(panel({ phase: "idle" })).not.toContain('disabled=""');
  });

  it("does not invent a typed failure for an ambiguous HTTP error", () => {
    const message = commandErrorText(new ApiError(504, "CC timeout"));
    const out = text(panel({ phase: "error", operation: "close", message }));
    expect(out).toContain("outcome is not known");
    expect(out).not.toContain("FAILED");
  });
});

describe("ControlCentreMiniStatus", () => {
  it("preserves the runner summary", () => {
    expect(text(render(<ControlCentreMiniStatus state={FRESH_STATE} waiting={false} />))).toContain("4/4 good");
    expect(text(render(<ControlCentreMiniStatus state={STALE_STATE} waiting={false} />))).toContain("CLOSED ⚠ STALE");
  });
});

describe("realism presentation", () => {
  it.each([
    ["rmu-1-remote", "RMU1 authority: LOCAL"],
    ["rmu-2-remote", "RMU2 authority: LOCAL"],
    ["oltc-1-remote", "T1 OLTC authority: LOCAL"],
  ])("surfaces %s LOCAL in the summary and schematic without calling it an alarm", (id, label) => {
    const state = structuredClone(FEEDER_STATE);
    state.points.find((p) => p.id === id)!.value = false;
    const out = text(render(<AbnormalSummary state={state} />));
    expect(out).toContain(label);
    expect(out).not.toContain("No alarms in GOOD observations");
    expect(text(screen({ state }))).toContain("0 active alarms");
    expect(render(<ControlCentreOneLine points={state.points} />)).toContain(`data-authority="${id}"`);
  });

  it("keeps abnormal-condition detail after the selected faceplate", () => {
    expect(text(render(<AbnormalSummary state={FEEDER_STATE} />))).toContain("No alarms in GOOD observations");
    const html = screen();
    expect(html.indexOf('aria-label="Abnormal conditions"')).toBeGreaterThan(html.indexOf('aria-label="T1 faceplate"'));
  });
  it("shows P/Q/PF and configured delay while PF IV remains a quality-marked dash", () => {
    const state = structuredClone(FEEDER_STATE);
    const pf = state.points.find((p) => p.id === "oltc-1-pf")!;
    pf.value = 0;
    pf.quality.iv = true;
    pf.stale_reason = "measurement_invalid";
    const out = text(screen({ state }));
    expect(out).toContain("T1 20 kV terminal active power P 3.05 MW");
    expect(out).toContain("T1 20 kV terminal reactive power Q 0.92 Mvar");
    expect(out).toContain("T1 20 kV terminal power factor PF — ⚠ STALE IV measurement_invalid");
    expect(out).toContain("Configured delay 30 s");
  });
  it("keeps S3 unknown when its own received kV is invalid despite a good F2-side switch", () => {
    const points = FEEDER_STATE.points.map((p) => p.id === "rmu-2-source-kv" ? { ...p, quality: { ...p.quality, iv: true } } : p);
    const html = render(<ControlCentreOneLine points={points} />);
    expect(text(html)).toContain("S3 industrial · UNKNOWN");
    expect(html).toContain("RMU2 · NOP assembly: switch function and earth");
  });
});
