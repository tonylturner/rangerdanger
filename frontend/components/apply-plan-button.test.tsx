// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { ApplyPlanButton } from "./apply-plan-button";

afterEach(() => { vi.unstubAllGlobals(); });

describe("Apply Your Plan", () => {
  it.each([...US_PLANS, ...EU_PLANS])("applies $packageId $label by scoped IDs and keeps all warnings", async (plan) => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ applied: true, warnings: ["Over budget", "Review deferred controls"], plan })));
    vi.stubGlobal("fetch", fetch);
    const onApplied = vi.fn();
    render(<ApplyPlanButton plan={plan} onApplied={onApplied} />);
    const button = screen.getByRole("button", { name: /^Apply Your Plan/ });
    expect(button.hasAttribute("disabled")).toBe(false);
    fireEvent.click(button);
    await waitFor(() => expect(onApplied).toHaveBeenCalledOnce());
    expect(fetch).toHaveBeenCalledWith("/api/firewall/apply-plan", expect.objectContaining({
      body: JSON.stringify({ packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id) }),
    }));
    expect(onApplied).toHaveBeenCalledOnce();
    expect(screen.getByText("Over budget")).toBeTruthy();
    expect(screen.getByText("Review deferred controls")).toBeTruthy();
    expect(screen.getByText(plan.label === "weak" ? "Applied your plan — Weak" : plan.label === "partial" ? "Applied your plan — Partial" : "Applied your plan — Hardened")).toBeTruthy();
  });
  it("allows a planning-only Weak plan", async () => {
    const plan = { ...EU_PLANS[0], actions: EU_PLANS[0].actions.map((a) => ({ ...a, chosen: a.id === "iec104-dpi" })) };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ applied: true, warnings: [], plan })));
    vi.stubGlobal("fetch", fetch);
    render(<ApplyPlanButton plan={plan} onApplied={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /^Apply Your Plan/ }));
    await screen.findByText("Applied your plan — Weak");
    expect(JSON.parse(fetch.mock.calls[0][1].body).actionIds).toEqual(["iec104-dpi"]);
    expect(screen.getByText("Applied your plan — Weak")).toBeTruthy();
  });
  it.each([409, 422])("keeps backend errors and warnings without claiming success at %i", async (status) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "Plan rejected", warnings: ["Safety lint failed"] }), { status })));
    const onApplied = vi.fn();
    render(<ApplyPlanButton plan={US_PLANS[0]} onApplied={onApplied} />);
    fireEvent.click(screen.getByRole("button", { name: /^Apply Your Plan/ }));
    expect(await screen.findByText(`${status} Plan rejected`)).toBeTruthy();
    expect(screen.getByText("Safety lint failed")).toBeTruthy();
    expect(onApplied).not.toHaveBeenCalled();
  });
  it("cannot apply before a compile response exists", () => {
    render(<ApplyPlanButton plan={null} onApplied={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Apply Your Plan" }).hasAttribute("disabled")).toBe(true);
  });
});
