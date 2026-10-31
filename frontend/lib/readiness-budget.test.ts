import { afterEach, describe, expect, it, vi } from "vitest";
import { fixtureCurriculum } from "./curriculum-storage.test-util";
import { computeBudgetAdjustments, DEFAULT_BUDGET } from "./readiness-budget";
import { decisionName, writeCurriculum } from "./curriculum-storage";
import { memoryStorage } from "./memory-storage.test-util";
import { loadRemediationPlan, loadSavedRemediationPlan, readRemediationPlanState, saveRemediationPlan } from "./remediation-plan";

afterEach(() => vi.unstubAllGlobals());

for (const pkg of ["us-dnp3-substation", "eu-iec104-substation"]) {
  describe(pkg, () => {
    const binding = fixtureCurriculum(`lab-definitions/packages/${pkg}`);
    const vendor = pkg.startsWith("us") ? "restrict-vendor-to-ot" : "restrict-vendor-to-station";
    const dpi = pkg.startsWith("us") ? "dnp3-dpi" : "iec104-dpi";
    const calculate = (answers: Record<string, string>, defaults = false) =>
      computeBudgetAdjustments(Object.entries(answers).map(([key, verdict]) => ({ key, verdict, label: key, good: false })), binding, defaults);
    const cap = (answers: Record<string, string>, team: string) => calculate(answers).teams.find((t) => t.name === team)!.capacity_hours;

    it("keeps baseline values with unanswered, good and unknown answers", () => {
      for (const answers of [{}, Object.fromEntries(binding.readiness.map((r) => [r.id, r.good])), { "ot-eng-availability": "unknown" }]) {
        const model = calculate(answers);
        expect(model.teams).toEqual(binding.teams);
        expect(model.catalog).toEqual(binding.catalog);
        expect(model.blockedReasons).toEqual({});
      }
    });

    it("implements the role caps and overhead table", () => {
      expect(cap({ "ot-eng-availability": "STRETCHED" }, "OT Engineer")).toBe(60);
      expect(cap({ "ot-eng-availability": "UNAVAILABLE" }, "OT Engineer")).toBe(0);
      expect(cap({ "fw-admin-skills": "VENDOR_LED" }, "Firewall Admin")).toBe(80);
      expect(cap({ "fw-admin-skills": "GAP" }, "Firewall Admin")).toBe(40);
      const model = calculate({ "fw-admin-skills": "VENDOR_LED", "vendor-change-window": "NEGOTIATING", "ot-eng-availability": "STRETCHED" });
      expect(model.catalog.find((a) => a.id === dpi)!.effort_hours).toBe(binding.catalog.find((a) => a.id === dpi)!.effort_hours + 20);
      expect(model.catalog.find((a) => a.id === vendor)!.effort_hours).toBe(75);
      expect(model.actionReasons[vendor].join()).toContain("NEGOTIATING");
      expect(model.teamReasons["OT Engineer"].join()).toContain("STRETCHED");
    });

    it("blocks OT, DPI, vendor and disputed-signoff actions; unclear only warns", () => {
      const unavailable = calculate({ "ot-eng-availability": "UNAVAILABLE" });
      for (const action of binding.catalog.filter((a) => a.roles.includes("OT Engineer"))) {
        expect(unavailable.blockedReasons[action.id]?.join()).toContain("UNAVAILABLE");
      }
      expect(calculate({ "fw-admin-skills": "GAP" }).blockedReasons[dpi]?.join()).toContain("GAP");
      for (const id of [vendor, "restrict-enterprise-to-vendor", "block-vendor-to-field", "vendor-zta-broker"]) {
        expect(calculate({ "vendor-change-window": "NOT_DISCUSSED" }).blockedReasons[id]?.join()).toContain("NOT_DISCUSSED");
      }
      expect(calculate({ "risk-acceptance-authority": "DISPUTED" }).blockedReasons["block-enterprise-to-field"]?.join()).toContain("DISPUTED");
      expect(calculate({ "risk-acceptance-authority": "UNCLEAR" }).blockedReasons).toEqual({});
    });

    it("default mode restores all caps, effort and eligibility without mutating data", () => {
      const before = JSON.stringify(binding);
      const model = calculate({ "ot-eng-availability": "UNAVAILABLE", "fw-admin-skills": "VENDOR_LED", "vendor-change-window": "NOT_DISCUSSED" }, true);
      expect(model.teams).toEqual(binding.teams);
      expect(model.catalog).toEqual(binding.catalog);
      expect(model.blockedReasons).toEqual({});
      expect(JSON.stringify(binding)).toBe(before);
    });

    it("filters every draft read without visiting the picker, retaining saved intent and scope", () => {
      vi.stubGlobal("window", { localStorage: memoryStorage() });
      const scope = { packageId: pkg, revision: 1, curriculum: binding };
      const saved = { exerciseId: binding.roles.planning, selectedActionIds: [vendor, "improve-logging"], savedAt: "original" };
      saveRemediationPlan(scope, saved);
      const answer = (verdict: string) => writeCurriculum(scope, decisionName(binding.roles.requirements, "vendor-change-window"), verdict);
      answer("NOT_DISCUSSED");
      expect(loadRemediationPlan(scope)?.selectedActionIds).toEqual(["improve-logging"]);
      expect(readRemediationPlanState(scope).excluded[0]).toMatchObject({ id: vendor });
      expect(loadSavedRemediationPlan(scope)).toEqual(saved);
      writeCurriculum(scope, DEFAULT_BUDGET, "true");
      expect(loadRemediationPlan(scope)).toEqual(saved);
      writeCurriculum(scope, DEFAULT_BUDGET, "false");
      answer("SECURED");
      expect(loadRemediationPlan(scope)).toEqual(saved);
      expect(loadRemediationPlan({ ...scope, revision: 2 })).toBeNull();
      expect(loadRemediationPlan({ ...scope, packageId: "another" })).toBeNull();
    });
  });
}

it("combines constraints order-independently and rounds effort only once", () => {
  const binding = structuredClone(fixtureCurriculum("lab-definitions/packages/us-dnp3-substation"));
  binding.catalog[0].effort_hours = 11;
  const id = binding.catalog[0].id;
  binding.budgetAdjustments = [
    { source: "ot-eng-availability", verdict: "STRETCHED", teamCaps: { "OT Engineer": 60 }, effort: [{ actions: [id], multiplier: 1.5, extraHours: 2 }] },
    { source: "fw-admin-skills", verdict: "GAP", teamCaps: { "OT Engineer": 40 }, effort: [{ actions: [id], multiplier: 1.5, extraHours: 3 }] },
  ];
  const answers = binding.budgetAdjustments.map((a) => ({ key: a.source, verdict: a.verdict, good: false, label: a.source }));
  const model = computeBudgetAdjustments(answers, binding);
  expect(model.catalog[0].effort_hours).toBe(30); // round(11 × 2.25 + 5), not staged rounding
  expect(model.teams[0].capacity_hours).toBe(40);
  expect(model.actionReasons[id]).toHaveLength(2);
  binding.budgetAdjustments.reverse();
  expect(computeBudgetAdjustments(answers, binding).catalog).toEqual(model.catalog);
});
