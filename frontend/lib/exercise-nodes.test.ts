import { describe, it, expect } from "vitest";
import {
  EXERCISE_NODE_MAP,
  getExerciseNodes,
  inferNodeFromDescription,
} from "./exercise-nodes";

describe("EXERCISE_NODE_MAP — workshop lab inventory", () => {
  // Tied to the workshop-deck-aligned 7-lab inventory. If a lab is
  // added/removed, this list updates in lockstep.
  const EXPECTED_LABS = [
    "baseline-assessment",
    "segmentation-requirements",
    "remediation-planning",
    "firewall-implementation",
    "hardening-configurations",
    "vendor-rdp-compromise",
    "validation-evidence",
  ];

  it("has an entry for every shipped lab", () => {
    for (const id of EXPECTED_LABS) {
      expect(EXERCISE_NODE_MAP[id], `missing entry for ${id}`).toBeDefined();
    }
  });

  it("has no entries for removed labs", () => {
    const removed = ["modbus-override", "dnp3-command-injection", "capbank-switching-attack"];
    for (const id of removed) {
      expect(EXERCISE_NODE_MAP[id], `${id} should be gone after restructure`).toBeUndefined();
    }
  });
});

describe("getExerciseNodes", () => {
  it("returns scenario-supplied nodes when present", () => {
    const result = getExerciseNodes("anything", ["alpha", "beta"]);
    expect(result).toEqual(["alpha", "beta"]);
  });

  it("falls back to the EXERCISE_NODE_MAP entry", () => {
    // baseline-assessment uses fw-1 + others
    const result = getExerciseNodes("baseline-assessment");
    expect(result[0]).toBe("fw-1");
    expect(result).toContain("eng-ws-1");
  });

  it("returns empty array for unknown scenario with no info", () => {
    expect(getExerciseNodes("nonexistent")).toEqual([]);
  });

  it("returns empty array for design-only scenarios with no primary", () => {
    // segmentation-requirements + remediation-planning are pure UI,
    // no terminal nodes
    expect(getExerciseNodes("segmentation-requirements")).toEqual([]);
    expect(getExerciseNodes("remediation-planning")).toEqual([]);
  });
});

describe("inferNodeFromDescription", () => {
  const cases: Array<[string, string | null]> = [
    ["from kali run mbpoll", "kali-1"],
    ["From the Kali terminal", "kali-1"],
    ["IP 10.10.10.50 is the attacker", "kali-1"],
    ["from the vendor jump box", "vendor-jump-1"],
    ["use the engineering workstation", "eng-ws-1"],
    ["openplc admin page", "openplc-1"],
    ["RTAC api/state", "rtac-1"],
    ["10.30.30.20 is the rtac", "rtac-1"],
    ["No relevant node mentioned", null],
  ];

  for (const [desc, expected] of cases) {
    it(`infers ${expected ?? "nothing"} from ${JSON.stringify(desc.slice(0, 40))}`, () => {
      expect(inferNodeFromDescription(desc)).toBe(expected);
    });
  }
});
