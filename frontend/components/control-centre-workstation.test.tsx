// @vitest-environment jsdom
import { fireEvent, render, screen as rtlScreen, waitFor, type RenderResult } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { AUDIT, TRUTH, BOOT_STATE, EU_PACKAGE, FEEDER_STATE, JOURNAL, NOW, PERIODIC_GI_STATE, WATCHDOG_STATE, commandResult } from "../lib/operator-fixtures.test-util";
import { ControlCentreScreen, InterrogateStation, LinkDetails, viewFromSearch } from "./control-centre-view";
import { ControlCentreOneLine } from "./control-centre-one-line";
import { resolveOperatorView } from "../lib/operator-view";
import { ControlCentreMiniStatus, RunnerOperatorStatus } from "./runner-operator-status";
import { PhysicalSystem } from "./physical-system";
import type { DeviceId } from "../lib/operator-workstation";
import { HistoryPlot } from "./historian-trend";
import { useOperatorState } from "../lib/operator-queries";
import { retryQuery } from "../lib/query-retry";
import liveState from "../lib/fixtures/operator-state-live.json";
import { HISTORY } from "../lib/historian-fixtures.test-util";

// Navigation is outside this node-environment interaction test.
vi.mock("next/link", () => ({ default: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a> }));

const clients: QueryClient[] = [];
function mount(node: ReactNode, usePortalRetry = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: usePortalRetry ? retryQuery : false, retryDelay: 0 } } }); clients.push(client);
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}
function screen(patch: Partial<Parameters<typeof ControlCentreScreen>[0]> = {}) {
  return <InteractiveScreen state={FEEDER_STATE} stateError={null} stateUpdatedAt={NOW - 1000} journal={JOURNAL} now={NOW} {...patch} />;
}
function InteractiveScreen(props: Parameters<typeof ControlCentreScreen>[0]) {
  const [selected, onSelect] = useState<DeviceId>("T1");
  return <ControlCentreScreen {...props} selected={selected} onSelect={onSelect} />;
}
function button(tree: RenderResult, text: string) {
  return [...tree.container.querySelectorAll("button")].find((b) => b.textContent?.includes(text));
}
const output = (tree: RenderResult) => tree.container.innerHTML;
afterEach(() => { clients.splice(0).forEach((c) => c.clear()); vi.unstubAllGlobals(); });

function OperatorStateScreenProbe() {
  const query = useOperatorState(60_000);
  return <ControlCentreScreen state={query.data} stateError={query.error} stateUpdatedAt={query.dataUpdatedAt} now={NOW} journal={JOURNAL} />;
}

describe("operator state failure visibility", () => {
  it("shows schema failure as unavailable after the first response, without silent retries", async () => {
    const malformed = structuredClone(liveState);
    malformed.schema = 1;
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify(malformed)));
    vi.stubGlobal("fetch", fetchMock);
    const tree = mount(<OperatorStateScreenProbe />, true);
    await waitFor(() => expect(output(tree)).toContain("Expected CC state schema 3"));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(output(tree)).toContain("Control centre API unavailable");
    expect(output(tree)).toContain("Expected CC state schema 3");
    expect(output(tree)).not.toContain("Loading the control centre");
    expect(output(tree)).not.toContain("waiting");
  });
  it("retains network retries and renders the live-shaped snapshot after recovery", async () => {
    const fetchMock = vi.fn().mockRejectedValueOnce(new TypeError("network offline"))
      .mockImplementation(async () => new Response(JSON.stringify(liveState)));
    vi.stubGlobal("fetch", fetchMock);
    const tree = mount(<OperatorStateScreenProbe />, true);
    await waitFor(() => expect(output(tree)).toContain("T1 faceplate"));
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(output(tree)).toContain("T1 faceplate");
    expect(tree.container.querySelector('[aria-label="CC observation snapshot"]')?.getAttribute("data-cached")).toBe("false");
    expect(tree.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(output(tree)).not.toContain("Loading the control centre");
  });
});

describe("operator workstation interactions", () => {
  it("selects every device by click, Enter or Space and defaults to T1", () => {
    const tree = mount(screen());
    expect(tree.container.querySelectorAll('[aria-label="T1 faceplate"]')).toHaveLength(1);
    const select = (id: string, key?: string) => {
      const symbol = tree.container.querySelector<SVGGElement>(`g[role="button"][aria-label^="${id}"]`)!;
      if (key) fireEvent.keyDown(symbol, { key });
      else fireEvent.click(symbol);
    };
    select("CB1", "Enter"); expect(output(tree)).toContain("CB1 faceplate");
    select("RMU1", " "); expect(output(tree)).toContain("RMU1 faceplate");
    select("RMU2"); expect(output(tree)).toContain("RMU2 faceplate");
    select("F2", "Enter"); expect(output(tree)).toContain("F2 faceplate");
    expect(output(tree)).toContain("Indication only");
    expect(button(tree, "Confirm")).toBeUndefined();
    select("T1"); expect(output(tree)).toContain("Configured delay only");
  });
  it.each([
    ["T1", "oltc-1-remote", true, "REMOTE"],
    ["RMU1", "rmu-1-remote", false, "LOCAL"],
    ["RMU2", "rmu-2-remote", null, "not established"],
    ["CB1", null, true, "not established"],
  ] as const)("confirms %s with received authority only, still subject to interlocks", (device, id, value, authority) => {
    const state = structuredClone(FEEDER_STATE);
    if (id) state.points.find((p) => p.id === id)!.value = value;
    const tree = mount(screen({ state }));
    fireEvent.click(tree.container.querySelector<SVGGElement>(`g[role="button"][aria-label^="${device}"]`)!);
    fireEvent.click(button(tree, "Confirm")!);
    expect(output(tree)).toContain(`Received control authority: `);
    expect(output(tree)).toContain(authority);
    expect(output(tree)).toContain("This attempt is still subject to device interlocks.");
  });
  it("cancels local arming on device change; one POST does wire SBO; late result retains CB1 attribution", async () => {
    let resolve!: (response: Response) => void;
    const fetchMock = vi.fn().mockImplementation(() => new Promise<Response>((r) => { resolve = r; })); vi.stubGlobal("fetch", fetchMock);
    const tree = mount(screen());
    const select = (label: string) => fireEvent.click(tree.container.querySelector<SVGGElement>(`g[role="button"][aria-label^="${label}"]`)!);
    select("CB1"); fireEvent.click(button(tree, "Confirm OPEN")!);
    expect(output(tree)).toContain("No RTU reservation"); expect(fetchMock).not.toHaveBeenCalled();
    select("RMU1");
    expect(button(tree, "Execute OPEN")).toBeUndefined();
    expect(output(tree)).not.toContain("No RTU reservation");
    select("CB1"); expect(button(tree, "Execute OPEN")).toBeUndefined();
    fireEvent.click(button(tree, "Confirm OPEN")!);
    fireEvent.click(button(tree, "Execute OPEN")!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ point: "feeder-breaker-command", operation: "open" });
    select("T1"); expect(button(tree, "Confirm RAISE")?.hasAttribute("disabled")).toBe(true);
    resolve(new Response(JSON.stringify(commandResult("completed"))));
    await waitFor(() => expect(output(tree)).toContain("COMPLETED"));
    expect(output(tree)).toContain("CB1 feeder breaker"); expect(output(tree)).toContain("feeder-breaker-command");
    expect(output(tree)).toContain("T1 faceplate"); expect(output(tree)).toContain("COMPLETED");
    expect(output(tree)).toContain("IOA 1001: OPEN at 12:00:01.600");
    expect(tree.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(output(tree)).not.toContain("command outcome is unknown");
  });
  it("marks the runner's retained snapshot when its operator API fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("operator proxy offline")));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
    client.setQueryData(["operator", "state"], FEEDER_STATE);
    const tree = render(<QueryClientProvider client={client}><RunnerOperatorStatus resolution={resolveOperatorView(EU_PACKAGE, null)} /></QueryClientProvider>);
    await waitFor(() => expect(output(tree)).toContain("operator proxy offline"));
    expect(output(tree)).toContain("Operator API unavailable");
    expect(output(tree)).toContain("Cached snapshot");
    expect(output(tree)).toContain("operator proxy offline");
  });
  it("keeps commands admitted and integrity true during a later GI, with no receipt-age rule", () => {
    const state = structuredClone(PERIODIC_GI_STATE);
    state.points.forEach((p) => { p.received_at = "2020-01-01T00:00:00Z"; });
    const tree = mount(screen({ state }));
    expect(button(tree, "Confirm RAISE")?.hasAttribute("disabled")).toBe(false);
    expect(button(tree, "Interrogate station")?.hasAttribute("disabled")).toBe(true);
    expect(renderToStaticMarkup(<LinkDetails link={state.link} now={NOW} />)).toContain("GI ok");
    expect(renderToStaticMarkup(<ControlCentreMiniStatus state={state} waiting={false} />)).toContain("GI ok");
  });
  it("opens bad-quality Points without turning an uncertain bus into a process alarm", () => {
    const tree = mount(screen({ state: WATCHDOG_STATE }));
    fireEvent.click(button(tree, "1 point with bad quality")!);
    expect((tree.container.querySelector("select") as HTMLSelectElement).value).toBe("bad quality");
    expect(tree.container.querySelectorAll("tbody tr")).toHaveLength(1);
    expect(output(tree)).toContain("not_updated");
    fireEvent.change(tree.container.querySelector("select")!, { target: { value: "status" } });
    expect(tree.container.querySelectorAll("tbody tr")).toHaveLength(21);
  });
  it("marks the complete cached surface and shows journal failures, without rewriting GOOD", () => {
    const state = structuredClone(FEEDER_STATE);
    const tree = mount(screen({ state, stateError: new Error("proxy offline"), journalError: new Error("journal offline"), initialTab: "Points" }));
    expect(output(tree).match(/Cached snapshot/g)).toHaveLength(4);
    expect(output(tree)).toContain("1.0 s"); expect(tree.container.querySelector('[aria-label="CC observation snapshot"]')?.getAttribute("data-cached")).toBe("true");
    expect(button(tree, "Confirm RAISE")?.hasAttribute("disabled")).toBe(true);
    expect(state).toEqual(FEEDER_STATE);
    fireEvent.click(button(tree, "Events")!); expect(output(tree)).toContain("Journal unavailable: "); expect(output(tree)).toContain("journal offline"); expect(output(tree)).toContain("cached events");
  });
  it("does not mount Physical System in the CC view or the CC schematic in Physical System", () => {
    expect(output(mount(screen({ physicalSystem: <div>FIELD-ONLY</div> })))).not.toContain("FIELD-ONLY");
    const field = output(mount(screen({ view: "physical-system", physicalSystem: <div>FIELD-ONLY</div> })));
    expect(field).toContain("FIELD-ONLY"); expect(field).not.toContain("CC-observed CIGRE");
    expect(viewFromSearch("?view=physical-system")).toBe("physical-system"); expect(viewFromSearch("?view=field-truth")).toBe("control-centre"); expect(viewFromSearch("?view=unknown")).toBe("control-centre");
  });
  it("shows received ASDU and reporting, and labels the trend band as current only", () => {
    const tree = mount(screen({ initialTab: "Points" }));
    expect(output(tree)).toContain("M_DP_TB_1"); expect(output(tree)).toContain("cyclic 10 s · deadband 0.1 kV");
    expect(renderToStaticMarkup(<HistoryPlot history={HISTORY} band={{ setpoint: 20, low: 19.8, high: 20.2, bus: 20.02 }} />)).toContain("not historical setpoints");
  });
  it("keeps switch geometry and uncertainty distinct", () => {
    const state = structuredClone(FEEDER_STATE);
    state.points.find((p) => p.id === "rmu-1-position")!.value = "intermediate";
    state.points.find((p) => p.id === "rmu-2-position")!.value = "indeterminate";
    const html = renderToStaticMarkup(<ControlCentreOneLine points={state.points} />);
    expect(html).toContain("½"); expect(html).toContain("×"); expect(html).toContain('fill="#cbd5e1"');
    expect(renderToStaticMarkup(<ControlCentreOneLine points={BOOT_STATE.points} />)).toContain('fill="url(#cc-unknown)"');
  });
});

describe("operator interrogation button", () => {
  it.each([409, 503])("shows %s without retrying", async (status) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: status === 409 ? "interrogation already running" : "link not started" }), { status })); vi.stubGlobal("fetch", fetchMock);
    const tree = mount(<InterrogateStation state={FEEDER_STATE} unavailable={false} />);
    fireEvent.click(button(tree, "Interrogate station")!);
    await waitFor(() => expect(output(tree)).toContain("No automatic retry"));
    expect(fetchMock).toHaveBeenCalledTimes(1); expect(output(tree)).toContain(String(status)); expect(output(tree)).toContain("No automatic retry");
  });
  it("disables on link loss or API failure and distinguishes accepted from completed", async () => {
    expect(button(mount(<InterrogateStation state={BOOT_STATE} unavailable={false} />), "Interrogate station")?.hasAttribute("disabled")).toBe(true);
    expect(button(mount(<InterrogateStation state={FEEDER_STATE} unavailable />), "Interrogate station")?.hasAttribute("disabled")).toBe(true);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ trigger: "operator", requested_at: "2026-10-09T12:00:30Z" }), { status: 202 })));
    const tree = mount(<InterrogateStation state={FEEDER_STATE} unavailable={false} />);
    fireEvent.click(button(tree, "Interrogate station")!);
    await waitFor(() => expect(output(tree)).toContain("awaiting CC start"));
    expect(output(tree)).toContain("awaiting CC start"); expect(button(tree, "Interrogate station")?.hasAttribute("disabled")).toBe(true);
  });
});

it("queries each selected device audit with an independent cache identity", async () => {
  const fetchMock = vi.fn().mockImplementation(async (url: string) => new Response(JSON.stringify(url.includes("/field/state") ? TRUTH : { ...AUDIT, entries: [] })));
  vi.stubGlobal("fetch", fetchMock);
  const tree = mount(<PhysicalSystem operator={FEEDER_STATE} now={NOW} />);
  await waitFor(() => expect(button(tree, "Advanced diagnostics")).toBeDefined());
  expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/field/audit"))).toBe(false);
  fireEvent.click(button(tree, "Advanced diagnostics")!);
  await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/field/audit?after=0&device=breaker-1"))).toBe(true));
  expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/field/audit?after=0&device=breaker-1"))).toBe(true);
  for (const device of ["rmu-1", "rmu-2", "oltc-1", "f2-breaker"]) {
    fireEvent.change(rtlScreen.getByRole("combobox", { name: "Audit device" }), { target: { value: device } });
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith(`/field/audit?after=0&device=${device}`))).toBe(true));
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith(`/field/audit?after=0&device=${device}`))).toBe(true);
  }
  expect(clients.at(-1)!.getQueryCache().findAll({ queryKey: ["field", "audit"] })).toHaveLength(5);
});
