// Contract test: Lab 2.2 (firewall-implementation) content stays in
// sync with the hardened reference config it teaches against
// (lab-definitions/firewall/substation-improved.json).
//
// What this catches: the Phase 3 rule example / required-rules table
// drifting from the real shipped policy — e.g. teaching an
// un-source-pinned or un-DPI'd RTAC rule when the hardened config
// actually pins to 10.30.30.20/32 with Modbus function-code DPI. That
// exact drift shipped once: the JSON example omitted both `sources`
// and a populated `ics`, so an Advanced-track student copying it built
// the broad lan1->lan2 rule the lab warns against. This locks it down.
//
// Loaded via plain fs (config is JSON; lab body matched as text) so no
// extra devDep — mirrors scenario-decision-graph.test.ts.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const ROOT = resolve(__dirname, "..", "..");

const improved = JSON.parse(
  readFileSync(
    resolve(ROOT, "lab-definitions", "firewall", "substation-improved.json"),
    "utf8",
  ),
);
const fw = improved.firewall ?? improved;
const rules: Array<Record<string, unknown>> = fw.rules ?? [];
const ruleById = (id: string) => rules.find((r) => r["id"] === id);

const LAB = readFileSync(
  resolve(ROOT, "lab-definitions", "scenarios", "firewall-implementation.yml"),
  "utf8",
);

describe("Lab 2.2 firewall content ↔ hardened reference config", () => {
  it("hardened config denies cross-zone by default", () => {
    expect(fw.defaultAction).toBe("DENY");
  });

  it("RTAC/GPS -> field rules are source-pinned to the host, not just the zone", () => {
    for (const id of ["rtac-to-field-modbus", "rtac-to-field-dnp3", "rtac-to-field-http"]) {
      const r = ruleById(id) as Record<string, unknown> | undefined;
      expect(r, `${id} present in hardened config`).toBeTruthy();
      expect((r?.["sources"] as string[]) ?? [], `${id} pinned to RTAC`).toContain(
        "10.30.30.20/32",
      );
    }
    expect((ruleById("gps-to-field-ntp")?.["sources"] as string[]) ?? []).toContain(
      "10.30.30.50/32",
    );
  });

  it("RTAC Modbus rule carries function-code DPI", () => {
    const ics = (ruleById("rtac-to-field-modbus")?.["ics"] as Record<string, unknown>) ?? {};
    expect(ics["protocol"]).toBe("modbus");
    expect(Array.isArray(ics["functionCode"]) && (ics["functionCode"] as unknown[]).length > 0).toBe(
      true,
    );
  });

  it("the lab teaches the RTAC source-pin the config enforces", () => {
    // Else students author the broad lan1->lan2 rule the lab warns against.
    expect(LAB).toContain("10.30.30.20/32");
  });

  it("the lab's Phase 3 JSON example includes sources + non-empty ICS DPI", () => {
    // Guards the exact regression: the example previously had neither.
    const example = LAB.slice(LAB.indexOf('"id": "rtac-to-field-modbus"'));
    expect(example).toMatch(/"sources":\s*\[\s*"10\.30\.30\.20\/32"\s*\]/);
    expect(example).toMatch(/"ics":\s*\{\s*"protocol":\s*"modbus"/);
  });

  it("the lab teaches every field ALLOW port the hardened config opens", () => {
    for (const port of ["502", "20000", "8080", "123"]) {
      expect(LAB, `lab mentions field port ${port}`).toContain(port);
    }
  });
});
