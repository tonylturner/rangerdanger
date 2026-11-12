import { afterEach, expect, it } from "vitest";
import { mkdtempSync, mkdirSync, renameSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { scenarioFiles } from "./scenario-sources.test-util";

const roots: string[] = [];
afterEach(() => roots.splice(0).forEach((root) => rmSync(root, { recursive: true })));
function fixture(directory: unknown) {
  const root = mkdtempSync(resolve(tmpdir(), "rd-sources-"));
  roots.push(root);
  const pkg = resolve(root, "packages/alpha");
  mkdirSync(pkg, { recursive: true });
  mkdirSync(resolve(pkg, "scenarios"));
  writeFileSync(resolve(pkg, "scenarios/not-the-id.yml"), "id: lesson\nsteps: []\n");
  writeFileSync(resolve(pkg, "package.yml"), JSON.stringify({ scenarios: directory }));
  return { root, pkg };
}

it("loads independent package directories with the same authored ID", () => {
  const { root, pkg } = fixture("scenarios");
  const other = resolve(root, "packages/beta");
  mkdirSync(resolve(other, "scenarios"), { recursive: true });
  writeFileSync(resolve(other, "package.yml"), JSON.stringify({ scenarios: "scenarios" }));
  writeFileSync(resolve(other, "scenarios/lesson.yml"), "id: lesson\nsteps: []\n");
  expect(scenarioFiles(pkg, root)).toEqual([resolve(pkg, "scenarios/not-the-id.yml")]);
  expect(scenarioFiles(other, root)).toEqual([resolve(other, "scenarios/lesson.yml")]);
});

it.each([
  [], [{ dir: "scenarios" }], { dir: "scenarios" }, null, "", " ", "/tmp", "C:/temp",
  "..\\scenarios", "../../../outside", "scenarios/not-the-id.yml",
].map((directory) => [directory]))("rejects invalid scenario directories %#", (directory) => {
  const { root, pkg } = fixture(directory);
  expect(() => scenarioFiles(pkg, root)).toThrow();
});

it.each([false, true])("rejects duplicate IDs and non-regular files (duplicate=%s)", (duplicate) => {
  const { root, pkg } = fixture("scenarios");
  const path = resolve(pkg, "scenarios/other.yml");
  if (duplicate) writeFileSync(path, "id: lesson\n");
  else mkdirSync(path);
  expect(() => scenarioFiles(pkg, root)).toThrow(/duplicate|regular/);
});

it("allows confined symlinks and literal directory names", () => {
  const { root, pkg } = fixture("linked");
  renameSync(resolve(pkg, "scenarios"), resolve(pkg, "lessons[1]"));
  renameSync(resolve(pkg, "lessons[1]/not-the-id.yml"), resolve(pkg, "lesson.yml"));
  symlinkSync("../lesson.yml", resolve(pkg, "lessons[1]/not-the-id.yml"));
  symlinkSync("lessons[1]", resolve(pkg, "linked"));
  expect(scenarioFiles(pkg, root)).toEqual([resolve(pkg, "linked/not-the-id.yml")]);
});

it.each([false, true])("confines symlink targets (file=%s)", (fileLink) => {
  const { root, pkg } = fixture(fileLink ? "scenarios" : "linked");
  const outside = mkdtempSync(resolve(tmpdir(), "rd-outside-"));
  roots.push(outside);
  writeFileSync(resolve(outside, "outside.yml"), "id: outside\n");
  symlinkSync(fileLink ? resolve(outside, "outside.yml") : outside,
    fileLink ? resolve(pkg, "scenarios/link.yml") : resolve(pkg, "linked"));
  expect(() => scenarioFiles(pkg, root)).toThrow(/escapes/);
});
