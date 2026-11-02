// @vitest-environment jsdom
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
  expect(physical).toContain(row.f2 === "tripped" ? "F2 · OPEN · sim" : "F2 · CLOSED · sim");
  const s3Live = row.supply.split("/")[2] !== "D";
  expect(line(physical, "337", "325")).toContain(`stroke="${s3Live ? "#38bdf8" : "#fb7185"}"`);
  expect(line(cc, "337", "325")).toContain(`stroke="${s3Live ? "#38bdf8" : "#fb7185"}"`);
  expect(line(cc, "337", "325").includes("stroke-dasharray")).toBe(!s3Live);
});

it.each([
  [false, false, "#fb7185", true], [null, false, "#fbbf24", true], [true, true, "#fbbf24", true],
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

it.each([[true, false], [false, true], [true, true]])(
  "keeps CB1 trip/lockout=%s and F2 trip/lockout=%s out of annotation lanes in both views",
  (cb1Latched, f2Latched) => {
    const { field, operator } = oracleFixture(EU_ORACLE[12]);
    for (const [prefix, device, latched] of [
      ["feeder", "breaker-1", cb1Latched], ["f2", "f2-breaker", f2Latched],
    ] as const) {
      const breaker = field.devices[device];
      if (breaker.kind !== "breaker") throw new Error("Expected breaker");
      Object.assign(breaker, { position: latched ? "open" : "closed", protection_tripped: latched, lockout: latched });
      operator.points.find((p) => p.id === `${prefix}-protection-trip`)!.value = latched;
      operator.points.find((p) => p.id === `${prefix}-lockout`)!.value = latched;
      operator.points.find((p) => p.id === (prefix === "feeder" ? "feeder-breaker-position" : "f2-position"))!.value = latched ? "open" : "closed";
    }
    for (const physical of [false, true]) {
      const root = document.createElement("div");
      root.innerHTML = renderToStaticMarkup(<FeederOneLine physical={physical}
        read={physical ? physicalSystemObservations(field) : controlCentreObservations(operator.points)} />);
      const cb1 = root.querySelector<HTMLElement>('[data-breaker-badges="CB1"]')!;
      const f2 = root.querySelector<HTMLElement>('[data-breaker-badges="F2"]')!;
      const x = (node: HTMLElement) => parseFloat(node.style.left) / 100 * 1060;
      const y = (node: HTMLElement) => parseFloat(node.style.top) / 100 * 460;
      expect(cb1.classList.contains("flex-row")).toBe(true);
      expect(cb1.classList.contains("h-6")).toBe(true);
      expect(cb1.classList.contains("w-[210px]")).toBe(true);
      // Reserve the entire 210 × 24 HTML row, not just one badge's origin.
      const vt = root.querySelector('[data-point="feeder-voltage"]')!;
      expect(y(cb1)).toBeGreaterThan(Number(vt.getAttribute("y")) + Number(vt.getAttribute("font-size")));
      expect(y(cb1) + 24).toBeLessThan(325); // F2 conductor
      expect(x(cb1) - 105).toBeGreaterThan(290); // T1 labels end before this lane
      expect(x(cb1) + 105).toBeLessThan(530); // RMU1 earth/FPI assembly
      expect(f2.classList.contains("flex-col")).toBe(true);
      expect(x(f2) - 60).toBeGreaterThan(365); // F2 position label
      expect(x(f2) + 60).toBeLessThan(570); // S3 labels
      expect(y(f2)).toBeGreaterThan(325);
      expect(y(f2) + 48).toBeLessThan(414); // lowest NO SUPPLY lane
      for (const [prefix, latched] of [["feeder", cb1Latched], ["f2", f2Latched]] as const) {
        for (const [suffix, label] of [["protection-trip", "TRIPPED"], ["lockout", "LOCKED OUT"]]) {
          const badge = root.querySelector(`[data-point="${prefix}-${suffix}"][data-condition="asserted"]`);
          expect(badge?.textContent ?? null).toBe(latched ? label : null);
        }
      }
    }
  },
);
