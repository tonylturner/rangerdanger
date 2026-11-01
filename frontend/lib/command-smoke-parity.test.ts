import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scenario } from "./api";
import { fixtureCurriculum } from "./curriculum-storage.test-util";
import { getExerciseNodes, inferNodeFromDescription } from "./exercise-nodes";
import { splitDescription, type Segment } from "./scenario-description";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
const root = resolve(__dirname, "../..");
type Contract = { container: string; prompt: string; action: string; reason?: string };
const commands = (text: string, inherited?: string): Extract<Segment, { type: "cmd" }>[] =>
  splitDescription(text, inherited).flatMap((seg) => {
    if (seg.type === "cmd") return [seg];
    if (seg.type === "hint") return commands(seg.value, seg.sourceNode);
    if (seg.type === "trackOnly") return commands(seg.body, seg.sourceNode);
    return [];
  });

describe("reviewed smoke contracts match UI dispatch", () => {
  for (const id of readdirSync(resolve(root, "lab-definitions/packages"))) {
    it(`checks every command and all three dispatch classes in ${id}`, () => {
      const base = resolve(root, "lab-definitions/packages", id);
      const pkg = load(readFileSync(resolve(base, "package.yml"), "utf8")) as { scenarios: string };
      const manifest = JSON.parse(readFileSync(resolve(base, "manifest.json"), "utf8")) as {
        services: { node?: string; container: string }[];
      };
      const nodes = Object.fromEntries(manifest.services.filter((s) => s.node).map((s) => [s.node, s.container]));
      const suffix = id === "us-dnp3-substation" ? "" : `-${id}`;
      const contracts = (JSON.parse(readFileSync(resolve(root, `scripts/lab-commands-expectations${suffix}.json`), "utf8")) as {
        commands: Record<string, Contract>;
      }).commands;
      const binding = fixtureCurriculum(`lab-definitions/packages/${id}`);
      const seen = new Set<string>();
      for (const file of readdirSync(resolve(base, pkg.scenarios)).filter((f) => f.endsWith(".yml"))) {
        const scenario = load(readFileSync(resolve(base, pkg.scenarios, file), "utf8")) as Scenario;
        scenario.steps.forEach((step, index) => {
          const fallback = step.node || inferNodeFromDescription(step.description, binding)
            || binding.nodeMap[scenario.id]?.primary || getExerciseNodes(scenario.id, scenario.nodes, binding)[0];
          const occurrences: Record<string, number> = {};
          for (const cmd of commands(step.description)) {
            const value = cmd.value.replace(/\\\n\s*/g, " ");
            occurrences[value] = (occurrences[value] ?? 0) + 1;
            const key = `${scenario.id}|${index + 1}|${value}|${occurrences[value]}`;
            const contract = contracts[key];
            expect(contract, key).toBeDefined();
            seen.add(key);
            const source = cmd.sourceNode || fallback;
            if (cmd.cli) {
              expect(contract.container, key).toBe(nodes["fw-1"]);
              expect(contract.prompt, key).toBe("cli");
            } else if (source === "host" || cmd.copyOnly) {
              expect(contract.action, key).toBe("SKIP");
              expect(contract.reason, key).toBeTruthy();
            } else if (contract.action === "RUN") {
              expect(contract.container, key).toBe(nodes[source]);
              expect(contract.prompt, key).toBe("shell");
            }
          }
        });
      }
      expect([...seen].sort()).toEqual(Object.keys(contracts).sort());
    });
  }
});
