// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, expect, it } from "vitest";
import { CurriculumScopeProvider, scopeOf } from "../lib/curriculum-scope";
import { curriculumKey, exerciseName } from "../lib/curriculum-storage";
import { US_PACKAGE, EU_PACKAGE } from "../lib/operator-fixtures.test-util";
import { saveProgress } from "../lib/scenario-runner-storage";
import { NotesPanel } from "./notes-panel";
import { HintBlock } from "./scenario-description-blocks";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { CATCH_UP_TITLE } from "../lib/catch-up";

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() }));
afterEach(() => {
  cleanup();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

it.each([US_PACKAGE, EU_PACKAGE])("shows scoped Notes, updates across tabs and clears on revision changes in $id", (pkg) => {
  const scope = scopeOf(pkg);
  const panel = (revision = pkg.revision) =>
    <CurriculumScopeProvider pkg={{ ...pkg, revision }}>
      <NotesPanel sourceScenario="planning" sourceScenarioLabel="1.4" title="Your change request (Lab 1.4)" />
    </CurriculumScopeProvider>;
  const tree = render(panel());
  expect(tree.container.textContent).toContain("Nothing saved in Lab 1.4");
  const record = { completedStepIds: [], skippedStepIds: [], notes: "CR-17\nOwner: shift engineer", cmdLog: [] };
  act(() => saveProgress(scope, "planning", record));
  expect(tree.container.textContent).toContain(record.notes);
  expect(tree.container.querySelector("textarea")).toBeNull();
  window.localStorage.setItem(curriculumKey(scope, exerciseName("planning")), JSON.stringify({ ...record, notes: "Peer reviewed" }));
  fireEvent(window, new StorageEvent("storage", { key: curriculumKey(scope, exerciseName("planning")) }));
  expect(tree.container.textContent).toContain("Peer reviewed");
  tree.rerender(panel(pkg.revision + 1));
  expect(tree.container.textContent).toContain("Nothing saved in Lab 1.4");
});

it("keeps catch-up hints collapsed and distinguishes them from answer hints", () => {
  const tree = render(<HintBlock title={CATCH_UP_TITLE} body={"From Kali:\n    curl example"}
    runIdPrefix="catch-up" runningId={null} onRun={() => {}} scenarioId="fixture" />);
  expect(tree.queryByRole("button", { name: "Run" })).toBeNull();
  const toggle = tree.getByRole("button", { name: new RegExp(CATCH_UP_TITLE) });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  expect(toggle.parentElement?.className).toContain("border-sky");
  fireEvent.click(toggle);
  expect(tree.getByRole("button", { name: "Run" })).toBeTruthy();
});

it("jumps from an Apply Your Plan hint to the expanded policy controls", () => {
  const tree = render(<>
    <HintBlock title="Catch up" body="Use Apply Your Plan if behind schedule."
      runIdPrefix="catch-up" runningId={null} onRun={null} scenarioId="fixture" />
    <details id="catch-up-controls"><summary>Policy controls</summary>Apply Your Plan</details>
  </>);
  fireEvent.click(tree.getByRole("button", { name: /Catch up/ }));
  fireEvent.click(tree.getByRole("link", { name: "Jump to policy controls" }));
  expect(tree.container.querySelector("details")?.open).toBe(true);
});
