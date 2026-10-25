import { afterEach, describe, expect, it, vi } from "vitest";
import { compilePlan, applyPlan } from "./api";
import { ALL_PLANS, EU_PLANS, US_PLANS } from "./plan-fixtures.test-util";
import { fixtureCurriculum, US_CURRICULUM } from "./curriculum-storage.test-util";
import { renderPlanTests, renderRuleTable } from "./plan-presentation";

afterEach(() => vi.unstubAllGlobals());
describe("server-owned plans across packages and selection states", () => {
  it.each([
    { plans: US_PLANS, binding: US_CURRICULUM, revision: 2, authorized: 8, unauthorized: 13, ruleCounts: [12, 12, 11], deferredCounts: [7, 6, 0], remoteIds: ["U12", "U13"] },
    { plans: EU_PLANS, binding: fixtureCurriculum("lab-definitions/packages/eu-iec104-substation"), revision: 5, authorized: 10, unauthorized: 27, ruleCounts: [20, 19, 10], deferredCounts: [10, 9, 0], remoteIds: ["U3", "U4"] },
  ])("renders the real $authorized + $unauthorized tuple universe at revision $revision", ({ plans, binding, revision, authorized, unauthorized, ruleCounts, deferredCounts, remoteIds }) => {
    expect(plans.map((p) => p.label)).toEqual(["weak", "partial", "hardened"]);
    expect(new Set(plans.map((p) => p.policyHash)).size).toBe(3);
    plans.forEach((plan, i) => {
      expect(plan.revision).toBe(revision);
      expect(plan.policyHash).toMatch(/^[a-f0-9]{64}$/);
      expect(plan.rules).toHaveLength(ruleCounts[i]);
      expect(plan.actions.map((a) => a.id)).toEqual(binding.catalog.map((a) => a.id));
      expect(plan.actions.filter((a) => a.chosen).map((a) => a.id)).toEqual(
        i === 0 ? [] : i === 1 ? ["restrict-enterprise-to-vendor"] : binding.catalog.map((a) => a.id),
      );
      for (const [category, prefix, count] of [["authorized", "A", authorized], ["unauthorized", "U", unauthorized]] as const) {
        expect(plan.expectations.filter((r) => r.category === category).map((r) => r.id)).toEqual(
          Array.from({ length: count }, (_, n) => `${prefix}${n + 1}`),
        );
      }
      // Baseline includes all authorized rows in both packages.
      expect(renderPlanTests(plan, binding, "baseline", "positive").match(/\| A\d+:/g)).toHaveLength(authorized);
      expect(plan.expectations.filter((r) => r.category === "authorized").every((r) =>
        r.planVerdict === "allow" && r.hardenedVerdict === "allow" && r.owners.length === 0,
      )).toBe(true);
      if (i === 2) expect(plan.expectations.every((r) => r.planVerdict === r.hardenedVerdict)).toBe(true);
      for (const id of remoteIds) {
        expect(plan.expectations.find((r) => r.id === id)).toMatchObject({
          owners: ["restrict-enterprise-to-vendor"], planVerdict: i === 0 ? "allow" : "deny", hardenedVerdict: "deny",
        });
      }
      const deferred = plan.rules.filter((r) => r.origin.kind === "deferred");
      expect(deferred).toHaveLength(deferredCounts[i]);
      for (const { origin } of deferred) {
        expect(plan.actions.find((a) => a.id === origin.action)).toMatchObject({ kind: "enforce", chosen: false });
        expect(renderRuleTable(plan, binding)).toContain(
          `deferred: ${binding.catalog.find((a) => a.id === origin.action)!.title} — Weak exposure open`,
        );
      }
    });
    // The partial plan closes only the two enterprise/vendor remote tuples.
    expect(plans[1].expectations.filter((r, i) => r.planVerdict !== plans[0].expectations[i].planVerdict).map((r) => r.id)).toEqual(remoteIds);
  });
  it("represents EU closures by removing deferred rules, not invented selected rules", () => {
    expect(EU_PLANS[2].rules.every((r) => r.origin.kind === "required")).toBe(true);
    expect(EU_PLANS[1].rules.map((r) => r.rule.id)).not.toContain("weak-enterprise-vendor-remote");
    expect(US_PLANS[1].rules[0]).toMatchObject({ rule: { id: "enterprise-to-vendor-only" }, origin: { kind: "selected", action: "restrict-enterprise-to-vendor" } });
  });
  it("retains EU unauthorized invariant denials even in the Weak plan", () => {
    for (const plan of EU_PLANS) {
      expect(plan.expectations.filter((r) => r.category === "unauthorized" && r.owners.length === 0).map((r) => r.id)).toEqual(["U2", "U26", "U27"]);
      for (const id of ["U2", "U26", "U27"]) {
        expect(plan.expectations.find((r) => r.id === id)).toMatchObject({ planVerdict: "deny", hardenedVerdict: "deny", bridged: false });
      }
    }
  });
  it.each(ALL_PLANS)("round-trips $packageId $label unchanged", async (plan) => {
    const body = { packageId: plan.packageId, revision: plan.revision, actionIds: plan.actions.filter((a) => a.chosen).map((a) => a.id) };
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(plan)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ applied: true, warnings: ["Budget warning"], plan })));
    vi.stubGlobal("fetch", fetch);
    expect(await compilePlan(body)).toEqual(plan);
    expect(await applyPlan(body)).toEqual({ applied: true, warnings: ["Budget warning"], plan });
    expect(fetch.mock.calls.map(([, init]) => JSON.parse(init.body))).toEqual([body, body]);
  });
});
