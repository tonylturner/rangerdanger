import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";
import type { LabGraph } from "../lib/api";
import { createFlowContext } from "../lib/observed-flows";
import { useNetworkFlowContext } from "../lib/network-flow-queries";
import { resetRangeScopedQueries } from "../lib/range";
import { TooltipProvider } from "./ui/tooltip";
import { PolicyBadge } from "./network-drawers";
import { US_PACKAGE, EU_PACKAGE, FRESH_STATE } from "../lib/operator-fixtures.test-util";
import { TrafficMatrixView } from "./traffic-matrix-view";

vi.mock("./range-context", () => ({
  useRange: () => ({ status: { package: "eu-iec104-substation", generation: 7, phase: "ready" } }),
}));

function render(graph: LabGraph, filter = { zone: "all", crossZoneOnly: false }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const markup = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <TooltipProvider><TrafficMatrixView context={createFlowContext(graph, {})} filter={filter} /></TooltipProvider>
    </QueryClientProvider>,
  );
  client.clear();
  return markup;
}

describe("TrafficMatrixView", () => {
  it("renders EU's real pairs, unknown GPS, generate+capture controls and package filters", () => {
    const out = render(euFixture as LabGraph);
    expect(out).toContain("cc-1");
    expect(out).toContain("rtu-1");
    expect(out).toContain("breaker-1");
    expect(out).toContain("gps-1");
    expect(out).toContain("kali-1");
    expect(out).not.toContain("rtac-1");
    expect(out).not.toContain("relay-1");
    // The EU workshop drives scenario traffic in lab 1.2, same as US, so the
    // generator controls are present (traffic_generation is true).
    expect(out).toContain('aria-label="Generate Traffic"');
    expect(out).toContain("Generate + PCAP");
    expect(out).toContain("scenario traffic generator");
    expect(out).toContain("sessions show conntrack entries, not delivery; traffic may already be blocked");
    expect(out).not.toContain("RTAC device comms");
    expect(out).toContain("unknown");
    expect(out).toContain('value="control_centre_net"');
    expect(out).toContain('value="station_net"');
    expect(out).toContain("process/truth plane is intentionally outside");
    expect(out).not.toContain("Last seen:");
  });

  it("uses exactly the canvas filters, including same-zone GPS", () => {
    const cross = render(euFixture as LabGraph, { zone: "all", crossZoneOnly: true });
    expect(cross).not.toContain(">gps-1<");
    expect(cross).toContain(">cc-1<");
    const cc = render(euFixture as LabGraph, { zone: "control_centre_net", crossZoneOnly: false });
    expect(cc).toContain(">cc-1<");
    expect(cc).not.toContain(">kali-1<");
    expect(cc).not.toContain(">breaker-1<");
  });

  it("retains US generation controls and protocol labels", () => {
    const out = render(usFixture as LabGraph);
    expect(out).toContain('aria-label="Generate Traffic"');
    expect(out).toContain("Generate + PCAP");
    expect(out).toContain("RTAC device comms");
    expect(out).toContain("DNP3");
    expect(out).toContain("NTP");
    expect(out).not.toContain("cc-1");
  });

  it("names custom and other declared policies without calling them Weak Baseline", () => {
    for (const pkg of [US_PACKAGE, EU_PACKAGE]) {
      const client = new QueryClient();
      client.setQueryData(["packages", "active"], pkg);
      for (const active of ["custom", "another-policy"]) {
        const out = renderToStaticMarkup(<QueryClientProvider client={client}><PolicyBadge activeConfig={active} /></QueryClientProvider>);
        expect(out).toContain(active === "custom" ? "Custom policy" : active);
        expect(out).not.toContain("Weak baseline");
      }
      client.clear();
    }
  });

  it("does not select US flows for an empty package", () => {
    const out = render({ ...euFixture, flows: [], liveness: {} } as LabGraph);
    expect(out).not.toContain(">cc-1<");
    expect(out).not.toContain(">rtac-1<");
  });

  it("mounts each poller from its own declared trigger: generator by operation, operator and sessions by provider", () => {
    function ReadContext({ graph }: { graph: LabGraph }) {
      useNetworkFlowContext(graph, undefined);
      return null;
    }
    for (const graph of [euFixture as LabGraph, usFixture as LabGraph]) {
      const providers = Object.values(graph.liveness ?? {}).map((b) => b.provider);
      const hasOperator = providers.some((p) => p === "operator-link" || p === "operator-device");
      const hasSessions = providers.includes("session-presence");
      const client = new QueryClient();
      renderToStaticMarkup(<QueryClientProvider client={client}><ReadContext graph={graph} /></QueryClientProvider>);
      const queries = client.getQueryCache();
      // The generator poll follows the package operation; the operator and
      // session polls follow the declared liveness providers, independently.
      expect(queries.find({ queryKey: ["traffic-status"] })?.options).toMatchObject({ enabled: graph.operations.traffic_generation });
      expect(queries.find({ queryKey: ["operator", "state"] })?.options).toMatchObject({ enabled: hasOperator });
      expect(queries.find({ queryKey: ["firewall-sessions", "all"] })?.options).toMatchObject({ enabled: hasSessions });
      client.clear();
    }
  });

  it("drops every shared flow observation on range resets", async () => {
    const client = new QueryClient();
    const keys = [["workshop", "graph"], ["workshop", "status"], ["traffic-status"], ["operator", "state"], ["firewall-sessions", "all"]];
    for (const key of keys) client.setQueryData(key, { old: true });
    await resetRangeScopedQueries(client);
    for (const key of keys) expect(client.getQueryData(key)).toBeUndefined();
    client.clear();
  });

  it("rejects stale/failed session polls while keeping the EU scenario generator", () => {
    function ReadContext() {
      const context = useNetworkFlowContext(euFixture as LabGraph, undefined)!;
      return <span>{context.observations["attacker-rtu"].status}/{context.observations["rtu-breaker"].status}/{context.traffic ? "gen" : "none"}: {context.observations["attacker-rtu"].reason}</span>;
    }
    // SSR creates a new observer for each snapshot. Keep it from replacing
    // the fixture's in-flight retry state with a new mount-time fetch.
    const client = new QueryClient({ defaultOptions: { queries: { refetchOnMount: false } } });
    const sessionKey = ["firewall-sessions", "all"];
    const sessionList = { sessions: [{ proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20", dport: 2404, sport: null, state: "ESTABLISHED", timeout: 60 }], count: 1 };
    client.setQueryData(["traffic-status"], { generating: true });
    client.setQueryData(["operator", "state"], FRESH_STATE);
    client.setQueryData(sessionKey, sessionList);
    const renderContext = () => renderToStaticMarkup(<QueryClientProvider client={client}><ReadContext /></QueryClientProvider>);
    expect(renderContext()).toContain("unknown/active/gen: Conntrack entry present (ESTABLISHED); traffic may already be blocked");
    client.setQueryData(sessionKey, sessionList, { updatedAt: Date.now() - 16_000 });
    expect(renderContext()).toContain("unknown/active/gen");
    expect(renderContext()).not.toContain("Conntrack entry present");
    client.setQueryData(sessionKey, sessionList);
    client.getQueryCache().find({ queryKey: sessionKey })!.setState({ fetchFailureCount: 1, fetchStatus: "fetching" });
    expect(renderContext()).toContain("unknown/active/gen");
    client.getQueryCache().find({ queryKey: sessionKey })!.setState({ status: "error", error: new Error("upstream unavailable") });
    expect(renderContext()).toContain("unknown/active/gen");
    client.clear();
  });
});
