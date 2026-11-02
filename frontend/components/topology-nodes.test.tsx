import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { NodeProps } from "reactflow";
import { describe, expect, it, vi } from "vitest";
import { FirewallNode, ZoneBoundaryNode } from "./topology-nodes";
import type { LabGraph, PolicySource, ZoneRuleSummary } from "../lib/api";
import { buildStyledGraph } from "../lib/network-graph";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";

describe("ZoneBoundaryNode header", () => {
  it.each(["Enterprise Zone", "Vendor / Engineering Zone", "UnbrokenVeryLongZoneNameForWrapping"])("stacks %s above the subnet with wrapping", (label) => {
    const props: NodeProps<React.ComponentProps<typeof ZoneBoundaryNode>["data"]> = {
      id: "boundary", type: "zoneBoundary", selected: false, dragging: false,
      isConnectable: false, xPos: 0, yPos: 0, zIndex: 0,
      data: { zone: "enterprise_net", label, subnet: "10.10.10.0/24", width: 200, height: 276 },
    };
    const out = renderToStaticMarkup(<ZoneBoundaryNode {...props} />);
    expect(out).toContain("flex-direction:column");
    expect(out).toContain("gap:4px");
    expect(out).not.toContain("justify-content:space-between");
    expect(out.match(/max-width:100%;overflow-wrap:anywhere/g)).toHaveLength(2);
    expect(out.indexOf(label)).toBeLessThan(out.indexOf("10.10.10.0/24"));
  });
});

// Render the badge and tooltip text together without Radix's hover portal.
vi.mock("./ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <aside>{children}</aside>,
}));
vi.mock("reactflow", async (importOriginal) => ({
  ...await importOriginal<typeof import("reactflow")>(), Handle: () => null,
}));

const referenceSummary: ZoneRuleSummary[] = [{
  source_zone: "wan", dest_zone: "lan2", action: "DENY", summary: "DENY",
  rule_details: ["Block direct enterprise field access"],
}, {
  source_zone: "dmz", dest_zone: "lan2", action: "DENY", summary: "DENY",
  rule_details: ["Block direct vendor field access"],
}, {
  source_zone: "lan1", dest_zone: "lan2", action: "MIXED", summary: "502, 20000",
  rule_details: ["Allow approved supervisory sources", "Deny other field access"],
}];
const weakSummary: ZoneRuleSummary[] = [{ ...referenceSummary[0], action: "ALLOW", rule_details: ["WEAK: broad field access"] }];

describe.each([
  ["US", usFixture, true], ["EU", euFixture, false],
] as const)("%s scoped segmentation badge", (_name, fixture, numericTarget) => {
  it.each<[{ label: string; source: PolicySource; config: string; summaries: ZoneRuleSummary[] | undefined; status: string; online?: boolean }]>([
    [{ label: "weak", source: "weak", config: "weak", summaries: weakSummary, status: "GAP" }],
    [{ label: "hardened reference", source: "hardened-reference", config: "improved", summaries: referenceSummary, status: "MET" }],
    [{ label: "partial plan", source: "plan", config: "custom", summaries: referenceSummary, status: "NOT ASSESSED" }],
    [{ label: "incomplete reference summary", source: "hardened-reference", config: "improved", summaries: referenceSummary.slice(0, 1), status: "NOT ASSESSED" }],
    [{ label: "empty", source: "hardened-reference", config: "improved", summaries: [], status: "NOT ASSESSED" }],
    [{ label: "missing", source: "hardened-reference", config: "improved", summaries: undefined, status: "NOT ASSESSED" }],
    [{ label: "custom", source: "manual-custom", config: "custom", summaries: referenceSummary, status: "NOT ASSESSED" }],
    [{ label: "unknown", source: "", config: "improved", summaries: referenceSummary, status: "NOT ASSESSED" }],
    [{ label: "contradictory", source: "hardened-reference", config: "improved", summaries: weakSummary, status: "NOT ASSESSED" }],
    [{ label: "offline", source: "hardened-reference", config: "improved", summaries: referenceSummary, status: "NOT ASSESSED", online: false }],
    [{ label: "empty details", source: "hardened-reference", config: "improved", summaries: [{ ...referenceSummary[0], rule_details: [] }], status: "NOT ASSESSED" }],
  ])("renders $label without achieved SL or DPI filtering claims", ({ source, config, summaries, status, online = true }) => {
    const graph = buildStyledGraph(fixture as LabGraph, summaries,
      { traffic: false, policyDim: true, iec62443: true }, undefined, undefined, undefined,
      online, undefined, undefined, { active_config: config, policy_source: source });
    const firewall = graph.nodes.find((n) => n.type === "firewall")!;
    const props: NodeProps<React.ComponentProps<typeof FirewallNode>["data"]> = {
      id: firewall.id, type: "firewall", selected: false, dragging: false,
      isConnectable: false, xPos: 0, yPos: 0, zIndex: 0, data: firewall.data,
    };
    const out = renderToStaticMarkup(<FirewallNode {...props} />).replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
    expect(out).toContain(`Segmentation criteria: ${status}`);
    expect(out.includes("Design target SL-3")).toBe(numericTarget);
    expect(out).toContain("not an IEC 62443 SL assessment");
    expect(out).toContain("The L4 source pin enforces; DPI detects and logs.");
    expect(out).not.toMatch(/achieves? SL|reach SL|SL-3 Target Met|function.code filtering|protocol-aware DPI/);
    if (status !== "MET") expect(out).not.toContain("criteria: MET");
  });
});
