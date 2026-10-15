import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { getBezierPath, Position, type EdgeProps, type Node } from "reactflow";
import { describe, expect, it, vi } from "vitest";
import { edgeTypes } from "./network-edges";
import { buildStyledGraph } from "../lib/network-graph";
import { createFlowContext } from "../lib/observed-flows";
import type { LabGraph } from "../lib/api";
import euFixture from "../../backend/internal/labs/testdata/eu-flow-graph.json";
import usFixture from "../../backend/internal/labs/testdata/us-flow-graph.json";

// React Flow's label portal requires a mounted canvas; keep its children
// in the SSR output so geometry is testable without a browser.
vi.mock("reactflow", async (importOriginal) => ({
  ...await importOriginal<typeof import("reactflow")>(),
  EdgeLabelRenderer: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

const base: EdgeProps = {
  id: "edge", source: "source", target: "target", selected: false,
  sourceX: 28, sourceY: 548, targetX: -28, targetY: 418,
  sourcePosition: Position.Right, targetPosition: Position.Left,
};

describe("PolicyEdge", () => {
  it.each([
    ["UNKNOWN", "unknown", "?"],
    ["ALLOW", "unknown", "?"],
    ["ALLOW", "allowed", "✓"],
    ["DENY", "blocked", "✕"],
    ["MIXED", "allowed", "⚠"],
  ])("uses a distinct icon for %s/%s", (action, permissiveness, icon) => {
    const out = renderToStaticMarkup(<svg><edgeTypes.policyEdge {...base} data={{ label: "Inbound policy", action, permissiveness }} /></svg>);
    expect(out.match(/<tspan[^>]*>(.*?)<\/tspan>/)?.[1]).toBe(`${icon}  `);
    if (permissiveness === "unknown") expect(out).not.toContain("✓");
  });

  it("does not default missing policy data to a green check", () => {
    const out = renderToStaticMarkup(<svg><edgeTypes.policyEdge {...base} data={{ label: "No policy data" }} /></svg>);
    expect(out).toContain("?");
    expect(out).not.toContain("✓");
    expect(out).not.toContain("#22c55e");
  });
});

function labelPosition(props: EdgeProps): [number, number] {
  const out = renderToStaticMarkup(<edgeTypes.trafficEdge {...props} />);
  const match = out.match(/translate\((-?[\d.]+)px, (-?[\d.]+)px\)/)!;
  return [Number(match[1]), Number(match[2])];
}

describe("TrafficEdge labels", () => {
  it.each([false, true])("applies the planned lane relative to live handles, sameZone=%s", (sameZone) => {
    const props = { ...base, data: { label: "HTTP", sameZone, labelOffset: { x: -28, y: 61 } } };
    const [, x, y] = getBezierPath({ ...props, curvature: 0.4 });
    expect(labelPosition(props)).toEqual([x - 28, y + 61]);
    const moved = { ...props, sourceX: props.sourceX + 100, targetX: props.targetX + 100 };
    expect(labelPosition(moved)).toEqual([x + 72, y + 61]);
  });

  it("uses the bezier midpoint for a zero planned offset", () => {
    const props = { ...base, targetX: 268, data: { label: "IEC104", sameZone: false, labelOffset: { x: 0, y: 0 } } };
    const [, x, y] = getBezierPath({ ...props, curvature: 0.4 });
    expect(labelPosition(props)).toEqual([x, y]);
  });
});

type Box = { id: string; x: number; y: number; width: number; height: number };
function overlap(a: Box, b: Box): boolean {
  return a.x < b.x + b.width && a.x + a.width > b.x &&
    a.y < b.y + b.height && a.y + a.height > b.y;
}
function edgeProps(source: Node, target: Node, policy: boolean): EdgeProps {
  const band = policy ? source.type === "zone" : source.data.verticalOnly;
  return {
    ...base,
    sourceX: source.position.x + (policy ? (band ? 1 : 80) : (band ? 60 : 88)),
    sourceY: source.position.y + (policy ? (band ? 0.5 : 88) : (band ? 56 : 28)),
    targetX: target.position.x + (policy ? (band ? 36 : 0.5) : (band ? 60 : 32)),
    targetY: target.position.y + (policy ? (band ? 44 : 0) : (band ? 0 : 28)),
    sourcePosition: band ? (policy ? Position.Right : Position.Bottom) : (policy ? Position.Bottom : Position.Right),
    targetPosition: band ? (policy ? Position.Left : Position.Top) : (policy ? Position.Top : Position.Left),
  };
}

describe.each([["EU", euFixture], ["US", usFixture]] as const)("%s shipped label geometry", (_, fixture) => {
  it.each([false, true])("has no pill/host or pill/pill intersections, bands=%s", (iec62443) => {
    // Also reverse every direction: lane ownership must follow spatial order,
    // not which endpoint happens to be the flow source.
    for (const direction of ["original", "reversed", "bidirectional"]) {
      const graph = structuredClone(fixture) as LabGraph;
      if (direction === "reversed") graph.flows = graph.flows.map((f) => ({ ...f, source: f.target, target: f.source }));
      if (direction === "bidirectional") {
        // Exercise multiple labels in one gap, including wider protocols.
        const zone = (id: string) => graph.nodes.find((n) => n.id === id)!.data.zone;
        graph.flows.push(...graph.flows.filter((f) => zone(f.source) === zone(f.target))
          .map((f) => ({ ...f, id: `${f.id}-reply`, source: f.target, target: f.source, protocol: "Diagnostic HTTP" })));
      }
      for (const active of [false, true]) {
        const context = createFlowContext(graph, {
          workshop: { workshop_id: "test", workshop_name: "test", firewall_online: true,
            firewall_config: "weak", scenario_count: 7, rtac_online: active,
            device_comms: { relay: active, recloser: active, regulator: active, capbank: active } },
        });
        const result = buildStyledGraph(graph, [], { traffic: true, policyDim: true, iec62443 },
          undefined, undefined, context);
        const hosts = result.nodes.filter((n) => n.type === "host");
        const obstacles: Box[] = hosts.flatMap((n) => [
          // HostNode: 56px icon, 8px margin, up to three 15px name
          // lines, 2.5px margin and 15px IP line. Use a conservative
          // full text block, not guessed glyph bounds that miss wrapping.
          { id: `${n.id} text`, x: n.position.x, y: n.position.y + 64, width: 120, height: 66 },
          { id: `${n.id} icon`, x: n.position.x + 32, y: n.position.y, width: 56, height: 56 },
        ]);
        const pills: Box[] = [];
        for (const edge of result.edges) {
          const source = result.nodes.find((n) => n.id === edge.source)!;
          const target = result.nodes.find((n) => n.id === edge.target)!;
          const props = { ...edgeProps(source, target, edge.type === "policyEdge"), id: edge.id, data: edge.data };
          let box: Box;
          if (edge.type === "trafficEdge") {
            const out = renderToStaticMarkup(<edgeTypes.trafficEdge {...props} />);
            const [x, y] = labelPosition(props);
            const size = out.match(/width:([\d.]+)px;height:([\d.]+)px/)!;
            const width = Number(size[1]), height = Number(size[2]);
            box = { id: edge.id, x: x - width / 2, y: y - height / 2, width, height };
            if (edge.data.sameZone) {
              const boundary = result.nodes.find((n) => n.id === `zone-boundary-${source.data.zone}`)!;
              expect(box.x, edge.id).toBeGreaterThanOrEqual(boundary.position.x);
              expect(box.y, edge.id).toBeGreaterThanOrEqual(boundary.position.y);
              expect(box.x + width, edge.id).toBeLessThanOrEqual(boundary.position.x + boundary.data.width);
              expect(box.y + height, edge.id).toBeLessThanOrEqual(boundary.position.y + boundary.data.height);
              if (!iec62443) {
                const upper = source.position.y < target.position.y ? source : target;
                const next = hosts.filter((n) => n.data.zone === upper.data.zone && n.position.y > upper.position.y)
                  .sort((a, b) => a.position.y - b.position.y)[0];
                expect(box.y, edge.id).toBeGreaterThanOrEqual(upper.position.y + 130);
                expect(box.y + height, edge.id).toBeLessThanOrEqual(next.position.y);
              }
            }
          } else {
            const out = renderToStaticMarkup(<svg><edgeTypes.policyEdge {...props} /></svg>);
            const rect = out.match(/<rect x="([^"]+)" y="([^"]+)" width="([^"]+)" height="([^"]+)"/)!;
            box = { id: edge.id, x: Number(rect[1]), y: Number(rect[2]), width: Number(rect[3]), height: Number(rect[4]) };
          }
          for (const other of [...obstacles, ...pills]) {
            expect(overlap(box, other), `${box.id} intersects ${other.id}, direction=${direction}, active=${active}`).toBe(false);
          }
          pills.push(box);
        }
      }
    }
  });
});
