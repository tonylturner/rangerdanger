import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { US_PLANS, EU_PLANS } from "../lib/plan-fixtures.test-util";
import { ApplyPlanButton } from "./apply-plan-button";

let panel: ReactTestRenderer;
afterEach(() => { act(() => panel?.unmount()); vi.unstubAllGlobals(); });

describe("Apply Your Plan", () => {
  it.each([...US_PLANS, ...EU_PLANS])("applies $packageId $label by scoped IDs and keeps all warnings", async (plan) => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ applied: true, warnings: ["Over budget", "Review deferred controls"], plan })));
    vi.stubGlobal("fetch", fetch);
    const onApplied = vi.fn();
    act(() => { panel = create(<ApplyPlanButton plan={plan} onApplied={onApplied} />); });
    expect(panel.root.findByType("button").props.disabled).toBe(false);
    await act(async () => { await panel.root.findByType("button").props.onClick(); });
    expect(fetch).toHaveBeenCalledWith("/api/firewall/apply-plan", expect.objectContaining({
      body: JSON.stringify({ packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id) }),
    }));
    expect(onApplied).toHaveBeenCalledOnce();
    expect(JSON.stringify(panel.toJSON())).toContain("Over budget");
    expect(JSON.stringify(panel.toJSON())).toContain("Review deferred controls");
    expect(JSON.stringify(panel.toJSON())).toContain(plan.label === "weak" ? "Applied your plan — Weak" : plan.label === "partial" ? "Applied your plan — Partial" : "Applied your plan — Hardened");
  });
  it("allows a planning-only Weak plan", async () => {
    const plan = { ...EU_PLANS[0], actions: EU_PLANS[0].actions.map((a) => ({ ...a, chosen: a.id === "iec104-dpi" })) };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ applied: true, warnings: [], plan })));
    vi.stubGlobal("fetch", fetch);
    act(() => { panel = create(<ApplyPlanButton plan={plan} onApplied={vi.fn()} />); });
    await act(async () => { await panel.root.findByType("button").props.onClick(); });
    expect(JSON.parse(fetch.mock.calls[0][1].body).actionIds).toEqual(["iec104-dpi"]);
    expect(JSON.stringify(panel.toJSON())).toContain("Applied your plan — Weak");
  });
  it.each([409, 422])("keeps backend errors and warnings without claiming success at %i", async (status) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "Plan rejected", warnings: ["Safety lint failed"] }), { status })));
    const onApplied = vi.fn();
    act(() => { panel = create(<ApplyPlanButton plan={US_PLANS[0]} onApplied={onApplied} />); });
    await act(async () => { await panel.root.findByType("button").props.onClick(); });
    expect(JSON.stringify(panel.toJSON())).toContain("Plan rejected");
    expect(JSON.stringify(panel.toJSON())).toContain("Safety lint failed");
    expect(onApplied).not.toHaveBeenCalled();
  });
  it("cannot apply before a compile response exists", () => {
    act(() => { panel = create(<ApplyPlanButton plan={null} onApplied={vi.fn()} />); });
    expect(panel.root.findByType("button").props.disabled).toBe(true);
  });
});
