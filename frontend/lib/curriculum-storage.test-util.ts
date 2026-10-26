// Test-only equivalent of the loader's scalar topology resolution. Production
// clients receive the resolved binding through GET /api/packages.
import { lstatSync, readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import type { CurriculumBinding } from "./api";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
const sectionFiles = new Set([
  "catalog.yml", "requirements.yml", "readiness.yml", "coverage.yml",
  "implementation.yml", "nodes.yml", "plan.yml",
]);

export function readCurriculumSections(packageDir: string, directory: string): Record<string, unknown> {
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(directory)) throw new Error("Invalid curriculum directory");
  const dir = resolve(packageDir, directory);
  if (!lstatSync(dir).isDirectory()) throw new Error("Curriculum must be a directory");
  const files = readdirSync(dir, { withFileTypes: true });
  if (!files.length) throw new Error("Empty curriculum directory");
  const merged: Record<string, unknown> = {};
  for (const file of files) {
    if (!sectionFiles.has(file.name) || !file.isFile()) throw new Error(`Unknown curriculum file ${file.name}`);
    const section = load(readFileSync(resolve(dir, file.name), "utf8"));
    if (!section || typeof section !== "object" || Array.isArray(section) || !Object.keys(section).length) {
      throw new Error(`Curriculum file ${file.name} must contain a nonempty mapping`);
    }
    for (const [key, value] of Object.entries(section)) {
      if (Object.hasOwn(merged, key)) throw new Error(`Duplicate curriculum key ${key}`);
      merged[key] = value;
    }
  }
  return merged;
}

export function fixtureCurriculum(packageDir: string, topologyFile?: string): CurriculumBinding {
  const root = resolve(__dirname, "../..");
  const dir = resolve(root, packageDir);
  const manifest = load(readFileSync(resolve(dir, "package.yml"), "utf8")) as { topology: string; curriculum: { dir: string }; ics_rules?: "allowed" | "forbidden" };
  const topology = load(readFileSync(topologyFile ?? resolve(dir, manifest.topology), "utf8")) as {
    nodes: { id: string; ip: string }[];
    networks: { id: string; zone: string; subnet: string; name: string }[];
  };
  const facts: Record<string, string> = {};
  for (const n of topology.nodes) facts[`ip:${n.id}`] = n.ip;
  for (const n of topology.networks) { facts[`subnet:${n.id}`] = n.subnet; facts[`zone:${n.id}`] = n.zone; }
  const resolveScalars = (value: unknown): unknown => {
    if (typeof value === "string") return value.replace(/\$\{([^}]+)\}/g, (_, ref: string) => {
      if (!facts[ref]) throw new Error(`Missing topology reference ${ref}`);
      return facts[ref];
    });
    if (Array.isArray(value)) return value.map(resolveScalars);
    if (value !== null && typeof value === "object") return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [key, resolveScalars(child)]),
    );
    return value;
  };
  const curriculum = resolveScalars(readCurriculumSections(dir, manifest.curriculum.dir));
  return {
    ...curriculum as CurriculumBinding,
    ...manifest.curriculum,
    icsRules: manifest.ics_rules ?? "allowed",
  };
}
export const US_CURRICULUM = fixtureCurriculum("lab-definitions/packages/us-dnp3-substation");
