// Offline counterpart of the backend's consumer-scoped source resolver.
import { readFileSync, readdirSync, realpathSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { isAbsolute, relative, resolve, sep } from "node:path";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };

export function scenarioFiles(packageDir: string, definitionsRoot = resolve(__dirname, "../../lab-definitions")): string[] {
  const manifest = load(readFileSync(resolve(packageDir, "package.yml"), "utf8")) as { scenarios: unknown };
  if (!Array.isArray(manifest.scenarios) || !manifest.scenarios.length) throw new Error("scenarios must be a nonempty source list");
  const confined = (path: string) => {
    for (const [root, candidate] of [[resolve(definitionsRoot), resolve(path)], [realpathSync(definitionsRoot), realpathSync(path)]]) {
      const rel = relative(root, candidate);
      if (isAbsolute(rel) || rel === ".." || rel.startsWith(`..${sep}`)) throw new Error("scenario path escapes definitions root");
    }
  };
  const files: string[] = [], owners = new Set<string>();
  for (const source of manifest.scenarios) {
    if (!source || typeof source !== "object" || Array.isArray(source)) throw new Error("invalid scenario source");
    const { dir, include } = source as { dir?: unknown; include?: unknown };
    if (Object.keys(source).some((key) => key !== "dir" && key !== "include")) throw new Error("unknown scenario source field");
    if (typeof dir !== "string" || !dir.trim() || isAbsolute(dir) || dir.includes("\\") || /^[A-Za-z]:/.test(dir)) throw new Error("invalid relative scenario directory");
    const selected = Object.hasOwn(source, "include");
    if (selected && (!Array.isArray(include) || !include.length || include.some((id) => typeof id !== "string" || !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(id)) || new Set(include).size !== include.length)) throw new Error("invalid scenario include list");
    const directory = resolve(packageDir, dir);
    confined(directory);
    const found = new Set<string>();
    for (const name of readdirSync(directory).filter((name) => name.endsWith(".yml")).sort()) {
      const path = resolve(directory, name);
      confined(path);
      if (!statSync(path).isFile()) throw new Error("scenario must be a regular file");
      const doc = load(readFileSync(path, "utf8")) as { id: string };
      if (selected && !(include as string[]).includes(doc.id)) continue;
      if (owners.has(doc.id)) throw new Error(`duplicate scenario id ${doc.id}`);
      owners.add(doc.id);
      found.add(doc.id);
      files.push(path);
    }
    if (selected && (include as string[]).some((id) => !found.has(id))) throw new Error("included scenario must resolve exactly once");
  }
  return files;
}
