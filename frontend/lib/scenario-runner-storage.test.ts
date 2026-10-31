import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearProgress, completionPercent, loadProgress, saveProgress } from "./scenario-runner-storage";
import type { CurriculumScope } from "./curriculum-storage";
import { memoryStorage } from "./memory-storage.test-util";

const US: CurriculumScope = { packageId: "us-dnp3-substation", revision: 1 };

const scenario = {
  id: "baseline-assessment",
  steps: [
    { id: "review-topology", title: "Review topology", description: "" },
    { id: "capture-traffic", title: "Capture traffic", description: "" },
    { id: "record-findings", title: "Record findings", description: "" },
    { id: "summarise", title: "Summarise", description: "" },
  ],
};

describe("exercise progress", () => {
  let storage: Storage;
  beforeEach(() => {
    storage = memoryStorage();
    vi.stubGlobal("window", { localStorage: storage });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("stores completed steps by step id under the scoped exercise key", () => {
    saveProgress(US, scenario.id, {
      completedStepIds: ["capture-traffic", "review-topology"],
      notes: "enterprise reaches field",
      cmdLog: ["[OK] done"],
    });
    const stored = JSON.parse(storage.getItem("rd:us-dnp3-substation:1:exercise:baseline-assessment") ?? "null");
    expect(stored).toEqual({
      completedStepIds: ["capture-traffic", "review-topology"],
      notes: "enterprise reaches field",
      cmdLog: ["[OK] done"],
    });
    expect(loadProgress(US, scenario).completedStepIds).toEqual(["capture-traffic", "review-topology"]);
    expect(completionPercent(US, scenario)).toBe(50);
  });

  it("keeps progress on the same steps when steps are reordered or inserted", () => {
    saveProgress(US, scenario.id, { completedStepIds: ["record-findings"], notes: "", cmdLog: [] });
    const reordered = {
      id: scenario.id,
      steps: [{ id: "new-intro", title: "Intro", description: "" }, ...[...scenario.steps].reverse()],
    };
    expect(loadProgress(US, reordered).completedStepIds).toEqual(["record-findings"]);
  });

  it("drops step ids the scenario no longer has and duplicates", () => {
    saveProgress(US, scenario.id, {
      completedStepIds: ["summarise", "removed-step", "summarise"],
      notes: "",
      cmdLog: [],
    });
    expect(loadProgress(US, scenario).completedStepIds).toEqual(["summarise"]);
    expect(completionPercent(US, scenario)).toBe(25);
  });

  it("ignores the old index-based key", () => {
    storage.setItem(
      "rd-exercise-baseline-assessment",
      JSON.stringify({ completedSteps: [0, 1, 2], notes: "old", cmdLog: ["old"] }),
    );
    expect(loadProgress(US, scenario)).toEqual({ completedStepIds: [], notes: "", cmdLog: [] });
    expect(completionPercent(US, scenario)).toBe(0);
  });

  it("ignores index-based progress even under the scoped key", () => {
    storage.setItem(
      "rd:us-dnp3-substation:1:exercise:baseline-assessment",
      JSON.stringify({ completedSteps: [0, 1], completedStepIds: [0, 1], notes: "kept", cmdLog: [] }),
    );
    expect(loadProgress(US, scenario)).toEqual({ completedStepIds: [], notes: "kept", cmdLog: [] });
  });

  it("returns empty progress for corrupt records and another revision", () => {
    storage.setItem("rd:us-dnp3-substation:1:exercise:baseline-assessment", "{not json");
    expect(loadProgress(US, scenario).completedStepIds).toEqual([]);
    saveProgress(US, scenario.id, { completedStepIds: ["summarise"], notes: "", cmdLog: [] });
    expect(loadProgress({ ...US, revision: 2 }, scenario).completedStepIds).toEqual([]);
  });

  it("clears only this exercise", () => {
    saveProgress(US, scenario.id, { completedStepIds: ["summarise"], notes: "", cmdLog: [] });
    saveProgress(US, "other-lab", { completedStepIds: ["x"], notes: "", cmdLog: [] });
    clearProgress(US, scenario.id);
    expect(storage.getItem("rd:us-dnp3-substation:1:exercise:baseline-assessment")).toBeNull();
    expect(storage.getItem("rd:us-dnp3-substation:1:exercise:other-lab")).not.toBeNull();
  });

  it("reports zero completion for a scenario without steps", () => {
    expect(completionPercent(US, { id: "empty", steps: [] })).toBe(0);
  });
});
