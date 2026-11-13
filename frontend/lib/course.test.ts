import React, { Children, isValidElement } from "react";
import { expect, it } from "vitest";
import { Page } from "@react-pdf/renderer";
import { WorkbookPDF } from "./exercise-pdf";
import { fixtureCourse, fixtureScenarios } from "./curriculum-storage.test-util";
import { activityOf } from "./course";
import { getExerciseNodes } from "./exercise-nodes";
import { showsValidateButton } from "./scenario-runner-logic";

const directory = "backend/internal/labs/testdata/course-basic/packages/basic";
const root = "backend/internal/labs/testdata/course-basic";

it("projects a featureless course in B then A order without planning", () => {
  const course = fixtureCourse(directory, undefined, root);
  const scenarios = fixtureScenarios(directory, root);
  expect(course.features.segmentationPlan).toBeUndefined();
  expect(scenarios.map((scenario) => [scenario.id, scenario.label])).toEqual([["first", "B"], ["second", "A"]]);
  expect(activityOf(course, "second")?.validate).toBe(false);
  expect(showsValidateButton({ ...scenarios[0], validator: "fixture" }, course)).toBe(true);
  expect(showsValidateButton({ ...scenarios[1], validator: "fixture" }, course)).toBe(false);
  expect(getExerciseNodes("first", undefined, course)).toEqual(["fw-1"]);
});

it("keeps the API course order in workbook contents and exercise pages", () => {
  const document = WorkbookPDF({ scenarios: fixtureScenarios(directory, root), generatedAt: "fixture" });
  const pages = Children.toArray(document.props.children).filter((child) => isValidElement(child) && child.type === Page);
  const keys = pages.map((page) => (page as React.ReactElement).key);
  expect(keys.filter((key) => String(key).endsWith("-h")).map((key) => String(key).split("$").at(-1))).toEqual(["first-h", "second-h"]);
  const links: string[] = [];
  const visit = (node: React.ReactNode) => Children.forEach(node, (child) => {
    if (!isValidElement<{src?: string; children?: React.ReactNode}>(child)) return;
    if (child.props.src?.startsWith("#ex-")) links.push(child.props.src);
    visit(child.props.children);
  });
  visit(document.props.children);
  expect(links).toEqual(["#ex-first", "#ex-second"]);
});
