import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearProgress, completionPercent, loadProgress, loadValidation, saveProgress, saveValidation } from "./scenario-runner-storage";
import type { ValidationResult } from "./api";
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
      skippedStepIds: [], completedStepIds: ["capture-traffic", "review-topology"],
      notes: "enterprise reaches field",
      cmdLog: ["[OK] done"],
    });
    const stored = JSON.parse(storage.getItem("rd:us-dnp3-substation:1:exercise:baseline-assessment") ?? "null");
    expect(stored).toEqual({
      skippedStepIds: [], completedStepIds: ["capture-traffic", "review-topology"],
      notes: "enterprise reaches field",
      cmdLog: ["[OK] done"],
    });
    expect(loadProgress(US, scenario).completedStepIds).toEqual(["capture-traffic", "review-topology"]);
    expect(completionPercent(US, scenario)).toBe(50);
  });

  it("keeps progress on the same steps when steps are reordered or inserted", () => {
    saveProgress(US, scenario.id, { skippedStepIds: [], completedStepIds: ["record-findings"], notes: "", cmdLog: [] });
    const reordered = {
      id: scenario.id,
      steps: [{ id: "new-intro", title: "Intro", description: "" }, ...[...scenario.steps].reverse()],
    };
    expect(loadProgress(US, reordered).completedStepIds).toEqual(["record-findings"]);
  });

  it("drops step ids the scenario no longer has and duplicates", () => {
    saveProgress(US, scenario.id, {
      skippedStepIds: [], completedStepIds: ["summarise", "removed-step", "summarise"],
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
    expect(loadProgress(US, scenario)).toEqual({ skippedStepIds: [], completedStepIds: [], notes: "", cmdLog: [] });
    expect(completionPercent(US, scenario)).toBe(0);
  });

  it("ignores index-based progress even under the scoped key", () => {
    storage.setItem(
      "rd:us-dnp3-substation:1:exercise:baseline-assessment",
      JSON.stringify({ completedSteps: [0, 1], skippedStepIds: [], completedStepIds: [0, 1], notes: "kept", cmdLog: [] }),
    );
    expect(loadProgress(US, scenario)).toEqual({ skippedStepIds: [], completedStepIds: [], notes: "kept", cmdLog: [] });
  });

  it("returns empty progress for corrupt records and another revision", () => {
    storage.setItem("rd:us-dnp3-substation:1:exercise:baseline-assessment", "{not json");
    expect(loadProgress(US, scenario).completedStepIds).toEqual([]);
    saveProgress(US, scenario.id, { skippedStepIds: [], completedStepIds: ["summarise"], notes: "", cmdLog: [] });
    expect(loadProgress({ ...US, revision: 2 }, scenario).completedStepIds).toEqual([]);
  });

  it("clears only this exercise", () => {
    saveProgress(US, scenario.id, { skippedStepIds: [], completedStepIds: ["summarise"], notes: "", cmdLog: [] });
    saveProgress(US, "other-lab", { skippedStepIds: [], completedStepIds: ["x"], notes: "", cmdLog: [] });
    clearProgress(US, scenario.id);
    expect(storage.getItem("rd:us-dnp3-substation:1:exercise:baseline-assessment")).toBeNull();
    expect(storage.getItem("rd:us-dnp3-substation:1:exercise:other-lab")).not.toBeNull();
  });

  it("reports zero completion for a scenario without steps", () => {
    expect(completionPercent(US, { id: "empty", steps: [] })).toBe(0);
  });

  const outcome = (value: "PASS" | "FAIL"): ValidationResult => ({
    scenario_id: scenario.id, outcome: value, timestamp: "2026-11-01T12:00:00Z",
    checks: [{ name: "Current state", status: value === "PASS" ? "pass" : "fail", detail: "Scoped observation" }],
  });
  const progress = { completedStepIds: ["review-topology"], skippedStepIds: ["capture-traffic"], notes: "student notes", cmdLog: [] };
  const EU = { ...US, packageId: "eu-iec104-substation" };

  it("refresh reloads outcome and completed/skipped progress from separate records", () => {
    saveProgress(US, scenario.id, progress);
    saveValidation(US, outcome("PASS"));
    vi.stubGlobal("window", { localStorage: storage });
    expect(loadProgress(US, scenario)).toEqual(progress);
    expect(loadValidation(US, scenario.id)).toEqual(outcome("PASS"));
    expect(completionPercent(US, scenario)).toBe(25);
  });

  it("reset command-log persistence does not replace progress or the historical outcome", () => {
    saveProgress(US, scenario.id, progress);
    saveValidation(US, outcome("FAIL"));
    saveProgress(US, scenario.id, { ...loadProgress(US, scenario), cmdLog: ["[RESET] Lab restored"] });
    expect(loadProgress(US, scenario)).toEqual({ ...progress, cmdLog: ["[RESET] Lab restored"] });
    expect(loadValidation(US, scenario.id)).toEqual(outcome("FAIL"));
    clearProgress(US, scenario.id);
    expect(loadProgress(US, scenario).completedStepIds).toEqual([]);
    expect(loadProgress(US, scenario).skippedStepIds).toEqual([]);
    expect(loadValidation(US, scenario.id)).toEqual(outcome("FAIL"));
  });

  it("package switches isolate both outcomes and student progress", () => {
    saveProgress(US, scenario.id, progress);
    saveValidation(US, outcome("PASS"));
    expect(loadValidation(EU, scenario.id)).toBeNull();
    expect(loadProgress(EU, scenario).completedStepIds).toEqual([]);
    const euProgress = { ...progress, completedStepIds: ["summarise"], skippedStepIds: ["record-findings"] };
    saveProgress(EU, scenario.id, euProgress);
    saveValidation(EU, outcome("FAIL"));
    expect(loadProgress(US, scenario)).toEqual(progress);
    expect(loadValidation(US, scenario.id)).toEqual(outcome("PASS"));
    expect(loadProgress(EU, scenario)).toEqual(euProgress);
    expect(loadValidation(EU, scenario.id)).toEqual(outcome("FAIL"));
    expect(loadValidation({ ...US, revision: US.revision + 1 }, scenario.id)).toBeNull();
    expect(loadValidation(US, "other-exercise")).toBeNull();
  });

  it("a second validation replaces only the outcome, in either direction", () => {
    saveProgress(US, scenario.id, progress);
    for (const result of [outcome("PASS"), outcome("FAIL"), outcome("PASS")]) {
      saveValidation(US, result);
      expect(loadValidation(US, scenario.id)).toEqual(result);
      expect(loadProgress(US, scenario)).toEqual(progress);
    }
  });

  it("drops invalid/duplicate skipped ids and never counts a skip as complete", () => {
    saveProgress(US, scenario.id, { ...progress, skippedStepIds: ["review-topology", "capture-traffic", "capture-traffic", "gone"] });
    expect(loadProgress(US, scenario).skippedStepIds).toEqual(["capture-traffic"]);
    expect(completionPercent(US, scenario)).toBe(25);
  });

  it.each([
    "{bad", "null", JSON.stringify({ ...outcome("PASS"), outcome: { toString: null } }),
    JSON.stringify({ ...outcome("PASS"), scenario_id: "other" }),
    JSON.stringify({ ...outcome("PASS"), timestamp: "invalid" }),
    JSON.stringify({ ...outcome("PASS"), checks: [{ status: "pass" }] }),
  ])("ignores corrupt persisted validation %s", (raw) => {
    storage.setItem("rd:us-dnp3-substation:1:validation:baseline-assessment", raw);
    expect(loadValidation(US, scenario.id)).toBeNull();
  });
});
