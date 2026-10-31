// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import type { LabGraph, ZoneRuleSummary } from "../lib/api";
import { aggregateFlows, createFlowContext } from "../lib/observed-flows";
import { buildStyledGraph } from "../lib/network-graph";
import { FEEDER_STATE } from "../lib/operator-fixtures.test-util";
import { FlowEvidence } from "./network-flow-evidence";
import { TrafficMatrixView } from "./traffic-matrix-view";
import { ActiveFlowsPanel } from "./network-drawers";
import { TooltipProvider } from "./ui/tooltip";

// This test exercises flow selection without Radix's tooltip DOM events.
vi.mock("./ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: ReactNode }) => children,
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const graph = euFixture as LabGraph;
const context = createFlowContext(graph, { operator: FEEDER_STATE });
const rules: ZoneRuleSummary[] = [{ source_zone: "lan4", dest_zone: "lan1", action: "MIXED", summary: "2404", rule_details: ["CC /32 only"] }];
function evidence(id: string, summaries = rules) {
  const flow = graph.flows.find((f) => f.id === id)!;
  const pair = aggregateFlows(context).find((p) => p.source === flow.source && p.target === flow.target)!;
  return renderToStaticMarkup(<FlowEvidence context={context} pair={pair} ruleSummaries={summaries} />);
}

describe("EU flow meaning", () => {
  it.each([
    ["cc-rtu", "Monitoring and control", "observations return on the same session"],
    ["rtu-breaker", "Monitoring and control", "polls field indications"],
    ["plc-oltc", "Regulation", "reads feedback and writes tap requests"],
    ["cc-historian", "Archive ingestion", "pushes received-observation snapshots"],
    ["enterprise-historian", "Archive reading", "separate snapshot-ingest listener"],
    ["engineering-plc", "Engineering", "separate management access"],
    ["attacker-rtu", "Exercise traffic", "does not authorize monitoring or control"],
    ["station-clock", "Time quality", "simulated over HTTP, not NTP/PTP"],
    ["rtu-plc-1", "Monitoring", "stays inside the station zone"],
  ])("explains %s purpose and boundaries", (id, purpose, detail) => {
    const html = evidence(id);
    for (const copy of [purpose, detail, "Topology", "Observed liveness", "Authorization",
      "Arrows show who initiates", "not information direction", "IEC 104 and Modbus exchanges are bidirectional",
      "Port rules do not distinguish reads from writes", "deep packet inspection (DPI)"]) expect(html).toContain(copy);
  });

  it("keeps active observations and declared topology distinct from unknown authorization", () => {
    const html = evidence("cc-rtu");
    expect(html).toContain("Observed liveness: active");
    expect(html).toContain("Topology</dt><dd>Declared by package");
    expect(html).toContain("Authorization unknown — MIXED zone-pair summary");
    expect(evidence("cc-rtu", [])).toContain("Authorization unknown — no matching");
    expect(evidence("cc-rtu", [{ ...rules[0], action: "DENY" }])).toContain("Denied by zone-pair summary");
  });

  it.each(["station-clock", "rtu-plc-1"])("keeps %s outside the firewall policy scope", (id) => {
    expect(evidence(id)).toContain("same zone · does not traverse containd");
    expect(evidence(id)).toContain("firewall policy does not establish authorization");
  });

  it("puts purpose, authorization and observation evidence on the canvas too", () => {
    for (const action of ["MIXED", "DENY"] as const) {
      const edges = buildStyledGraph(graph, [{ ...rules[0], action }], { policyDim: true, traffic: true, iec62443: false }, undefined, undefined, context).edges;
      const data = edges.find((e) => e.id === "traffic-cc-1->rtu-1")!.data;
      expect(data.blockedByFirewall).toBe(action === "DENY");
      const text = data.tooltipLines.join(" ");
      expect(text).toContain("Monitoring and control");
      expect(text).toContain("Topology: declared");
      expect(text).toContain("Observed liveness: active");
      expect(text).toContain(action === "DENY" ? "Denied by zone-pair summary" : "Authorization unknown");
    }
  });

  it("uses the EU declared same-zone inventory in the node inspector", () => {
    const html = renderToStaticMarkup(<ActiveFlowsPanel graph={graph} nodeId="rtu-1" nodeZone="station_net" multiHomedZones={[]} />);
    expect(html).toContain("Same zone · firewall-blind");
    expect(html).toContain("gps-1");
    expect(html).toContain("plc-1");
    expect(html).toContain("These flows never reach the firewall");
  });

  it("opens flow evidence with a native keyboard-accessible button, independently of transient map highlighting", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(["pcap-status"], { capturing: false, files: [] });
    const tree = render(<QueryClientProvider client={client}><TooltipProvider><TrafficMatrixView context={context} ruleSummaries={rules} /></TooltipProvider></QueryClientProvider>);
    const row = () => screen.getByRole("button", { name: "Inspect flow cc-1 to rtu-1" });
    try {
      expect(row().getAttribute("aria-expanded")).toBe("false");
      expect(row().tagName.toLowerCase()).toBe("button");
      expect(row().className).toContain("focus-visible:outline-solid");
      fireEvent.click(row());
      expect(row().getAttribute("aria-expanded")).toBe("true");
      expect(screen.getAllByLabelText("Flow evidence cc-1 to rtu-1")).toHaveLength(1);
      fireEvent.mouseLeave(row().parentElement!);
      expect(row().getAttribute("aria-expanded")).toBe("true");
      fireEvent.click(row());
      expect(row().getAttribute("aria-expanded")).toBe("false");
      fireEvent.click(row());
      expect(row().getAttribute("aria-expanded")).toBe("true");
    } finally {
      tree.unmount();
      client.clear();
    }
  });
});
