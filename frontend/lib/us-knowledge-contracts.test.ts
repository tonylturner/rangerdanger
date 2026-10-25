import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const article = (name: string) => readFileSync(resolve(__dirname,
  `../../lab-definitions/packages/us-dnp3-substation/knowledge/${name}.md`), "utf8");

describe("US knowledge guidance", () => {
  it.each(["ics-dpi", "live-dpi-events-strip", "how-containd-enforces-policy",
    "modbus-tcp", "dnp3", "weak-vs-hardened", "plan-coverage-pipeline"])(
    "%s distinguishes L4 enforcement from detection", (name) => {
      expect(article(name)).toContain("The L4 source pin enforces; DPI detects and logs");
      expect(article(name)).not.toMatch(/DPI verdict drops|rejected by DPI|NF_DROP/);
    },
  );

  it("keeps plan coverage advisory rather than a claim about running state", () => {
    expect(article("plan-coverage-pipeline")).toContain("The plan is advisory");
    expect(article("plan-coverage-pipeline")).toContain("does not apply a firewall policy or rewrite later labs");
  });
});
