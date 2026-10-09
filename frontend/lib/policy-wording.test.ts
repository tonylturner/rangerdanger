import { describe, expect, it } from "vitest";
import { EU_PACKAGE, US_PACKAGE } from "./operator-fixtures.test-util";
import { isHardened, matchesExpected, packagePolicies, policyWording } from "./policy-wording";

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
