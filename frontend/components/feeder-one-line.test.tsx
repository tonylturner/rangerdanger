// @vitest-environment jsdom
import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_ORACLE, oracleFixture } from "../lib/eu-oracle.test-util";
import { controlCentreObservations, physicalSystemObservations } from "../lib/observation-readings";
import { FeederOneLine } from "./feeder-one-line";

const line = (html: string, x1: string, y1: string) => html.match(new RegExp(`<line x1="${x1}" y1="${y1}"[^>]*>`))![0];

type Box = { x: number; y: number; width: number; height: number };
const centre = (box: Box) => ({ x: box.x + box.width / 2, y: box.y + box.height / 2 });
const distance = (a: Box, b: Box) => Math.hypot(centre(a).x - centre(b).x, centre(a).y - centre(b).y);
const overlaps = (a: Box, b: Box) => a.x < b.x + b.width && a.x + a.width > b.x
  && a.y < b.y + b.height && a.y + a.height > b.y;
const rectBox = (rect: Element, scale: number): Box => Object.fromEntries(
  ["x", "y", "width", "height"].map((key) => [key, Number(rect.getAttribute(key)) * scale]),
) as Box;
const textBox = (text: Element, scale: number): Box => {
  // jsdom has no SVG layout. Reserve a conservative 0.75em per character,
  // a full em above the baseline and 0.25em below; ignore tooltip <title>s.
  const copy = text.cloneNode(true) as Element;
  copy.querySelectorAll("title").forEach((title) => title.remove());
  const font = Number(text.getAttribute("font-size") ?? 16) * scale;
  const width = (copy.textContent?.length ?? 0) * font * 0.75;
  const anchor = text.getAttribute("text-anchor");
  return { x: Number(text.getAttribute("x")) * scale - (anchor === "middle" ? width / 2 : anchor === "end" ? width : 0),
    y: Number(text.getAttribute("y")) * scale - font, width, height: font * 1.25 };
};

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
  "keeps CB1 trip/lockout=%s and F2 trip/lockout=%s nearest their own breaker and clear of text in both views",
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
      // HTML badge sizes stay fixed while SVG labels scale with the canvas.
      // Check the minimum scroll width and larger desktop drawing widths.
      for (const canvasWidth of [1060, 1320, 1800]) {
        const scale = canvasWidth / 1060;
        const switches = new Map(["CB1", "F2", "RMU1", "RMU2"].map((device) => {
          const node = root.querySelector(`[data-device="${device}"]`)!;
          const rect = node.tagName === "rect" ? node : node.querySelector('rect[stroke-width="2"]')!;
          return [device, rectBox(rect, scale)];
        }));
        const clusters: Box[] = [];
        for (const device of ["CB1", "F2"]) {
          const node = root.querySelector<HTMLElement>(`[data-breaker-badges="${device}"]`)!;
          if (!node.children.length) continue;
          const row = node.classList.contains("flex-row");
          // Reserve the whole CB1 row, and padded bounds for the F2 column.
          const width = row ? 210 : 120;
          const height = row ? 24 : node.children.length * 24 + (node.children.length - 1) * 4;
          const box = { x: parseFloat(node.style.left) / 100 * canvasWidth - width / 2,
            y: parseFloat(node.style.top) / 100 * 460 * scale, width, height };
          if (row) {
            expect(node.classList.contains("w-[210px]")).toBe(true);
            expect(node.classList.contains("h-6")).toBe(true);
          }
          clusters.push(box);
          // Check each latch as well as the cluster, so a wide row cannot
          // leave LOCKED OUT nearer a different switch than TRIPPED.
          const widths = Array.from(node.children, (badge) => badge.textContent!.length * 12 * 0.75 + 18);
          const rowWidth = widths.reduce((sum, width) => sum + width, 0) + (widths.length - 1) * 4;
          const latchBoxes = widths.map((badgeWidth, index) => {
            const offset = widths.slice(0, index).reduce((sum, width) => sum + width + 4, 0);
            return { x: row ? centre(box).x - rowWidth / 2 + offset : centre(box).x - badgeWidth / 2,
              y: row ? box.y : box.y + index * 28, width: badgeWidth, height: 24 };
          });
          for (const bounds of [box, ...latchBoxes]) {
            for (const [other, symbol] of switches) {
              if (other !== device) expect(distance(bounds, switches.get(device)!),
                `${device} latch must be closer to ${device} than ${other}`).toBeLessThan(distance(bounds, symbol));
            }
          }
          for (const text of root.querySelectorAll("svg text")) {
            expect(overlaps(box, textBox(text, scale)),
              `${device} badges overlap "${text.firstChild?.textContent}" at canvas width ${canvasWidth}`).toBe(false);
          }
        }
        if (clusters.length === 2) expect(overlaps(clusters[0], clusters[1])).toBe(false);
      }
      for (const [prefix, latched] of [["feeder", cb1Latched], ["f2", f2Latched]] as const) {
        for (const [suffix, label] of [["protection-trip", "TRIPPED"], ["lockout", "LOCKED OUT"]]) {
          const badge = root.querySelector(`[data-point="${prefix}-${suffix}"][data-condition="asserted"]`);
          expect(badge?.textContent ?? null).toBe(latched ? label : null);
        }
      }
    }
  },
);

it.each([false, true])("keeps RMU leads, S2 VT and protection badges clear at supported canvas widths (physical=%s)", (physical) => {
  const { field, operator } = oracleFixture(EU_ORACLE[12]);
  for (const [prefix, id] of [["rmu-1", "rmu-1"], ["rmu-2", "rmu-2"]]) {
    const device = field.devices[id];
    if (device.kind !== "rmu") throw new Error("Expected RMU");
    Object.assign(device, { fault_passage: true, earth_closed: true });
    for (const suffix of ["fault-passage", "earth"]) operator.points.find((p) => p.id === `${prefix}-${suffix}`)!.value = true;
  }
  const root = document.createElement("div");
  root.innerHTML = renderToStaticMarkup(<FeederOneLine physical={physical} read={physical ? physicalSystemObservations(field) : controlCentreObservations(operator.points)} />);
  const vt = root.querySelector('[data-point="rmu-2-section-kv"]')!;
  expect(Number(vt.getAttribute("x"))).toBe(700);
  expect(Number(vt.getAttribute("x"))).toBeLessThan(850);
  expect(root.querySelector("figcaption")?.textContent).toContain("Filled switch (F2 adds a dot): closed");
  for (const canvasWidth of [1060, 1320, 1800]) {
    const scale = canvasWidth / 1060;
    const enclosures = [...root.querySelectorAll('[aria-label$="assembly: switch function and earth"] rect')].map((rect) => rectBox(rect, scale));
    for (const label of root.querySelectorAll('g[aria-label^="RMU2 S2 bus VT:"] text')) {
      for (const enclosure of enclosures) expect(overlaps(textBox(label, scale), enclosure), "S2 VT must not cross an RMU enclosure").toBe(false);
    }
    for (const device of ["RMU1", "RMU2"]) {
      const node = root.querySelector<HTMLElement>(`[data-rmu-badges="${device}"]`)!;
      expect(node.children).toHaveLength(2);
      const width = 126, height = node.children.length * 24 + (node.children.length - 1) * 4;
      const box = { x: parseFloat(node.style.left) / 100 * canvasWidth - width / 2, y: parseFloat(node.style.top) / 100 * 460 * scale, width, height };
      for (const enclosure of enclosures) expect(overlaps(box, enclosure)).toBe(false);
      for (const label of root.querySelectorAll("svg text")) expect(overlaps(box, textBox(label, scale)), `${device} overlaps ${label.firstChild?.textContent} at ${canvasWidth}`).toBe(false);
      const symbol = root.querySelector(`[data-device="${device}"]`)!;
      const lead = symbol.querySelector('[aria-label$="assembly: switch function and earth"] path')!;
      expect(lead.getAttribute("d")).toMatch(/h-65 v60$/);
      const x = device === "RMU1" ? 570 : 850;
      const status = [...symbol.querySelectorAll("text")].find((label) => label.getAttribute("y") === "225")!;
      expect((x - 65) * scale).toBeLessThan(textBox(status, scale).x);
    }
  }
});
