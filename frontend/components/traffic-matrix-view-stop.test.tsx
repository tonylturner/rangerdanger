// @vitest-environment jsdom
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import type { LabGraph } from "../lib/api";
import { createFlowContext } from "../lib/observed-flows";
import { TooltipProvider } from "./ui/tooltip";
import { TrafficMatrixView } from "./traffic-matrix-view";

// Radix's tooltip event handlers are excluded from these action tests.
vi.mock("./ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: ReactNode }) => children,
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const clients: QueryClient[] = [];
afterEach(() => {
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
  const view = render(<QueryClientProvider client={client}><TooltipProvider><TrafficMatrixView context={context} /></TooltipProvider></QueryClientProvider>);
  const stop = () => [...view.container.querySelectorAll("button")].find((button) => ["Stop", "Stopping…"].includes(button.textContent?.trim() ?? ""))!;
  return { view, stop, refresh, client };
}

function expectBothRefreshed(refresh: ReturnType<typeof mount>["refresh"]) {
  expect(refresh).toHaveBeenCalledWith({ queryKey: ["traffic-status"] });
  expect(refresh).toHaveBeenCalledWith({ queryKey: ["pcap-status"] });
}

describe("Traffic Matrix Stop", () => {
  it.each([["US", usFixture], ["EU", euFixture]])("stops both the observed %s traffic run and capture, then refreshes both", async (_, graph) => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopping"}')));
    vi.stubGlobal("fetch", fetch);
    const { view, stop, refresh } = mount(graph as LabGraph);
    fireEvent.click(stop());
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({
      method: "POST", body: JSON.stringify({ run_id: "observed-run" }),
    }));
    expect(fetch).toHaveBeenCalledWith("/api/pcap/stop", expect.objectContaining({ method: "POST" }));
    expectBothRefreshed(refresh);
    expect(view.container.querySelectorAll('[role="alert"]')).toHaveLength(0);
  });

  it.each(["traffic", "pcap"])("attempts both stops and refreshes both after a %s failure", async (failed) => {
    let finishOther!: (value: Response) => void;
    const fetch = vi.fn((path: string) => path === `/api/${failed}/stop`
      ? Promise.resolve(new Response('{"error":"cannot stop"}', { status: 500 }))
      : new Promise<Response>((resolve) => { finishOther = resolve; }));
    vi.stubGlobal("fetch", fetch);
    const { view, stop, refresh } = mount(usFixture as LabGraph);
    fireEvent.click(stop());
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(stop().hasAttribute("disabled")).toBe(true);
    expect(refresh).not.toHaveBeenCalled();
    await act(async () => {
      finishOther(new Response('{"status":"stopped"}'));
      await waitFor(() => expectBothRefreshed(refresh));
    });
    await waitFor(() => expect(view.container.querySelector('[role="alert"]')?.textContent).toBe(
      `${failed === "traffic" ? "Traffic" : "Capture"}: 500 cannot stop`,
    ));
    expect(stop().hasAttribute("disabled")).toBe(false);
    // Retry clears the previous error as well as reattempting both calls.
    fetch.mockImplementation(() => Promise.resolve(new Response('{"status":"stopped"}')));
    fireEvent.click(stop());
    await waitFor(() => expect(view.container.querySelectorAll('[role="alert"]')).toHaveLength(0));
  });

  it("reports both failures instead of hiding one", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response('{"error":"unavailable"}', { status: 500 }))));
    const { view, stop, refresh } = mount(usFixture as LabGraph);
    fireEvent.click(stop());
    await waitFor(() => expect(view.container.querySelector('[role="alert"]')?.textContent).toBe("Traffic: 500 unavailable; Capture: 500 unavailable"));
    expectBothRefreshed(refresh);
  });

  it("offers Stop when only traffic is still running", async () => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopped"}')));
    vi.stubGlobal("fetch", fetch);
    const { stop, refresh } = mount(usFixture as LabGraph, { capturing: false });
    expect(stop()).toBeTruthy();
    fireEvent.click(stop());
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    expect(fetch).toHaveBeenCalledTimes(2);
    expectBothRefreshed(refresh);
  });

  it("stops capture alone for a package without generation", async () => {
    const fetch = vi.fn(() => Promise.resolve(new Response('{"status":"stopped"}')));
    vi.stubGlobal("fetch", fetch);
    const graph = { ...euFixture, operations: { traffic_generation: false } } as LabGraph;
    const { stop, refresh } = mount(graph, { generating: false });
    fireEvent.click(stop());
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
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
    const { view, stop, client } = mount(usFixture as LabGraph, { generating: false });
    fireEvent.click(view.getByLabelText("Generate Traffic"));
    await waitFor(() => expect(client.getQueryData(["traffic-status"])).toMatchObject({ run_id: "new-run", generating: true }));
    expect(client.getQueryData(["traffic-status"])).toMatchObject({ run_id: "new-run", generating: true });
    fireEvent.click(stop());
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({ body: '{"run_id":"new-run"}' })));
    expect(fetch).toHaveBeenCalledWith("/api/traffic/stop", expect.objectContaining({ body: '{"run_id":"new-run"}' }));
    expect(fetch).not.toHaveBeenCalledWith("/api/traffic/status", expect.anything());
  });
});
