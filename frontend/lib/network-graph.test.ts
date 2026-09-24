import { describe, expect, it } from "vitest";
import type { LabGraph } from "./api";
import { buildStyledGraph } from "./network-graph";

describe("buildStyledGraph", () => {
  it("builds the current default layout for one firewall zone", () => {
    const graph: LabGraph = {
      nodes: [
        {
          id: "fw-1",
          type: "containd_ngfw",
          position: { x: 0, y: 0 },
          data: { label: "Firewall", zone: "enterprise_net", networks: [] },
        },
        {
          id: "relay-1",
          type: "relay_sim",
          position: { x: 0, y: 0 },
          data: { label: "Relay", zone: "field_net", networks: ["field_net"] },
        },
      ],
      edges: [],
    };

    const result = buildStyledGraph(
      graph,
      undefined,
      { policyDim: true, traffic: false, iec62443: false },
    );

    expect(result.nodes).toHaveLength(4);
    expect(result.edges).toHaveLength(1);
    expect(result.edges[0].id).toBe("fw-to-field_net");
  });
});
