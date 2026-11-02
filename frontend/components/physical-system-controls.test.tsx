// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import * as api from "../lib/api";
import { TRUTH } from "../lib/operator-fixtures.test-util";
import { PhysicalEquipmentControls, PhysicalSystemControls } from "./physical-system-controls";
import { PhysicalSystemScreen } from "./physical-system";

beforeEach(() => {
  vi.spyOn(api, "sendFieldAction").mockResolvedValue({});
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => vi.restoreAllMocks());

it.each([
  ["Inject fault S1", "fault_s1", "S1"],
  ["Inject fault S2", "fault_s2", "S2"],
] as const)("confirms %s as a simulation fault, not an operator command", async (label, action, section) => {
  render(<PhysicalSystemControls unavailable={false} />);
  fireEvent.click(screen.getByRole("button", { name: label }));
  expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining(`simulated fault on ${section}`));
  expect(api.sendFieldAction).toHaveBeenCalledExactlyOnceWith({ device: "process", action });
  await screen.findByText(`${label}: simulation action completed. Live state refreshes automatically.`);
  expect(screen.getByText(/out-of-band, not IEC 104 operator commands/)).toBeTruthy();
});

it("cancels a destructive action without sending a request", () => {
  vi.mocked(window.confirm).mockReturnValue(false);
  render(<PhysicalSystemControls unavailable={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Inject fault S1" }));
  expect(api.sendFieldAction).not.toHaveBeenCalled();
  expect(screen.queryByRole("status")).toBeNull();
});

it("clears only the process fault, shows pending and prevents duplicate submission", async () => {
  let complete!: (state: Record<string, unknown>) => void;
  vi.mocked(api.sendFieldAction).mockImplementation(() => new Promise((resolve) => { complete = resolve; }));
  render(<PhysicalSystemControls unavailable={false} />);
  const clear = screen.getByRole("button", { name: "Clear fault" });
  fireEvent.click(clear);
  fireEvent.click(clear);
  expect(api.sendFieldAction).toHaveBeenCalledExactlyOnceWith({ device: "process", action: "clear_fault" });
  expect(window.confirm).not.toHaveBeenCalled();
  expect(screen.getByRole("status").textContent).toContain("pending");
  for (const b of screen.getAllByRole("button")) expect((b as HTMLButtonElement).disabled).toBe(true);
  await act(async () => complete({ fault_section: "none" }));
  expect(screen.getByRole("status").textContent).toContain("completed");
  expect((clear as HTMLButtonElement).disabled).toBe(false);
  expect(screen.getByText(/Clearing a fault does not reset lockout or FPI latches/)).toBeTruthy();
});

it("shows an inline failure honestly, never retries, and allows a new user action", async () => {
  vi.mocked(api.sendFieldAction).mockRejectedValueOnce(new api.ApiError(502, "solver unavailable"));
  render(<PhysicalSystemControls unavailable={false} />);
  fireEvent.click(screen.getByRole("button", { name: "Clear fault" }));
  expect((await screen.findByRole("alert")).textContent).toContain("502 solver unavailable");
  expect(api.sendFieldAction).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(/simulation action completed/)).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Clear fault" }));
  await screen.findByText(/simulation action completed/);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("disables process actions for retained simulation state", () => {
  render(<PhysicalSystemControls unavailable />);
  for (const b of screen.getAllByRole("button")) {
    expect((b as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(b);
  }
  expect(api.sendFieldAction).not.toHaveBeenCalled();
});

const equipment = [
  ["CB1", "breaker-1", "feeder-breaker", "Reset lockout", "reset_lockout"],
  ["F2", "f2-breaker", "f2-breaker", "Reset lockout", "reset_lockout"],
  ["RMU1", "rmu-1", "rmu-1", "Reset FPI", "reset_fault_passage"],
  ["RMU2", "rmu-2", "rmu-2", "Reset FPI", "reset_fault_passage"],
] as const;

it.each(equipment)("resets only the %s latch through the field API", async (id, key, device, label, action) => {
  const state = structuredClone(TRUTH.devices[key]);
  if (state.kind === "breaker") state.lockout = true;
  if (state.kind === "rmu") state.fault_passage = true;
  const tree = render(<PhysicalEquipmentControls id={id} device={state} unavailable={false} />);
  fireEvent.click(screen.getByRole("button", { name: label }));
  expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining(id));
  expect(api.sendFieldAction).toHaveBeenCalledExactlyOnceWith({ device, action });
  await screen.findByText(/simulation action completed/);
  // An action response is not substituted for the polled physical state.
  if (state.kind === "breaker") expect(state.lockout).toBe(true);
  if (state.kind === "rmu") expect(state.fault_passage).toBe(true);
  if (id === "CB1") expect(tree.container.textContent).toContain("leaves the breaker OPEN. Close it from the Control Centre.");
  if (id === "F2") expect(tree.container.textContent).toContain("F2 has no close control in the Control Centre.");
});

it.each(equipment)("disables %s recovery unless latched and current", (id, key, _device, label) => {
  const state = structuredClone(TRUTH.devices[key]);
  if (state.kind === "breaker") state.lockout = false;
  if (state.kind === "rmu") state.fault_passage = false;
  const tree = render(<PhysicalEquipmentControls id={id} device={state} unavailable={false} />);
  const button = screen.getByRole("button", { name: label }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  if (state.kind === "breaker") state.lockout = true;
  if (state.kind === "rmu") state.fault_passage = true;
  tree.rerender(<PhysicalEquipmentControls id={id} device={state} unavailable />);
  expect(button.disabled).toBe(true);
  fireEvent.click(button);
  expect(api.sendFieldAction).not.toHaveBeenCalled();
});

it("keeps equipment cancellation and error outcomes inline", async () => {
  const state = structuredClone(TRUTH.devices["rmu-1"]);
  if (state.kind === "rmu") state.fault_passage = true;
  vi.mocked(window.confirm).mockReturnValueOnce(false);
  render(<PhysicalEquipmentControls id="RMU1" device={state} unavailable={false} />);
  const button = screen.getByRole("button", { name: "Reset FPI" });
  fireEvent.click(button);
  expect(api.sendFieldAction).not.toHaveBeenCalled();
  vi.mocked(api.sendFieldAction).mockRejectedValueOnce(new Error("device unavailable"));
  fireEvent.click(button);
  expect((await screen.findByRole("alert")).textContent).toContain("Reset FPI: device unavailable");
});

it("mounts process controls and card-local recovery even when cards are collapsed", async () => {
  const field = structuredClone(TRUTH);
  for (const state of Object.values(field.devices)) {
    if (state.kind === "breaker") state.lockout = true;
    if (state.kind === "rmu") state.fault_passage = true;
  }
  render(<PhysicalSystemScreen field={field} stateError={null} operator={undefined} now={0} />);
  expect(screen.getByRole("region", { name: "Process simulation controls" })).toBeTruthy();
  for (const [id, , , label] of equipment) {
    const card = document.getElementById(`physical-equipment-${id}`)!;
    const button = within(card).getByRole("button", { name: label });
    expect((button as HTMLButtonElement).disabled).toBe(false);
    expect(within(card).getByRole("button", { name: /Expand/ }).getAttribute("aria-expanded")).toBe("false");
  }
  expect(within(document.getElementById("physical-equipment-T1")!).queryByText("Reset lockout")).toBeNull();
  fireEvent.click(within(document.getElementById("physical-equipment-F2")!).getByRole("button", { name: "Reset lockout" }));
  await waitFor(() => expect(api.sendFieldAction).toHaveBeenCalledExactlyOnceWith({ device: "f2-breaker", action: "reset_lockout" }));
  await screen.findByText(/simulation action completed/);
});

it("does not expose actions before field truth arrives", () => {
  render(<PhysicalSystemScreen field={undefined} stateError={null} operator={undefined} now={0} />);
  expect(screen.queryByRole("button", { name: "Clear fault" })).toBeNull();
});
