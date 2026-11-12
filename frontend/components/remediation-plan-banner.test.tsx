// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { fixtureCourse } from "../lib/curriculum-storage.test-util";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { memoryStorage } from "../lib/memory-storage.test-util";

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() }));
afterEach(() => {
  cleanup();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

describe("RemediationPlanBanner planning link", () => {
  it.each([US_PACKAGE, EU_PACKAGE])("opens the planning exercise declared by $id", (pkg) => {
    const curriculum = fixtureCourse(`lab-definitions/packages/${pkg.id}`);
    render(
      <CurriculumScopeProvider pkg={{ ...pkg, course: curriculum }}>
        <RemediationPlanBanner currentExerciseId={curriculum.modules[1].activities[1].scenario} />
      </CurriculumScopeProvider>,
    );

    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("target")).toBe("_blank");
    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("href"))
      .toBe(`/exercises/${curriculum.features.segmentationPlan!.activities.planning}`);
  });

  it("uses the binding rather than a fixed planning exercise ID", () => {
    const curriculum = fixtureCourse(`lab-definitions/packages/${US_PACKAGE.id}`);
    curriculum.features.segmentationPlan!.activities.planning = "replacement-planning";
    render(
      <CurriculumScopeProvider pkg={{ ...US_PACKAGE, course: curriculum }}>
        <RemediationPlanBanner currentExerciseId={curriculum.modules[1].activities[1].scenario} />
      </CurriculumScopeProvider>,
    );

    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("target")).toBe("_blank");
    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("href"))
      .toBe("/exercises/replacement-planning");
  });
});
