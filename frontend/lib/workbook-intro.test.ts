import { readFileSync } from "node:fs";
import { expect, it } from "vitest";

it("lists requirements before planning in the workbook intro", () => {
  const source = readFileSync(new URL("./exercise-pdf.tsx", import.meta.url), "utf8");
  expect(source.replace(/\s+/g, " ")).toContain(
    "identify required communication flows and exposures, design segmentation requirements, plan remediation under real-world constraints, execute attacks",
  );
});
