import { fixtureCurriculum, US_CURRICULUM } from "./curriculum-storage.test-util";
import { describe, expect, it } from "vitest";
import { buildDynamicPlan } from "./remediation-to-rules";
import {
  actionLabel,
  deviceLabel,
  injectDynamicContent,
  isBaselineStep,
  isSegmentationStep,
  showsValidateButton,
} from "./scenario-runner-logic";

describe("scenario runner logic", () => {
  it("binds dynamic content by step ID, not title fragments", () => {
    const plan = buildDynamicPlan(null, US_CURRICULUM);
    expect(injectDynamicContent("original", "Phase 3: Create Minimal Policy", plan)).toBe("original");
    expect(injectDynamicContent("original", "phase-3-create-minimal-required-rules", plan)).not.toBe("original");
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
    expect(injectDynamicContent(description, "phase-5-validate-allowed-traffic", null)).toBe(description);
  });

  it("injects positive validation commands for phase 5", () => {
    const plan = buildDynamicPlan({
      exerciseId: "remediation-planning",
      selectedActionIds: ["positive-validation"],
      savedAt: "2026-01-01T00:00:00.000Z",
    }, US_CURRICULUM);

    const description = injectDynamicContent(
      "Existing step instructions.",
      "phase-5-validate-allowed-traffic",
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

  it("keeps US visibility metadata and follows the declared validator elsewhere", () => {
    const us = { package_id: "us-dnp3-substation", validator: "us-baseline-assessment" };
    expect(showsValidateButton({ ...us, id: "firewall-implementation" }, US_CURRICULUM)).toBe(true);
    // A US planning lab with a validator stays hidden.
    expect(showsValidateButton({ ...us, id: "baseline-assessment" }, US_CURRICULUM)).toBe(false);
    const eu = { package_id: "eu-iec104-substation", id: "eu-hardening-configurations" };
    expect(showsValidateButton({ ...eu, validator: "eu-hardening-configurations" })).toBe(true);
    expect(showsValidateButton({ ...eu, validator: "" })).toBe(false);
  });
});

describe("shipped EU curriculum visibility", () => {
  it("exposes exactly the five validators and neither planning slot", () => {
    const eu = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");
    for (const [role, id] of Object.entries(eu.roles)) {
      const validates = role !== "requirements" && role !== "planning";
      expect(showsValidateButton({ package_id: "eu-iec104-substation", id, validator: validates ? id : "" }, eu)).toBe(validates);
    }
  });
});
