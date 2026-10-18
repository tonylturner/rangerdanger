import { afterEach, describe, expect, it, vi } from "vitest";
import { type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { FieldState, OperatorState } from "../lib/operator-api";
import { AUDIT, FEEDER_STATE, JOURNAL, NOW, TRUTH } from "../lib/operator-fixtures.test-util";
import { physicalAbnormalities } from "../lib/physical-abnormalities";
import { physicalSystemObservations } from "../lib/observation-readings";
import { PhysicalSystem, PhysicalSystemScreen } from "./physical-system";
import { AuditTable } from "./physical-system-audit";
import { PhysicalDiagnostics } from "./physical-system-diagnostics";
import { EquipmentCard } from "./physical-system-equipment";
import { ControlCentreScreen, ControlCentreView } from "./control-centre-view";

const text = (html: string) => html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
const clients: QueryClient[] = [], trees: ReactTestRenderer[] = [];
const scroll = vi.fn(), selector = vi.fn(() => ({ scrollIntoView: scroll }));
function mount(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  let tree!: ReactTestRenderer;
  act(() => { tree = create(<QueryClientProvider client={client}>{node}</QueryClientProvider>, { createNodeMock: () => ({ querySelector: selector }) }); });
  trees.push(tree); return tree;
}
const props = { field: TRUTH, stateError: null, operator: FEEDER_STATE, now: NOW };
const output = (tree: ReactTestRenderer) => JSON.stringify(tree.toJSON());
const button = (tree: ReactTestRenderer, label: string) => tree.root.findAllByType("button").find((b) => b.children.map((c) => typeof c === "string" ? c : JSON.stringify(c.props.children)).join("").includes(label))!;
const settle = () => new Promise((resolve) => setTimeout(resolve, 10));
afterEach(() => {
  act(() => trees.splice(0).forEach((tree) => tree.unmount()));
  clients.splice(0).forEach((client) => client.clear());
  vi.unstubAllGlobals(); vi.clearAllMocks();
});
function normalField() {
  const field = structuredClone(TRUTH);
  field.process.fault_section = "none";
  for (const d of Object.values(field.devices)) {
    if (d.kind === "breaker") { d.position = "closed"; d.lockout = false; d.protection_tripped = false; }
    if (d.kind === "rmu") d.fault_passage = false;
    d.measurements.pf = .957;
  }
  return field;
}
function matchingOperator(field: FieldState): OperatorState {
  const read = physicalSystemObservations(field);
  return { ...FEEDER_STATE, points: FEEDER_STATE.points.map((p) => {
    const r = read(p.id);
    return r.condition === "good" ? { ...p, value: r.value ?? null } : p;
  }) };
}

describe("Physical System presentation", () => {
  it("shows a quiet normal state, every device and the required section order", () => {
    const field = normalField(), operator = matchingOperator(field);
    const body = <PhysicalSystemScreen {...props} field={field} operator={operator} />;
    const html = renderToStaticMarkup(<ControlCentreScreen state={operator} stateError={null} stateUpdatedAt={NOW} journal={JOURNAL} now={NOW} view="physical-system" physicalSystem={body} />);
    const out = text(html);
    for (const value of ["Physical System State", "Live electrical and device conditions from the simulation, independent of SCADA communications.", "Simulation observations · Independent of IEC 104", "No active physical abnormalities", "CB1 — Feeder breaker", "T1 — Transformer / OLTC", "RMU1", "RMU2 · NOP", "SCADA data quality:", "Physical agreement: 31 matched / 0 mismatched", "CC alarm limits 19.8–21.0 kV"]) expect(out).toContain(value);
    const sections = ["Physical abnormalities", "System overview", "Physical system vs. Control Centre", "Electrical network overview", "Equipment overview", "Advanced diagnostics"];
    const indices = sections.map((label) => html.indexOf(`aria-label="${label}"`));
    expect(indices.every((i) => i >= 0)).toBe(true);
    expect(indices).toEqual([...indices].sort((a, b) => a - b));
    expect(out).not.toContain("Instructor"); expect(out).not.toContain("faceplate");
    expect(html).toContain("max-w-full overflow-x-auto");
    expect(out).not.toContain("Solver epoch"); expect(out).not.toContain("Device audit");
  });
  it("renders the Physical System without a clock and declares its absence in Station time", () => {
    const field = { ...normalField(), clock: null };
    const tree = mount(<PhysicalSystemScreen {...props} field={field} />);
    expect(physicalAbnormalities(field).some((a) => a.id === "clock")).toBe(false);
    expect(output(tree)).toContain("No active physical abnormalities");
    expect(output(tree)).not.toContain("Time-sync warning");
    act(() => button(tree, "Advanced diagnostics").props.onClick());
    const stationTime = tree.root.findAllByType("h4").find((h) => h.children.join("") === "Station time")!.parent!;
    expect(stationTime.findByType("p").children.join("")).toBe("No station clock declared.");
    expect(output(tree)).toContain("Device diagnostics");
    expect(output(tree)).not.toContain("Time-sync warning");
  });
  it("keeps NOP open quiet, off-normal switching informational and clock warnings separate", () => {
    const field = normalField();
    expect(physicalAbnormalities(field)).toEqual([]);
    const rmu = field.devices["rmu-2"];
    if (rmu.kind === "rmu") rmu.position = "closed";
    expect(physicalAbnormalities(field)).toEqual([expect.objectContaining({ tone: "info", text: "RMU2 closed · off-normal switching" })]);
    field.clock = { ...field.clock!, sync_status: "freerun", alarm: true, comms_ok: false };
    expect(physicalAbnormalities(field).find((a) => a.id === "clock")).toMatchObject({ tone: "warning", text: expect.stringContaining("Time-sync warning") });
  });
  it("surfaces protection, interlocks, delivery failures and stale process inputs without opening diagnostics", () => {
    const field = structuredClone(TRUTH);
    const t1 = field.devices["oltc-1"], rmu = field.devices["rmu-1"];
    if (t1.kind === "oltc") { t1.blocked = true; t1.remote_enabled = false; }
    if (rmu.kind === "rmu") { rmu.earth_closed = true; rmu.measurements.valid = false; }
    field.devices["breaker-1"].protection.picked_up = true;
    field.process.devices["rmu-1"].delivered = false;
    field.process.truth_stale = true; field.process.f2_tripped = true;
    const out = text(renderToStaticMarkup(<PhysicalSystemScreen {...props} field={field} />));
    for (const value of ["CB1 protection TRIPPED", "CB1 LOCKOUT", "CB1 protection picked up", "RMU1 earth CLOSED", "RMU1 fault passage LATCHED", "T1 OLTC effective block", "T1 LOCAL control authority", "RMU1 measurements INVALID", "Fault section S1", "F2 TRIPPED", "device input STALE", "rmu-1 device delivery FAILED"]) expect(out).toContain(value);
  });
  it("shows physical API failure without inventing a SCADA outage", () => {
    const out = text(renderToStaticMarkup(<PhysicalSystemScreen {...props} stateError={new Error("device offline")} />));
    expect(out).toContain("Physical system unavailable: device offline");
    expect(out).toContain("Showing retained observations, not current conditions.");
    expect(out).not.toContain("Control centre API unavailable");
    expect(out).toContain("Physical topology path: unknown");
  });
  it.each(["holdover", "freerun"] as const)("preserves %s clock evidence and treats it as time synchronization", (sync_status) => {
    const field = { ...TRUTH, clock: { ...TRUTH.clock!, sync_status, alarm: true, comms_ok: false, time_offset_sec: -30 } };
    const out = text(renderToStaticMarkup(<PhysicalDiagnostics field={field} audit={null} />));
    expect(out).toContain(`Sync status ${sync_status.toUpperCase()}`);
    expect(out).toContain("Time offset -30.000 s");
    expect(out).toContain("Alarm / comms OK YES / no");
    expect(physicalAbnormalities(field).find((a) => a.id === "clock")?.text).toContain("Time-sync warning");
  });
  it("promotes a genuine mismatch with a red text-and-symbol badge and independent physical values", () => {
    const html = renderToStaticMarkup(<PhysicalSystemScreen {...props} />);
    expect(html).toContain("≠ MISMATCH"); expect(html).toContain("border-red-700");
    expect(html).toContain("CB1: OPEN · GOOD · sim");
    expect(html).toContain("F2 · closed (sim)");
    expect(html).toContain("Physical topology path: via F2");
    expect(html).toContain("ENERGIZED"); expect(html).toContain("solver bus live");
    expect(html).toContain("received 29 s ago"); expect(html).toContain("Source UTC");
    const noOperator = renderToStaticMarkup(<PhysicalSystemScreen {...props} operator={undefined} />);
    expect(noOperator).not.toContain("CC alarm limits"); expect(noOperator).toContain("? UNKNOWN");
  });
  it("distinguishes stale-but-equal from agreement and promotes secondary rows", () => {
    const field = normalField(), operator = matchingOperator(field);
    const p = operator.points.find((p) => p.id === "rmu-2-source-kv")!;
    p.quality = { ...p.quality, iv: true, nt: true }; p.stale_reason = "link_lost";
    const tree = mount(<PhysicalSystemScreen {...props} field={field} operator={operator} />);
    const row = tree.root.findAll((n) => n.props["data-comparison"] === p.id)[0];
    expect(row.props["data-status"]).toBe("STALE");
    expect(output(tree)).toContain("CC↔RTU link lost");
    expect(button(tree, "All comparable points (31)").props["aria-expanded"]).toBe(false);
  });
  it("never turns null into zero and distinguishes validity, freshness, energization and known positions", () => {
    const field = structuredClone(TRUTH);
    field.devices["oltc-1"].measurements.current_a = null;
    field.devices["oltc-1"].measurements.voltage_kv_ll = null;
    field.devices["rmu-1"].measurements.valid = false;
    field.process.truth_stale = true;
    delete field.devices["breaker-1"];
    const html = renderToStaticMarkup(<PhysicalSystemScreen {...props} field={field} />);
    expect(html).toContain("I unknown"); expect(html).toContain("Bus voltage");
    expect(html).toContain("CB1: unknown · UNKNOWN · sim");
    expect(html).toContain("INVALID · retained measurements");
    expect(html).toContain("F2 · unknown (sim)");
    expect(html).toContain("Physical topology path: unknown");
    expect(html).toContain("UNKNOWN · solver bus live");
  });
  it("shows solver energization separately from measured kV on the physical lower branch", () => {
    const field = structuredClone(TRUTH);
    field.process.buses.s3.live = false;
    const html = renderToStaticMarkup(<PhysicalSystemScreen {...props} field={field} />);
    expect(html).toContain("S3 industrial · LIVE");
    expect(html).toContain("NOT ENERGIZED · solver bus live");
    expect(html.match(/<line x1="337"[^>]*>/)?.[0]).toContain('stroke="#94a3b8"');
    expect(html.match(/<line x1="337"[^>]*>/)?.[0]).not.toContain("stroke-dasharray");
    field.process.truth_stale = true;
    const stale = renderToStaticMarkup(<PhysicalSystemScreen {...props} field={field} />);
    expect(stale.match(/<line x1="337"[^>]*>/)?.[0]).toContain('stroke="#fbbf24"');
  });
  it("distinguishes completed tap, target tap and motion", () => {
    const html = renderToStaticMarkup(<PhysicalSystemScreen {...props} />);
    expect(text(html)).toContain("Completed tap 1 MANUAL · moving · target 2");
    expect(html).toContain("Tap 1 · TRANSIENT · GOOD · sim");
  });
  it("retains every engineering group and applies only fields meaningful to the equipment", () => {
    const render = (id: string) => renderToStaticMarkup(<EquipmentCard id="CB1" title={id} device={TRUTH.devices[id]} open onToggle={() => {}} stale={false} />);
    const cb1 = text(render("breaker-1")), t1 = text(render("oltc-1")), rmu = text(render("rmu-1"));
    for (const value of ["Electrical", "Switching &amp; control", "Protection &amp; interlocks", "Events &amp; timestamps", "Diagnostics", "Active power P", "Reactive power Q", "Power factor PF", "Section / source voltage", "Section / source energization", "Control authority REMOTE", "Protection function", "Pickup / configured delay 400 A / 1000 ms", "Instantaneous pickup 3000 A", "Protection picked up", "Last trip current", "Revision / generation", "Event sequence", "Changed (UTC)", "Solver epoch", "Measurement received (UTC)"]) expect(cb1).toContain(value);
    expect(cb1).not.toContain("Completed tap"); expect(cb1).not.toContain("Earth closed");
    expect(t1).toContain("Target tap 2"); expect(t1).toContain("Manual block / armed"); expect(t1).toContain("PLC request result not_auto"); expect(t1).toContain("Heartbeat 200");
    expect(rmu).toContain("Earth closed"); expect(rmu).toContain("Fault passage"); expect(rmu).not.toContain("Lockout");
  });
  it("preserves solver, clock, delivery and audit evidence in advanced diagnostics", () => {
    const html = renderToStaticMarkup(<PhysicalDiagnostics field={TRUTH} audit={<AuditTable audit={AUDIT} />} />);
    const out = text(html);
    for (const value of ["Simulation engine", "OpenDSS", "Solver epoch e1", "Device input stale", "residential profile factor 0.800 ×", "F2 last trip current unknown", "Station time", "Sync status LOCKED", "Time offset 2.500 s", "Satellites 9", "Reported time (UTC) 2026-10-09T12:00:32.000Z", "Actual time (UTC) 2026-10-09T12:00:29.500Z", "Device diagnostics", "Heartbeat 200", "solved rev 5 / current 5", "close rejected modbus · station · 10.30.30.20:41000 lockout", "closed → open"]) expect(out).toContain(value);
    expect(out.indexOf("first_solve")).toBeLessThan(out.indexOf("local_protection_trip"));
  });
});

describe("Physical System interactions", () => {
  it("expands from overview, keeps state and scroll fixed during polling, and discloses matching secondary rows", () => {
    const tree = mount(<PhysicalSystemScreen {...props} />);
    act(() => button(tree, "Feeder breaker").props.onClick());
    expect(button(tree, "CB1 — Feeder breaker").props["aria-expanded"]).toBe(true);
    act(() => button(tree, "Advanced diagnostics").props.onClick());
    const calls = scroll.mock.calls.length;
    act(() => tree.update(<QueryClientProvider client={clients[0]}><PhysicalSystemScreen {...props} field={structuredClone(TRUTH)} /></QueryClientProvider>));
    expect(button(tree, "Advanced diagnostics").props["aria-expanded"]).toBe(true);
    expect(button(tree, "CB1 — Feeder breaker").props["aria-expanded"]).toBe(true);
    expect(scroll).toHaveBeenCalledTimes(calls);
    act(() => button(tree, "SCADA agreement").props.onClick());
    expect(button(tree, "All comparable points (31)").props["aria-expanded"]).toBe(true);
  });
  it("shares device selection across views in both directions", async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes("/field/state") ? TRUTH : url.includes("journal") ? JOURNAL : FEEDER_STATE)));
    vi.stubGlobal("fetch", fetchMock);
    const location = { search: "", href: "http://localhost/substation" };
    vi.stubGlobal("window", { location, history: { pushState: (_: unknown, _title: string, url: URL) => { location.search = url.search; location.href = url.href; } }, addEventListener: vi.fn(), removeEventListener: vi.fn() });
    const tree = mount(<ControlCentreView />);
    await act(async () => { await settle(); });
    act(() => tree.root.findAllByType("g").find((n) => n.props.role === "button" && n.props["aria-label"].startsWith("CB1:"))!.props.onClick());
    act(() => button(tree, "Physical System").props.onClick());
    await act(async () => { await settle(); });
    expect(location.search).toBe("?view=physical-system");
    expect(button(tree, "CB1 — Feeder breaker").props["aria-expanded"]).toBe(true);
    expect(selector).toHaveBeenCalledWith("#physical-equipment-CB1");
    expect(tree.root.findAllByType("g").find((n) => n.props.role === "button" && n.props["aria-label"].startsWith("CB1:"))!.props["aria-pressed"]).toBe(true);
    act(() => tree.root.findAllByType("g").find((n) => n.props.role === "button" && n.props["aria-label"].startsWith("RMU2 ·"))!.props.onClick());
    act(() => button(tree, "Control Centre").props.onClick());
    expect(output(tree)).toContain("RMU2 faceplate");
  });
  it("retains audit evidence on audit failure independently of physical and SCADA availability", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (url: string) => {
      if (url.includes("/field/state")) return new Response(JSON.stringify(TRUTH));
      throw new Error("audit offline");
    }));
    const tree = mount(<PhysicalSystem operator={FEEDER_STATE} now={NOW} />);
    await act(async () => { await settle(); });
    clients[0].setQueryData(["field", "audit", "breaker-1"], AUDIT);
    await act(async () => { button(tree, "Advanced diagnostics").props.onClick(); await settle(); });
    expect(output(tree)).toContain("audit offline");
    expect(output(tree)).toContain("Showing the last answer.");
    expect(output(tree)).toContain("rejected");
    expect(output(tree)).not.toContain("Physical system unavailable");
    expect(output(tree)).not.toContain("Control centre API unavailable");
  });
  it("polls audit only inside the expanded advanced section and retains selected device through data refresh", async () => {
    const fetchMock = vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes("/field/state") ? TRUTH : AUDIT)));
    vi.stubGlobal("fetch", fetchMock);
    const tree = mount(<PhysicalSystem operator={FEEDER_STATE} now={NOW} />);
    await act(async () => { await settle(); });
    expect(fetchMock.mock.calls.some(([url]) => url.includes("audit"))).toBe(false);
    await act(async () => { button(tree, "Advanced diagnostics").props.onClick(); await settle(); });
    await act(async () => { tree.root.findByType("select").props.onChange({ target: { value: "rmu-2" } }); await settle(); });
    expect(fetchMock.mock.calls.some(([url]) => url.endsWith("device=rmu-2"))).toBe(true);
    act(() => { clients[0].setQueryData(["field", "state"], structuredClone(TRUTH)); });
    expect(tree.root.findByType("select").props.value).toBe("rmu-2");
    act(() => button(tree, "Advanced diagnostics").props.onClick());
    expect(clients[0].getQueryCache().findAll({ queryKey: ["field", "audit"] }).every((q) => q.getObserversCount() === 0)).toBe(true);
  });
});
