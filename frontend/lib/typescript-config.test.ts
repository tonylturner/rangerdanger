import { resolve } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";

const root = resolve(__dirname, "..");

function readConfig(name: string): ts.ParsedCommandLine {
  const path = resolve(root, name);
  const { config, error } = ts.readConfigFile(path, ts.sys.readFile);
  expect(error).toBeUndefined();
  const parsed = ts.parseJsonConfigFileContent(config, ts.sys, root);
  expect(parsed.errors).toEqual([]);
  return parsed;
}

describe("TypeScript project boundaries", () => {
  it("keeps tests and their helpers in the full editor/typecheck project", () => {
    const full = readConfig("tsconfig.json");
    for (const file of [
      "components/traffic-matrix-view.test.tsx",
      "lib/network-console-data.test.ts",
      "lib/network-graph.test.ts",
      "lib/observed-flows.test.ts",
      "lib/curriculum-storage.test-util.ts",
      "lib/typescript-config.test.ts",
    ]) {
      expect(full.fileNames).toContain(resolve(root, file));
    }
  });

  it("excludes only tests and helpers from the production build project", () => {
    const full = readConfig("tsconfig.json");
    const build = readConfig("tsconfig.build.json");
    const production = full.fileNames.filter(
      (file) => !/\.(?:test|test-util)\.tsx?$/.test(file),
    );
    expect(production).toContain(resolve(root, "app/page.tsx"));
    expect(build.fileNames.toSorted()).toEqual(production.toSorted());
    expect(build.options).toEqual(full.options);
  });
});
