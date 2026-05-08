import { describe, it, expect } from "vitest";
import { decisionStorageKey, REMEDIATION_PLAN_STORAGE_KEY } from "./decision-storage";

describe("decisionStorageKey", () => {
  it("produces the canonical decision:<scenario>:<id> shape", () => {
    expect(decisionStorageKey("baseline-assessment", "enterprise-to-field"))
      .toBe("decision:baseline-assessment:enterprise-to-field");
  });

  it("treats kebab-case ids verbatim — no normalisation", () => {
    // The fence parser preserves whatever the YAML author wrote.
    // If a YAML uses underscores by mistake, the key reflects that;
    // the contract test catches the mismatch downstream rather than
    // silently coercing the format here.
    expect(decisionStorageKey("baseline-assessment", "enterprise_to_field"))
      .toBe("decision:baseline-assessment:enterprise_to_field");
  });

  it("empty inputs produce a still-parseable key (no string fallback)", () => {
    expect(decisionStorageKey("", "")).toBe("decision::");
  });
});

describe("REMEDIATION_PLAN_STORAGE_KEY", () => {
  it("is the literal key both Lab 1.4 (write) and Lab 2.4 (read) agree on", () => {
    // Pinned because remediation-plan.ts and the PlanCoveragePanel
    // are in different files and a typo would silently zero out
    // Lab 2.4's coverage panel.
    expect(REMEDIATION_PLAN_STORAGE_KEY).toBe("rd-remediation-plan");
  });
});
