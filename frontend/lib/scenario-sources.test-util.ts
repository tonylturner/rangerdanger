// Offline counterpart of the backend's confined scenario-directory resolver.
import { readFileSync, readdirSync, realpathSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { isAbsolute, relative, resolve, sep } from "node:path";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };

export function scenarioFiles(packageDir: string, definitionsRoot = resolve(__dirname, "../../lab-definitions")): string[] {
  const manifest = load(readFileSync(resolve(packageDir, "package.yml"), "utf8")) as { scenarios: unknown };
  const dir = manifest.scenarios;
  if (typeof dir !== "string" || !dir.trim() || isAbsolute(dir) || dir.includes("\\") || /^[A-Za-z]:/.test(dir)) throw new Error("invalid relative scenario directory");
  const confined = (path: string) => {
    for (const [root, candidate] of [[resolve(definitionsRoot), resolve(path)], [realpathSync(definitionsRoot), realpathSync(path)]]) {
      const rel = relative(root, candidate);
      if (isAbsolute(rel) || rel === ".." || rel.startsWith(`..${sep}`)) throw new Error("scenario path escapes definitions root");
    }
  };
  const directory = resolve(packageDir, dir);
  confined(directory);
  const files: string[] = [], owners = new Set<string>();
  for (const name of readdirSync(directory).filter((name) => name.endsWith(".yml")).sort()) {
    const path = resolve(directory, name);
    confined(path);
    if (!statSync(path).isFile()) throw new Error("scenario must be a regular file");
    const doc = load(readFileSync(path, "utf8")) as { id: string };
    if (owners.has(doc.id)) throw new Error(`duplicate scenario id ${doc.id}`);
    owners.add(doc.id);
    files.push(path);
  }
  return files;
}
