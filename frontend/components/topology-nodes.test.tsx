import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { NodeProps } from "reactflow";
import { describe, expect, it } from "vitest";
import { ZoneBoundaryNode } from "./topology-nodes";

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
