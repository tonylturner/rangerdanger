import { describe, expect, it } from "vitest";
import { EU_PACKAGE, US_PACKAGE } from "./operator-fixtures.test-util";
import {
  activePolicyWording,
  isHardened,
  matchesExpected,
  packagePolicies,
  policyWording,
} from "./policy-wording";

describe("package policy wording", () => {
  it.each([US_PACKAGE, EU_PACKAGE])("takes every label and description from $id", (pkg) => {
    expect(packagePolicies(pkg)).toBe(pkg.policies);
    for (const { name, label, description } of pkg.policies) {
      expect(policyWording(name, packagePolicies(pkg))).toEqual({ label, description });
      expect(activePolicyWording(name, packagePolicies(pkg))).toEqual({ label, description });
    }
  });

  it("uses authored wording even when names and operator view are unfamiliar", () => {
    const pkg = { ...EU_PACKAGE, operator_view: "new-view", policies: [{ name: "audit", label: "Review mode", description: "Observe the range." }] };
    expect(policyWording("audit", packagePolicies(pkg))).toEqual({ label: "Review mode", description: "Observe the range." });
  });

  it.each(["weak", "partial", "hardened"] as const)("labels an applied %s plan without changing the custom identity", (label) => {
    for (const pkg of [US_PACKAGE, EU_PACKAGE]) {
      const plan = { packageId: pkg.id, revision: pkg.revision, label, actionIds: [], policyHash: "abc", appliedAt: "now" };
      expect(activePolicyWording("custom", packagePolicies(pkg), "plan", plan).label).toBe(`Your plan — ${label[0].toUpperCase() + label.slice(1)}`);
    }
  });

  it("distinguishes custom and unknown configurations from named policies", () => {
    expect(activePolicyWording("custom", packagePolicies(US_PACKAGE))).toEqual({ label: "Custom policy", description: "" });
    expect(activePolicyWording("unknown", packagePolicies(US_PACKAGE))).toEqual({ label: "unknown", description: "" });
    expect(activePolicyWording("weak", undefined)).toEqual({ label: "Loading…", description: "" });
  });
});

describe("matchesExpected", () => {
  it("reads the hardened alias as the hardened policy or a custom one", () => {
    for (const expected of ["hardened", "improved"]) {
      expect(matchesExpected("improved", expected)).toBe(true);
      expect(matchesExpected("custom", expected)).toBe(true);
      expect(matchesExpected("weak", expected)).toBe(false);
    }
  });

  it("needs any other named policy to be the active one", () => {
    expect(matchesExpected("weak", "weak")).toBe(true);
    expect(matchesExpected("custom", "weak")).toBe(false);
    expect(matchesExpected("weak", undefined)).toBe(true);
  });

  it("calls only the hardened policy hardened", () => {
    expect(isHardened("improved")).toBe(true);
    expect(isHardened("custom")).toBe(false);
    expect(isHardened(null)).toBe(false);
  });
});
