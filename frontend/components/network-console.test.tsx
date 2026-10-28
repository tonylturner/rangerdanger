import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import type { ViewMode } from "../lib/network-console-data";
import { describe, expect, it, vi } from "vitest";
import { NetworkConsole } from "./network-console";

const { workshop } = vi.hoisted(() => ({
  workshop: {
    firewall_online: true, firewall_config: "improved",
    control_centre: { link: "started", gi: "complete" },
  },
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({}) }));
vi.mock("../lib/live-queries", () => ({ useWorkshopStatus: () => ({ data: workshop }) }));
vi.mock("../lib/operator-queries", () => ({ useOperatorView: () => ({ kind: "known", view: "eu-iec104", pkg: { title: "EU Substation" } }) }));
vi.mock("../lib/network-flow-queries", () => ({ useNetworkFlowContext: () => undefined }));
vi.mock("./use-styled-graph", () => ({ useStyledGraph: () => ({ nodes: [], edges: [] }) }));
vi.mock("./node-terminal", () => ({ NodeTerminal: () => null }));
vi.mock("./terminal-context", () => ({
  SharedTerminalPanel: () => null, useTerminals: () => ({ openTerminals: new Set(), open: vi.fn() }),
}));
vi.mock("./topology-nodes", () => ({ nodeTypes: {} }));
vi.mock("./network-edges", () => ({ edgeTypes: {} }));
vi.mock("reactflow", () => ({
  default: () => null,
  Background: () => null, BackgroundVariant: { Dots: "dots", Lines: "lines" },
  ControlButton: () => null, Controls: () => null, EdgeLabelRenderer: () => null, MiniMap: () => null,
}));
vi.mock("./network-drawers", () => ({
  ActiveFlowsPanel: () => null, InfoRow: () => null,
  LeftDrawerMenu: ({ active, onChange }: { active: string | null; onChange: (value: string) => void }) =>
    <button aria-label="Open drawer" data-active={active} onClick={() => onChange("traffic")} />,
  PolicyBadge: () => null,
  ViewModeToolbar: ({ viewMode, onChange }: { viewMode: ViewMode; onChange: (mode: ViewMode) => void }) =>
    <button aria-label="Toggle policy" onClick={() => onChange({ ...viewMode, policyDim: !viewMode.policyDim })} />,
  // Expose tooltip content without a browser portal; the real console
  // still chooses which status and copy to pass to the shared dot.
  StatusDot: ({ label, tooltipBody }: { label: ReactNode; tooltipBody: string }) =>
    <span>{label}<span>{tooltipBody}</span></span>,
}));

describe("NetworkConsole IEC 104 explanation", () => {
  it("clears the active drawer in the policy-toggle event", () => {
    let tree!: ReactTestRenderer;
    act(() => { tree = create(<NetworkConsole />); });
    try {
      const toggle = () => act(() => tree.root.findByProps({ "aria-label": "Toggle policy" }).props.onClick());
      act(() => tree.root.findByProps({ "aria-label": "Open drawer" }).props.onClick());
      expect(tree.root.findByProps({ "aria-label": "Open drawer" }).props["data-active"]).toBe("traffic");
      toggle();
      expect(tree.root.findAllByProps({ "aria-label": "Open drawer" })).toHaveLength(0);
      toggle();
      expect(tree.root.findByProps({ "aria-label": "Open drawer" }).props["data-active"]).toBe(null);
    } finally {
      act(() => tree.unmount());
    }
  });

  it("explains event/cyclic reporting and operator GI without expiring quiet status", () => {
    const html = renderToStaticMarkup(<NetworkConsole />);
    expect(html).toContain("CC→RTU TCP/2404");
    expect(html).toContain("spontaneous status events, cyclic/deadband measurands and commands");
    expect(html).toContain("GI runs on connect, every 15 minutes or via Interrogate station");
    expect(html).toContain("Quiet GOOD status does not expire");
    expect(html).toContain("link loss marks retained values stale");
    expect(html).not.toContain("RTAC polls");
  });

  it("discloses omitted infrastructure and same-zone boundaries without claiming blanket enforcement", () => {
    const html = renderToStaticMarkup(<NetworkConsole />);
    expect(html).toContain("<summary");
    expect(html).toContain(">Infrastructure planes</summary>");
    expect(html).not.toMatch(/<details[^>]*open/);
    for (const copy of [
      "teaching-zone conduits through containd", "Management / orchestration", "Process truth",
      "Desktop delivery", "Desktop forwarding is disabled", "Portal OpenPLC access is management access, not an engineering conduit",
      "RTU→PLC and RTU→clock stay inside the station zone", "simulated over HTTP, not NTP/PTP",
      "Port rules do not distinguish reads from writes", "command-level filtering would need DPI",
    ]) expect(html).toContain(copy);
    expect(html).not.toContain("Mediates every cross-zone flow");
  });
});
