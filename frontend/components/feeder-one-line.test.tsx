import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_ORACLE, oracleFixture } from "../lib/eu-oracle.test-util";
import { controlCentreObservations, physicalSystemObservations } from "../lib/observation-readings";
import { FeederOneLine } from "./feeder-one-line";

const line = (html: string, x1: string, y1: string) => html.match(new RegExp(`<line x1="${x1}" y1="${y1}"[^>]*>`))![0];

it.each(EU_ORACLE)("$switches / F2 $f2 / VT $vt: physical source stubs stay live even at zero current; CC retains measurement provenance", (row) => {
  const { field, operator } = oracleFixture(row);
  const physical = renderToStaticMarkup(<FeederOneLine physical read={physicalSystemObservations(field)} />);
  const cc = renderToStaticMarkup(<FeederOneLine read={controlCentreObservations(operator.points)} />);
  for (const y of ["45", "325"]) {
    expect(line(physical, "80", y)).toContain('stroke="#38bdf8"');
    expect(line(physical, "80", y)).not.toContain("stroke-dasharray");
    expect(line(cc, "80", y)).toContain(y === "45" ? 'stroke="#94a3b8"' : 'stroke="#38bdf8"');
    expect(line(cc, "80", y).includes("stroke-dasharray")).toBe(y === "45");
  }
  expect(physical).toContain(row.f2 === "tripped" ? "F2 · OPEN · GOOD · sim" : "F2 · CLOSED · GOOD · sim");
  const s3Live = row.supply.split("/")[2] !== "D";
  expect(line(physical, "337", "325")).toContain(`stroke="${s3Live ? "#38bdf8" : "#94a3b8"}"`);
  expect(line(cc, "337", "325")).toContain(`stroke="${s3Live ? "#38bdf8" : "#94a3b8"}"`);
  expect(line(cc, "337", "325")).not.toContain("stroke-dasharray");
});

it.each([
  [false, false, "#94a3b8", false], [null, false, "#fbbf24", true], [true, true, "#fbbf24", true],
] as const)("marks physical source stubs from bus20 live=%s, truth stale=%s", (live, stale, color, dashed) => {
  const { field } = oracleFixture(EU_ORACLE[12]);
  field.process.buses.bus20.live = live;
  field.process.truth_stale = stale;
  const html = renderToStaticMarkup(<FeederOneLine physical read={physicalSystemObservations(field)} />);
  for (const y of ["45", "325"]) {
    expect(line(html, "80", y)).toContain(`stroke="${color}"`);
    expect(line(html, "80", y).includes("stroke-dasharray")).toBe(dashed);
  }
});
