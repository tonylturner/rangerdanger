import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import type { LabGraph } from "../lib/api";
import { createFlowContext } from "../lib/observed-flows";
import { TooltipProvider } from "./ui/tooltip";
import { TrafficMatrixView } from "./traffic-matrix-view";

// Action tests run in Node; Radix's tooltip event handlers require a DOM.
vi.mock("./ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: ReactNode }) => children,
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const clients: QueryClient[] = [], trees: ReactTestRenderer[] = [];
afterEach(() => {
  act(() => trees.splice(0).forEach((tree) => tree.unmount()));
  clients.splice(0).forEach((client) => client.clear());
  vi.unstubAllGlobals();
});

function mount(graph: LabGraph, { capturing = true, generating = true, runID = "observed-run" } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  clients.push(client);
  client.setQueryData(["pcap-status"], { capturing, files: [] });
  const refresh = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
  const context = createFlowContext(graph, {
    traffic: { generating, started_at: new Date().toISOString(), flows_generated: 2, run_id: runID },
  });
  let tree!: ReactTestRenderer;
  act(() => {
    tree = create(<QueryClientProvider client={client}><TooltipProvider><TrafficMatrixView context={context} /></TooltipProvider></QueryClientProvider>);
  });
  trees.push(tree);
  const stop = () => tree.root.findAllByType("button").find((b) => b.children.includes("Stop") || b.children.includes("Stopping…"))!;
  return { tree, stop, refresh, client };
}

function expectBothRefreshed(refresh: ReturnType<typeof mount>["refresh"]) {
  expect(refresh).toHaveBeenCalledWith({ queryKey: ["traffic-status"] });
  expect(refresh).toHaveBeenCalledWith({ queryKey: ["pcap-status"] });
}

describe("Traffic Matrix Stop", () => {
  it.each([["US", usFixture], ["EU", euFixture]])("stops both the observed %s traffic run and capture, then refreshes both", async (_, graph) => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopping"}')));
    vi.stubGlobal("fetch", fetch);
    const { tree, stop, refresh } = mount(graph as LabGraph);
    await act(async () => { await stop().props.onClick(); });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({
      method: "POST", body: JSON.stringify({ run_id: "observed-run" }),
    }));
    expect(fetch).toHaveBeenCalledWith("/api/pcap/stop", expect.objectContaining({ method: "POST" }));
    expectBothRefreshed(refresh);
    expect(tree.root.findAllByProps({ role: "alert" })).toHaveLength(0);
  });

  it.each(["traffic", "pcap"])("attempts both stops and refreshes both after a %s failure", async (failed) => {
    let finishOther!: (value: Response) => void;
    const fetch = vi.fn((path: string) => path === `/api/${failed}/stop`
      ? Promise.resolve(new Response('{"error":"cannot stop"}', { status: 500 }))
      : new Promise<Response>((resolve) => { finishOther = resolve; }));
    vi.stubGlobal("fetch", fetch);
    const { tree, stop, refresh } = mount(usFixture as LabGraph);
    let pending!: Promise<void>;
    await act(async () => { pending = stop().props.onClick(); });
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(stop().props.disabled).toBe(true);
    expect(refresh).not.toHaveBeenCalled();
    await act(async () => {
      finishOther(new Response('{"status":"stopped"}'));
      await pending;
    });
    expectBothRefreshed(refresh);
    expect(tree.root.findByProps({ role: "alert" }).children.join("")).toBe(
      `${failed === "traffic" ? "Traffic" : "Capture"}: 500 cannot stop`,
    );
    expect(stop().props.disabled).toBe(false);
    // Retry clears the previous error as well as reattempting both calls.
    fetch.mockImplementation(() => Promise.resolve(new Response('{"status":"stopped"}')));
    await act(async () => { await stop().props.onClick(); });
    expect(tree.root.findAllByProps({ role: "alert" })).toHaveLength(0);
  });

  it("reports both failures instead of hiding one", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response('{"error":"unavailable"}', { status: 500 }))));
    const { tree, stop, refresh } = mount(usFixture as LabGraph);
    await act(async () => { await stop().props.onClick(); });
    expect(tree.root.findByProps({ role: "alert" }).children.join("")).toBe("Traffic: 500 unavailable; Capture: 500 unavailable");
    expectBothRefreshed(refresh);
  });

  it("offers Stop when only traffic is still running", async () => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopped"}')));
    vi.stubGlobal("fetch", fetch);
    const { stop, refresh } = mount(usFixture as LabGraph, { capturing: false });
    expect(stop()).toBeDefined();
    await act(async () => { await stop().props.onClick(); });
    expect(fetch).toHaveBeenCalledTimes(2);
    expectBothRefreshed(refresh);
  });

  it("stops capture alone for a package without generation", async () => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopped"}')));
    vi.stubGlobal("fetch", fetch);
    const graph = { ...euFixture, operations: { traffic_generation: false } } as LabGraph;
    const { stop, refresh } = mount(graph, { generating: false });
    await act(async () => { await stop().props.onClick(); });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledWith("/api/pcap/stop", expect.objectContaining({ method: "POST" }));
    expectBothRefreshed(refresh);
  });

  it("uses the returned run ID immediately after Generate, before polling catches up", async () => {
    const fetch = vi.fn((path: string) => Promise.resolve(new Response(path === "/api/traffic/generate"
      ? '{"status":"generating","duration_sec":30,"run_id":"new-run","started_at":"2026-10-25T12:00:00Z"}'
      : '{"status":"stopped"}')));
    vi.stubGlobal("fetch", fetch);
    // Capture is already running, but the prop's traffic snapshot is idle.
    const { tree, stop, client } = mount(usFixture as LabGraph, { generating: false });
    await act(async () => { await tree.root.findByProps({ "aria-label": "Generate Traffic" }).props.onClick(); });
    expect(client.getQueryData(["traffic-status"])).toMatchObject({ run_id: "new-run", generating: true });
    await act(async () => { await stop().props.onClick(); });
    expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({ body: '{"run_id":"new-run"}' }));
    expect(fetch).not.toHaveBeenCalledWith("/api/traffic/status", expect.anything());
  });
});
