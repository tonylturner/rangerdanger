// Test-only equivalent of the loader's scalar topology resolution. Production
// clients receive the resolved binding through GET /api/packages.
import { lstatSync, readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { scenarioFiles } from "./scenario-sources.test-util";
import type { Course, CurriculumBinding, Scenario, SegmentationPlan } from "./api";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
const sectionFiles = new Set([
  "catalog.yml", "requirements.yml", "readiness.yml", "coverage.yml",
  "implementation.yml", "plan.yml",
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

export function fixtureCourse(packageDir: string, topologyFile?: string, definitionsRoot?: string): Course {
  const root = resolve(__dirname, "../..");
  const dir = resolve(root, packageDir);
  const manifest = load(readFileSync(resolve(dir, "package.yml"), "utf8")) as { topology: string; course: Omit<Course, "features"> & { nodes: string; features?: { segmentationPlan?: Omit<SegmentationPlan, "model"> & { dir: string } } }; ics_rules?: CurriculumBinding["icsRules"] };
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
  const routing = resolveScalars(load(readFileSync(resolve(dir, manifest.course.nodes), "utf8"))) as Pick<Course, "nodeMap" | "nodeInference">;
  const course: Course = { modules: manifest.course.modules, ...routing, features: {} };
  const feature = manifest.course.features?.segmentationPlan;
  if (!feature) return course;
  const curriculum = resolveScalars(readCurriculumSections(dir, feature.dir)) as CurriculumBinding;
  course.features.segmentationPlan = { activities: feature.activities, dynamicSteps: feature.dynamicSteps, model: curriculum };
  curriculum.icsRules = manifest.ics_rules ?? "allowed";
  for (const [phase, template] of Object.entries(curriculum.phases ?? {})) {
    if (!template.includes("{{step:")) continue;
    const documents = scenarioFiles(dir, definitionsRoot ? resolve(__dirname, "../..", definitionsRoot) : undefined)
      .map((file) => load(readFileSync(file, "utf8")) as {
        id: string; steps: { id: string; description: string }[];
      });
    const steps = new Map(documents.filter((doc) => doc.id === feature.activities.implementation)
      .flatMap((doc) => doc.steps.map((step) => [step.id, step.description.trim()] as const)));
    const text = template.replace(/\{\{step:([a-z0-9-]+)\}\}/g, (_, id: string) => {
      const body = steps.get(id);
      if (feature.dynamicSteps[id] !== phase || !body || body.includes("{{step:")) {
        throw new Error(`Invalid implementation step reference ${id}`);
      }
      return body;
    });
    if (text.includes("{{step:")) throw new Error("Invalid implementation step reference");
    curriculum.phases[phase] = text;
  }
  return course;
}

export function fixtureCurriculum(packageDir: string, topologyFile?: string, definitionsRoot?: string): CurriculumBinding {
  const model = fixtureCourse(packageDir, topologyFile, definitionsRoot).features.segmentationPlan?.model;
  if (!model) throw new Error("Fixture has no segmentation plan");
  return model;
}

// Authored-source discovery remains separate from this rendered course projection.
export function fixtureScenarios(packageDir: string, definitionsRoot?: string): Scenario[] {
  const dir = resolve(__dirname, "../..", packageDir);
  const course = fixtureCourse(packageDir, undefined, definitionsRoot);
  const manifest = load(readFileSync(resolve(dir, "package.yml"), "utf8")) as { id: string; topology: string };
  const topology = load(readFileSync(resolve(dir, manifest.topology), "utf8")) as { id: string };
  const documents = new Map(scenarioFiles(dir, definitionsRoot ? resolve(__dirname, "../..", definitionsRoot) : undefined).map((file) => {
    const doc = load(readFileSync(file, "utf8")) as Scenario;
    return [doc.id, doc];
  }));
  return course.modules.flatMap((module) => module.activities).map((activity) => {
    const doc = documents.get(activity.scenario);
    if (!doc) throw new Error(`Missing scenario ${activity.scenario}`);
    return { ...doc, package_id: manifest.id, lab_template_id: topology.id, validator: doc.validator ?? "", tags: doc.tags ?? [], label: activity.label };
  });
}
export const US_COURSE = fixtureCourse("lab-definitions/packages/us-dnp3-substation");
export const US_CURRICULUM = US_COURSE.features.segmentationPlan!.model;
