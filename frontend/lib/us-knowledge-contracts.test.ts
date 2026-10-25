import { readFileSync, readdirSync } from "node:fs";
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

  it("uses flag-first polling and same-origin app links", () => {
    expect(article("tool-dnp3")).toContain("dnp3poll -a 1 -t 3s 10.40.40.20:20000");
    expect(article("hmi-scada-fuxa")).toContain("[Feeder HMI](/substation)");
    expect(article("hmi-scada-fuxa")).toContain("[FUXA UI](/apps/fuxa-hmi/)");
    expect(article("plc-ladder-openplc")).toContain("[OpenPLC UI](/apps/openplc/)");
    const dir = resolve(__dirname, "../../lab-definitions/packages/us-dnp3-substation/knowledge");
    for (const file of readdirSync(dir).filter((name) => name.endsWith(".md"))) {
      expect(readFileSync(resolve(dir, file), "utf8")).not.toMatch(/localhost:8088|frangoteam\.com|openplcproject\.com/);
    }
  });
});
