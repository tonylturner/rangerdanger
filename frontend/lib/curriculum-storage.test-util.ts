// Test-only equivalent of the loader's scalar topology resolution. Production
// clients receive the resolved binding through GET /api/packages.
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import type { CurriculumBinding } from "./api";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
export function fixtureCurriculum(packageDir: string, topologyFile?: string): CurriculumBinding {
  const root = resolve(__dirname, "../..");
  const dir = resolve(root, packageDir);
  const manifest = load(readFileSync(resolve(dir, "package.yml"), "utf8")) as { topology: string; curriculum: object; ics_rules?: "allowed" | "forbidden" };
  const topology = load(readFileSync(topologyFile ?? resolve(dir, manifest.topology), "utf8")) as {
    nodes: { id: string; ip: string }[];
    networks: { id: string; zone: string; subnet: string; name: string }[];
  };
  const facts: Record<string, string> = {};
  for (const n of topology.nodes) facts[`ip:${n.id}`] = n.ip;
  for (const n of topology.networks) { facts[`subnet:${n.id}`] = n.subnet; facts[`zone:${n.id}`] = n.zone; }
  const raw = readFileSync(resolve(dir, "curriculum.yml"), "utf8").replace(/\$\{([^}]+)\}/g, (_, ref: string) => {
    if (!facts[ref]) throw new Error(`Missing topology reference ${ref}`);
    return facts[ref];
  });
  return {
    ...load(raw) as CurriculumBinding,
    ...manifest.curriculum,
    icsRules: manifest.ics_rules ?? "allowed",
  };
}
export const US_CURRICULUM = fixtureCurriculum("lab-definitions/packages/us-dnp3-substation");
