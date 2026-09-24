import { describe, expect, it } from "vitest";
import { buildDynamicPlan } from "./remediation-to-rules";
import {
  actionLabel,
  deviceLabel,
  injectDynamicContent,
  isBaselineStep,
  isSegmentationStep,
  titleMatches,
} from "./scenario-runner-logic";

describe("scenario runner logic", () => {
  it("matches phase title fragments", () => {
    expect(titleMatches("Phase 3: Create Minimal Policy", ["create minimal", "phase 3"])).toBe(true);
    expect(titleMatches("Review the completed baseline", ["create minimal", "phase 3"])).toBe(false);
  });

  it("maps action labels and passes unknown actions through", () => {
    expect(actionLabel("sequence")).toBe("auto");
    expect(actionLabel("custom-action")).toBe("custom-action");
  });

  it("maps device labels and passes unknown devices through", () => {
    expect(deviceLabel("relay")).toBe("Feeder Breaker (10.40.40.20)");
    expect(deviceLabel("custom-device")).toBe("custom-device");
  });

  it("leaves descriptions unchanged without a plan", () => {
    const description = "Existing step instructions.";
    expect(injectDynamicContent(description, "Phase 5: Validate Allowed Traffic", null)).toBe(description);
  });

  it("injects positive validation commands for phase 5", () => {
    const plan = buildDynamicPlan({
      exerciseId: "remediation-planning",
      selectedActionIds: ["positive-validation"],
      savedAt: "2026-01-01T00:00:00.000Z",
    });

    const description = injectDynamicContent(
      "Existing step instructions.",
      "Phase 5: Validate Allowed Traffic",
      plan,
    );

    expect(description).toContain("**Positive validation commands (based on your plan):**");
    expect(description).toContain("**RTAC → field Modbus**");
    expect(description).toContain("mbpoll -m tcp");
  });

  it("identifies segmentation and baseline steps", () => {
    expect(isSegmentationStep("Apply hardened policy")).toBe(true);
    expect(isSegmentationStep("Collect observations")).toBe(false);
    expect(isBaselineStep("Observe the baseline")).toBe(true);
    expect(isBaselineStep("Apply hardened policy")).toBe(false);
  });
});
