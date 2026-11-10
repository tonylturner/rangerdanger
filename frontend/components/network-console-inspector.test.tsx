// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { MouseEvent } from "react";
import type { Node } from "reactflow";
import { describe, expect, it, vi } from "vitest";
import { NetworkConsole } from "./network-console";
import { SharedTerminalPanel, TerminalProvider } from "./terminal-context";

const { nodes } = vi.hoisted(() => ({
  nodes: [
    { id: "rtu", position: { x: 0, y: 0 }, data: { label: "RTU", zone: "station_net" } },
    { id: "eng-ws", position: { x: 0, y: 0 }, data: { label: "Engineering workstation", ui_path: "/eng-ws/" } },
    { id: "firewall", position: { x: 0, y: 0 }, data: { label: "Firewall", external_ui_url: "/containd/" } },
  ],
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({}) }));
vi.mock("../lib/live-queries", () => ({ useWorkshopStatus: () => ({}) }));
vi.mock("../lib/operator-queries", () => ({ useOperatorView: () => ({ kind: "loading" }) }));
vi.mock("../lib/network-flow-queries", () => ({ useNetworkFlowContext: () => undefined }));
vi.mock("./use-styled-graph", () => ({ useStyledGraph: () => ({ nodes, edges: [] }) }));
vi.mock("./node-terminal", () => ({
  NodeTerminal: ({ nodeId }: { nodeId: string }) => <div data-testid="terminal">{nodeId}</div>,
}));
vi.mock("./topology-nodes", () => ({ nodeTypes: {} }));
vi.mock("./network-edges", () => ({ edgeTypes: {} }));
vi.mock("reactflow", () => ({
  default: ({ onNodeClick }: { onNodeClick: (event: MouseEvent, node: Node) => void }) =>
    <div>{nodes.map((node) => <button key={node.id} onClick={(event) => onNodeClick(event, node)}>Inspect {node.id}</button>)}</div>,
  Background: () => null, BackgroundVariant: { Dots: "dots", Lines: "lines" },
  ControlButton: () => null, Controls: () => null, EdgeLabelRenderer: () => null, MiniMap: () => null,
}));
vi.mock("./network-drawers", () => ({
  ActiveFlowsPanel: () => <div style={{ height: 1200 }}>Tall communications details</div>,
  InfoRow: () => null, LeftDrawerMenu: () => null, PolicyBadge: () => null,
  StatusDot: () => null, ViewModeToolbar: () => null,
}));

function inspect(id: string) {
  fireEvent.click(screen.getByRole("button", { name: `Inspect ${id}` }));
}

function expectSelection(uiSelected: boolean, terminalSelected: boolean) {
  const inspector = within(screen.getByRole("complementary", { name: "Node inspector" }));
  const ui = inspector.getByRole("button", { name: /^(UI|No UI)$/ });
  const terminal = inspector.getByRole("button", { name: "Terminal" });
  for (const [button, selected] of [[ui, uiSelected], [terminal, terminalSelected]] as const) {
    expect(button.getAttribute("aria-pressed")).toBe(String(selected));
    expect(button.classList.contains("bg-brand")).toBe(selected);
  }
}

describe("Network Map node inspector", () => {
  it.each(["eng-ws", "firewall"])("selects only the displayed viewer for %s", (id) => {
    render(<TerminalProvider><NetworkConsole /></TerminalProvider>);
    inspect(id);
    expectSelection(false, false);
    expect(screen.getByText("Select UI or Terminal to view here.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "UI" }));
    expectSelection(true, false);
    expect(screen.getByTitle("Embedded UI").getAttribute("src")).toBe(id === "eng-ws" ? "/eng-ws/" : "/containd/");
    fireEvent.click(screen.getByRole("button", { name: "Terminal" }));
    expectSelection(false, true);
    expect(screen.queryByTitle("Embedded UI")).toBeNull();
    expect(screen.getByTestId("terminal").textContent).toBe(id);
    expect(screen.getByTestId("terminal").parentElement?.parentElement?.style.height).toBe("100%");
    fireEvent.click(screen.getByRole("button", { name: "UI" }));
    expectSelection(true, false);
  });

  it("keeps no-UI nodes unselected until Terminal is opened, and restores the terminal on node return", () => {
    render(<TerminalProvider><NetworkConsole /></TerminalProvider>);
    inspect("rtu");
    expectSelection(false, false);
    expect(screen.getByRole("button", { name: "No UI" }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Terminal" }));
    expectSelection(false, true);
    inspect("eng-ws");
    expectSelection(false, false);
    inspect("rtu");
    expectSelection(false, true);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByLabelText("Node inspector")).toBeNull();
    inspect("rtu");
    expectSelection(false, true);
  });

  it.each(["rtu", "eng-ws", "firewall"])("bounds %s details separately from the flexible viewer", (id) => {
    render(<TerminalProvider><NetworkConsole /></TerminalProvider>);
    inspect(id);
    const inspector = screen.getByRole("complementary", { name: "Node inspector" });
    const details = screen.getByLabelText("Node details");
    const viewer = screen.getByLabelText("Node viewer");
    for (const token of ["fixed", "h-dvh", "flex", "flex-col", "overflow-hidden"]) {
      expect(inspector.classList.contains(token)).toBe(true);
    }
    for (const token of ["min-h-0", "flex-1", "overflow-y-auto"]) {
      expect(details.classList.contains(token)).toBe(true);
    }
    expect(details.tabIndex).toBe(0);
    expect(within(details).getByText("Tall communications details")).toBeTruthy();
    for (const token of ["min-h-[240px]", "flex-[2]", "overflow-hidden"]) {
      expect(viewer.classList.contains(token)).toBe(true);
    }
    const buttons = screen.getByRole("button", { name: "Terminal" }).parentElement!;
    expect(buttons.classList.contains("shrink-0")).toBe(true);
    expect(inspector.firstElementChild?.classList.contains("shrink-0")).toBe(true);
    expect(Array.from(inspector.children)).toEqual([inspector.firstElementChild, buttons, details, viewer]);
  });

  it("keeps both expand-to-modal actions working", () => {
    render(<TerminalProvider><NetworkConsole /></TerminalProvider>);
    inspect("eng-ws");
    fireEvent.click(screen.getByRole("button", { name: "UI" }));
    fireEvent.click(screen.getByTitle("Expand to full size"));
    expect(screen.getByTitle("Expanded UI").getAttribute("src")).toBe("/eng-ws/");
    // The UI modal close button is the last button in the console.
    fireEvent.click(screen.getAllByRole("button").at(-1)!);
    expect(screen.queryByTitle("Expanded UI")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Terminal" }));
    fireEvent.click(screen.getByTitle("Expand terminal"));
    expect(screen.getAllByTestId("terminal")).toHaveLength(2);
    fireEvent.click(screen.getAllByRole("button").at(-1)!);
    expect(screen.getAllByTestId("terminal")).toHaveLength(1);
  });

  it("preserves fixed-height shared panels for other callers while allowing parent-sized panels", () => {
    const panel = (height?: number | string) => <TerminalProvider>
      <SharedTerminalPanel nodes={["rtu"]} activeNode="rtu" height={height} />
    </TerminalProvider>;
    const tree = render(panel());
    const height = () => screen.getByTestId("terminal").parentElement?.parentElement?.style.height;
    expect(height()).toBe("300px");
    tree.rerender(panel(450));
    expect(height()).toBe("450px");
    tree.rerender(panel("100%"));
    expect(height()).toBe("100%");
  });
});
