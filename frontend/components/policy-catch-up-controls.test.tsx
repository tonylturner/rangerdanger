// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as api from "../lib/api";
import { EU_PLANS } from "../lib/plan-fixtures.test-util";
import { PolicyCatchUpControls } from "./policy-catch-up-controls";

afterEach(() => vi.restoreAllMocks());

function mount(source: api.PolicySource = "manual-custom") {
  const onApplied = vi.fn();
  const tree = render(<PolicyCatchUpControls activeConfig="custom" policySource={source} plan={EU_PLANS[0]} onApplied={onApplied} />);
  fireEvent.click(tree.container.querySelector("summary")!);
  return { ...tree, onApplied };
}

describe("catch-up policy replacement", () => {
  it.each(["Apply Hardened", "Apply Your Plan", "Reset to Weak"])("cancels %s without an API request", (label) => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const canned = vi.spyOn(api, "applyFirewallConfig");
    const plan = vi.spyOn(api, "applyPlan");
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: new RegExp(`^${label}`) }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("hand-written firewall policy"));
    expect(canned).not.toHaveBeenCalled();
    expect(plan).not.toHaveBeenCalled();
  });

  it.each(["Apply Hardened", "Reset to Weak"])("disables all replacements while %s is pending and shows server errors", async (label) => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let reject: (error: Error) => void = () => { throw new Error("Request not started"); };
    vi.spyOn(api, "applyFirewallConfig").mockImplementation(() => new Promise((_, fail) => { reject = fail; }));
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: label }));
    for (const button of tree.getAllByRole("button")) expect(button.hasAttribute("disabled")).toBe(true);
    reject(new Error("409 Policy revision changed"));
    await waitFor(() => expect(tree.getByRole("alert").textContent).toBe("409 Policy revision changed"));
    expect(tree.onApplied).not.toHaveBeenCalled();
    expect(tree.getByRole("button", { name: label }).hasAttribute("disabled")).toBe(false);
  });

  it("applies an explicitly confirmed plan and retains its warnings", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(api, "applyPlan").mockResolvedValue({ applied: true, warnings: ["Review deferred controls"], plan: EU_PLANS[0] });
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: /^Apply Your Plan/ }));
    await waitFor(() => expect(tree.onApplied).toHaveBeenCalledOnce());
    expect(tree.getByText("Review deferred controls")).toBeTruthy();
  });

  it("does not ask to replace a hand-written policy when the source is named", async () => {
    const confirm = vi.spyOn(window, "confirm");
    vi.spyOn(api, "applyFirewallConfig").mockResolvedValue({ status: "applied", active_config: "weak" });
    const tree = mount("hardened-reference");
    fireEvent.click(tree.getByRole("button", { name: "Reset to Weak" }));
    await waitFor(() => expect(tree.onApplied).toHaveBeenCalledOnce());
    expect(confirm).not.toHaveBeenCalled();
  });
});
