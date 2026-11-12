import { fixtureCourse, US_COURSE } from "./curriculum-storage.test-util";
import { describe, expect, it } from "vitest";
import { US_PLANS } from "./plan-fixtures.test-util";
import {
  actionLabel,
  injectDynamicContent,
  showsValidateButton,
} from "./scenario-runner-logic";

describe("scenario runner logic", () => {
  it("binds dynamic content by step ID, not title fragments", () => {
    const plan = US_PLANS[0];
    expect(injectDynamicContent("original", "phase-3-create-minimal-required-rules", plan, US_COURSE, "hardening-configurations")).toBe("original");
    expect(injectDynamicContent("original", "Phase 3: Create Minimal Policy", plan, US_COURSE, US_COURSE.features.segmentationPlan!.activities.implementation)).toBe("original");
    expect(injectDynamicContent("original", "phase-3-create-minimal-required-rules", plan, US_COURSE, US_COURSE.features.segmentationPlan!.activities.implementation)).not.toBe("original");
  });

  it("maps action labels and passes unknown actions through", () => {
    expect(actionLabel("sequence")).toBe("auto");
    expect(actionLabel("custom-action")).toBe("custom-action");
  });

  it("leaves descriptions unchanged without a plan", () => {
    const description = "Existing step instructions.";
    expect(injectDynamicContent(description, "phase-5-validate-allowed-traffic", null, undefined, "firewall-implementation")).toBe(description);
  });

  it("injects proof expectations for phase 5 instead of conditional browser tests", () => {
    const description = injectDynamicContent("original", "phase-5-validate-allowed-traffic", US_PLANS[0], US_COURSE, US_COURSE.features.segmentationPlan!.activities.implementation);
    expect(description).toContain("Expected result");
    expect(description).toContain("Hardened reference");
    expect(description).toContain("TCP/502");
  });

  it("keeps US visibility metadata and follows the declared validator elsewhere", () => {
    const us = { package_id: "us-dnp3-substation", validator: "us-baseline-assessment" };
    expect(showsValidateButton({ ...us, id: "firewall-implementation" }, US_COURSE)).toBe(true);
    // A US planning lab with a validator stays hidden.
    expect(showsValidateButton({ ...us, id: "baseline-assessment" }, US_COURSE)).toBe(false);
    const eu = { package_id: "eu-iec104-substation", id: "eu-hardening-configurations" };
    expect(showsValidateButton({ ...eu, validator: "eu-hardening-configurations" })).toBe(true);
    expect(showsValidateButton({ ...eu, validator: "" })).toBe(false);
  });
});

describe("shipped EU curriculum visibility", () => {
  it("exposes exactly the five validators and neither planning slot", () => {
    const eu = fixtureCourse("lab-definitions/packages/eu-iec104-substation");
    for (const activity of eu.modules.flatMap((module) => module.activities)) {
      const id = activity.scenario;
      const validates = ![eu.features.segmentationPlan!.activities.requirements, eu.features.segmentationPlan!.activities.planning].includes(id);
      expect(showsValidateButton({ package_id: "eu-iec104-substation", id, validator: validates ? id : "" }, eu)).toBe(validates);
    }
  });
});
