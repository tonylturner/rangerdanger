import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { getBezierPath, Position, type EdgeProps } from "reactflow";
import { describe, expect, it, vi } from "vitest";
import { edgeTypes } from "./network-edges";

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
  it.each([false, true])("clears the full host/IP width in a shared column, reversed=%s", (reversed) => {
    const props = {
      ...base,
      sourceY: reversed ? base.targetY : base.sourceY,
      targetY: reversed ? base.sourceY : base.targetY,
      data: { label: "HTTP", sameZone: true },
    };
    const [x] = labelPosition(props);
    // Host centered at x=0: wrapper [-60,60], pill half-width 28.
    expect(x - 28).toBeGreaterThan(60);
  });

  it("accounts for long protocol labels in shared columns", () => {
    const label = "Modbus + DNP3 + HTTP";
    const [x] = labelPosition({ ...base, data: { label, sameZone: true } });
    expect(x - (label.length * 6 + 16) / 2).toBeGreaterThan(60);
  });

  it.each([false, true])("clears the icons in a shared band, reversed=%s", (reversed) => {
    const [, y] = labelPosition({
      ...base,
      sourceX: reversed ? 160 : 30, targetX: reversed ? 30 : 160,
      sourceY: 106, targetY: 50,
      sourcePosition: Position.Bottom, targetPosition: Position.Top,
      data: { label: "HTTP", sameZone: true },
    });
    expect(y + 11).toBeLessThan(50);
  });

  it("keeps cross-zone labels at the bezier midpoint", () => {
    const props = { ...base, targetX: 268, data: { label: "IEC104", sameZone: false } };
    const [, x, y] = getBezierPath({ ...props, curvature: 0.4 });
    expect(labelPosition(props)).toEqual([x, y]);
  });
});
