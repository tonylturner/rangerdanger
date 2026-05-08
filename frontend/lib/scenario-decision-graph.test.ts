// Contract test for the lab decision-flow graph.
//
// What this catches: silent breakage when someone renames a
// decision id in one YAML and forgets to update the matching
// references in downstream labs. The flow is:
//
//   YAML A: `:::decision id=enterprise-to-field`
//                   defines `enterprise-to-field` for scenario A
//   YAML B: `:::findings-panel from=A`
//             - enterprise-to-field: <label>
//                   reads decision:A:enterprise-to-field at runtime
//   YAML C: `:::decision id=enterprise-to-field default-from=A:enterprise-to-field`
//                   inherits decision:A:enterprise-to-field if its
//                   own value is empty
//
// All three references — id, findings-panel item, default-from —
// must agree on the kebab-case id and the scenario id. A stray
// underscore or typo silently breaks downstream rendering with no
// error. This test loads every scenario YAML, parses all three
// fence types, and asserts every reference resolves.
//
// Loaded via plain fs + regex (not pyyaml) to avoid an extra
// devDep — the fences are line-anchored so regex is robust enough.

import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";

const SCENARIOS_DIR = resolve(__dirname, "..", "..", "lab-definitions", "scenarios");

type ScenarioFile = {
  file: string;
  id: string;
  raw: string;
  /** decision ids defined in this scenario via `:::decision id=...` */
  defines: Set<string>;
  /** findings-panel references this scenario makes */
  findingsRefs: Array<{ sourceScenario: string; itemId: string }>;
  /** default-from references this scenario makes */
  defaultFromRefs: Array<{ sourceScenario: string; sourceId: string }>;
};

function readScenarios(): ScenarioFile[] {
  if (!existsSync(SCENARIOS_DIR)) return [];
  const out: ScenarioFile[] = [];
  for (const file of readdirSync(SCENARIOS_DIR)) {
    if (!file.endsWith(".yml") && !file.endsWith(".yaml")) continue;
    const raw = readFileSync(join(SCENARIOS_DIR, file), "utf8");

    // YAML id field — anchored to start of line, single-quoted /
    // double-quoted / unquoted all accepted.
    const idMatch = raw.match(/^id:\s*['"]?([^'"\n]+?)['"]?\s*$/m);
    const id = idMatch?.[1]?.trim() ?? "";

    const defines = new Set<string>();
    const findingsRefs: ScenarioFile["findingsRefs"] = [];
    const defaultFromRefs: ScenarioFile["defaultFromRefs"] = [];

    // Walk fence-opening lines. Mirrors the shape splitDescription
    // recognises (see frontend/lib/scenario-description.ts).
    const lines = raw.split("\n");
    let inFindingsPanel = false;
    let findingsSource = "";

    for (const line of lines) {
      const trimmed = line.trim();

      // ::: closes any block
      if (trimmed === ":::" || trimmed.startsWith(":::end")) {
        inFindingsPanel = false;
        findingsSource = "";
        continue;
      }

      // :::decision id=<X> [default-from=<Y>:<Z>]
      const decisionMatch = trimmed.match(
        /^:::decision\b(.*)$/
      );
      if (decisionMatch) {
        const attrs = decisionMatch[1] ?? "";
        const idM = attrs.match(/\bid=([A-Za-z0-9_-]+)/);
        if (idM) defines.add(idM[1]);
        const defaultFromM = attrs.match(/\bdefault-from=([A-Za-z0-9_-]+):([A-Za-z0-9_-]+)/);
        if (defaultFromM) {
          defaultFromRefs.push({
            sourceScenario: defaultFromM[1],
            sourceId: defaultFromM[2],
          });
        }
        continue;
      }

      // :::findings-panel from=<scenarioId>
      const findingsOpen = trimmed.match(/^:::findings-panel\b(.*)$/);
      if (findingsOpen) {
        const attrs = findingsOpen[1] ?? "";
        const fromM = attrs.match(/\bfrom=([A-Za-z0-9_-]+)/);
        if (fromM) {
          inFindingsPanel = true;
          findingsSource = fromM[1];
        }
        continue;
      }

      // Inside a findings-panel: every "id: label" row is a reference.
      // Author convention is two-space indent + dash, but we only
      // need to match the id token before the colon.
      if (inFindingsPanel) {
        const itemM = trimmed.match(/^([A-Za-z0-9_-]+):\s*\S/);
        if (itemM && findingsSource) {
          findingsRefs.push({
            sourceScenario: findingsSource,
            itemId: itemM[1],
          });
        }
      }
    }

    out.push({ file, id, raw, defines, findingsRefs, defaultFromRefs });
  }
  return out;
}

const SCENARIOS = readScenarios();
const BY_ID = new Map(SCENARIOS.map((s) => [s.id, s]));

describe("scenario decision-graph contract", () => {
  it("loads at least the seven workshop scenarios", () => {
    // Sanity guard — if the directory layout moves, this test
    // would silently pass with zero scenarios. Pin the floor.
    expect(SCENARIOS.length).toBeGreaterThanOrEqual(7);
  });

  it("every scenario file declares an id at the top", () => {
    for (const s of SCENARIOS) {
      expect(s.id, `${s.file}: missing 'id:' field`).not.toBe("");
    }
  });

  it("every findings-panel reference resolves to a real upstream decision", () => {
    const orphans: string[] = [];
    for (const s of SCENARIOS) {
      for (const ref of s.findingsRefs) {
        const upstream = BY_ID.get(ref.sourceScenario);
        if (!upstream) {
          orphans.push(`${s.file}: findings-panel from=${ref.sourceScenario} — no scenario with that id`);
          continue;
        }
        if (!upstream.defines.has(ref.itemId)) {
          orphans.push(
            `${s.file}: findings-panel item "${ref.itemId}" not defined in ${ref.sourceScenario} ` +
              `(${upstream.file} defines: ${Array.from(upstream.defines).sort().join(", ") || "<none>"})`
          );
        }
      }
    }
    expect(orphans, orphans.join("\n")).toEqual([]);
  });

  it("every default-from reference resolves to a real upstream decision", () => {
    const orphans: string[] = [];
    for (const s of SCENARIOS) {
      for (const ref of s.defaultFromRefs) {
        const upstream = BY_ID.get(ref.sourceScenario);
        if (!upstream) {
          orphans.push(`${s.file}: default-from=${ref.sourceScenario}:${ref.sourceId} — no upstream scenario`);
          continue;
        }
        if (!upstream.defines.has(ref.sourceId)) {
          orphans.push(
            `${s.file}: default-from=${ref.sourceScenario}:${ref.sourceId} — upstream doesn't define that id ` +
              `(${upstream.file} defines: ${Array.from(upstream.defines).sort().join(", ") || "<none>"})`
          );
        }
      }
    }
    expect(orphans, orphans.join("\n")).toEqual([]);
  });

  it("decision ids inside one scenario are unique", () => {
    // The graph builder uses a Set, so duplicate definitions in
    // raw YAML would dedupe silently and the second definition's
    // attributes (correct, options) would be lost. Walk the raw
    // file once more to catch repeats.
    const dups: string[] = [];
    for (const s of SCENARIOS) {
      const seen = new Map<string, number>();
      for (const m of s.raw.matchAll(/^:::decision\b[^\n]*\bid=([A-Za-z0-9_-]+)/gm)) {
        seen.set(m[1], (seen.get(m[1]) ?? 0) + 1);
      }
      for (const [id, n] of seen) {
        if (n > 1) {
          dups.push(`${s.file}: decision id "${id}" defined ${n} times`);
        }
      }
    }
    expect(dups, dups.join("\n")).toEqual([]);
  });
});
