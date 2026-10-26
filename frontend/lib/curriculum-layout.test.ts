import { afterEach, describe, expect, it } from "vitest";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { readCurriculumSections } from "./curriculum-storage.test-util";

const roots: string[] = [];
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true }); });
function fixture(files: Record<string, string>): string {
  const root = mkdtempSync(resolve(tmpdir(), "rd-curriculum-"));
  roots.push(root);
  mkdirSync(resolve(root, "curriculum"));
  for (const [name, text] of Object.entries(files)) writeFileSync(resolve(root, "curriculum", name), text);
  return root;
}

describe("curriculum section reader", () => {
  it("merges disjoint keys without changing scalar values", () => {
    const root = fixture({ "catalog.yml": "schema: 1\n", "plan.yml": 'plan: "${ip:cc-1}"\n' });
    expect(readCurriculumSections(root, "curriculum")).toEqual({ schema: 1, plan: "${ip:cc-1}" });
  });
  it.each([
    [{}, /Empty/],
    [{ "notes.yml": "schema: 1\n" }, /Unknown/],
    [{ "catalog.yml": "schema: 1\n", "plan.yml": "schema: 1\n" }, /Duplicate/],
    [{ "catalog.yml": "schema: 1\nschema: 1\n" }, /duplicated/],
    [{ "catalog.yml": "schema: 1\n---\nschema: 1\n" }, /single document/],
    [{ "catalog.yml": "null\n" }, /nonempty mapping/],
    [{ "catalog.yml": "[]\n" }, /nonempty mapping/],
  ] as const)("rejects malformed sections %#", (files, error) => {
    expect(() => readCurriculumSections(fixture(files), "curriculum")).toThrow(error);
  });
  it("rejects a non-sibling directory", () => {
    expect(() => readCurriculumSections(fixture({}), "../curriculum")).toThrow(/Invalid/);
  });
});
