import { afterEach, expect, it } from "vitest";
import { mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { scenarioFiles } from "./scenario-sources.test-util";

const roots: string[] = [];
afterEach(() => roots.splice(0).forEach((root) => rmSync(root, { recursive: true })));
function fixture(sources: unknown) {
  const root = mkdtempSync(resolve(tmpdir(), "rd-sources-"));
  roots.push(root);
  const pkg = resolve(root, "packages/alpha");
  mkdirSync(pkg, { recursive: true });
  mkdirSync(resolve(root, "shared"));
  writeFileSync(resolve(root, "shared/not-the-id.yml"), "id: lesson\nsteps: []\n");
  writeFileSync(resolve(pkg, "package.yml"), JSON.stringify({ scenarios: sources }));
  return { root, pkg };
}

it("selects authored IDs independently in each consumer", () => {
  const { root, pkg } = fixture([{ dir: "../../shared", include: ["lesson"] }]);
  const other = resolve(root, "packages/beta");
  mkdirSync(other);
  writeFileSync(resolve(other, "package.yml"), JSON.stringify({ scenarios: [{ dir: "../../shared" }] }));
  expect(scenarioFiles(pkg, root)).toEqual([resolve(root, "shared/not-the-id.yml")]);
  expect(scenarioFiles(other, root)).toEqual(scenarioFiles(pkg, root));
});

it.each([
  "scenarios", [], [{ dir: "" }], [{ dir: "/tmp" }], [{ dir: "..\\shared" }],
  [{ dir: "../../../outside" }], [{ dir: "../../shared", include: [] }],
  [{ dir: "../../shared", include: null }], [{ dir: "../../shared", include: ["missing"] }],
  [{ dir: "../../shared", include: ["not-the-id"] }],
  [{ dir: "../../shared", include: ["lesson", "lesson"] }],
  [{ dir: "../../shared" }, { dir: "../../shared" }],
].map((sources) => [sources]))("rejects invalid scenario sources %#", (sources) => {
  const { root, pkg } = fixture(sources);
  expect(() => scenarioFiles(pkg, root)).toThrow();
});

it.each([false, true])("confines symlink targets (file=%s)", (fileLink) => {
  const { root, pkg } = fixture([{ dir: fileLink ? "../../shared" : "linked" }]);
  const outside = mkdtempSync(resolve(tmpdir(), "rd-outside-"));
  roots.push(outside);
  writeFileSync(resolve(outside, "outside.yml"), "id: outside\n");
  symlinkSync(fileLink ? resolve(outside, "outside.yml") : outside,
    fileLink ? resolve(root, "shared/link.yml") : resolve(pkg, "linked"));
  expect(() => scenarioFiles(pkg, root)).toThrow(/escapes/);
});
