import { describe, expect, it } from "vitest";
import { EU_PACKAGE, US_PACKAGE } from "./operator-fixtures.test-util";
import {
  activePolicyWording,
  isHardened,
  matchesExpected,
  packagePolicies,
  policyWording,
  US_WORKSHOP_POLICY_WORDING,
} from "./policy-wording";

const US_TABLE = { weak: { label: "Weak Baseline", description: "US text" } };

describe("packagePolicies", () => {
  it("takes the names from the package and US wording only for the US package", () => {
    expect(packagePolicies(US_PACKAGE)).toEqual({ names: ["weak", "improved"], usWording: true });
    expect(packagePolicies(EU_PACKAGE)).toEqual({ names: ["weak", "improved"], usWording: false });
  });
});

describe("policyWording", () => {
  it("uses the US table for the US package", () => {
    expect(policyWording("weak", packagePolicies(US_PACKAGE), US_TABLE)).toEqual(US_TABLE.weak);
  });

  it("shows any other package's policy by its declared name", () => {
    expect(policyWording("weak", packagePolicies(EU_PACKAGE), US_TABLE)).toEqual({ label: "weak", description: "" });
  });

  it("falls back to the name for a US policy the table does not know", () => {
    expect(policyWording("audit", packagePolicies(US_PACKAGE), US_TABLE)).toEqual({ label: "audit", description: "" });
  });
});

describe("activePolicyWording", () => {
  it.each(["weak", "partial", "hardened"] as const)("labels an applied %s plan without changing the custom identity", (label) => {
    const plan = { packageId: US_PACKAGE.id, revision: 2, label, actionIds: [], policyHash: "abc", appliedAt: "now" };
    expect(activePolicyWording("custom", packagePolicies(US_PACKAGE), US_WORKSHOP_POLICY_WORDING, "plan", plan).label).toBe(`Your plan — ${label[0].toUpperCase() + label.slice(1)}`);
  });

  it("uses exact US wording and consistently labels a custom policy", () => {
    expect(activePolicyWording("weak", packagePolicies(US_PACKAGE), US_WORKSHOP_POLICY_WORDING)).toEqual({
      label: "Weak Baseline",
      description: "",
    });
    expect(activePolicyWording("custom", packagePolicies(US_PACKAGE), US_WORKSHOP_POLICY_WORDING)).toEqual({
      label: "Custom policy",
      description: "",
    });
  });

  it("preserves declared names outside the US and for unknown configurations", () => {
    expect(activePolicyWording("improved", packagePolicies(EU_PACKAGE), US_WORKSHOP_POLICY_WORDING)).toEqual({
      label: "improved",
      description: "",
    });
    expect(activePolicyWording("audit", packagePolicies(US_PACKAGE), US_WORKSHOP_POLICY_WORDING)).toEqual({
      label: "audit",
      description: "",
    });
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
