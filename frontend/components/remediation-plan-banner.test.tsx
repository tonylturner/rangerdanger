// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { CurriculumScopeProvider } from "../lib/curriculum-scope";
import { fixtureCurriculum } from "../lib/curriculum-storage.test-util";
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
    const curriculum = fixtureCurriculum(`lab-definitions/packages/${pkg.id}`);
    render(
      <CurriculumScopeProvider pkg={{ ...pkg, curriculum }}>
        <RemediationPlanBanner currentExerciseId={curriculum.roles.hardening} />
      </CurriculumScopeProvider>,
    );

    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("href"))
      .toBe(`/exercises/${curriculum.roles.planning}`);
  });

  it("uses the binding rather than a fixed planning exercise ID", () => {
    const curriculum = fixtureCurriculum(`lab-definitions/packages/${US_PACKAGE.id}`);
    curriculum.roles = { ...curriculum.roles, planning: "replacement-planning" };
    render(
      <CurriculumScopeProvider pkg={{ ...US_PACKAGE, curriculum }}>
        <RemediationPlanBanner currentExerciseId={curriculum.roles.hardening} />
      </CurriculumScopeProvider>,
    );

    expect(screen.getByRole("link", { name: "Remediation Planning" }).getAttribute("href"))
      .toBe("/exercises/replacement-planning");
  });
});
