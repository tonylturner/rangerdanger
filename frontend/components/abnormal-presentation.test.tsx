// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FEEDER_STATE } from "../lib/operator-fixtures.test-util";
import { QUALITY_DEFINITIONS, qualityDefinition } from "../lib/operator-format";
import { SWITCH_NORMALS } from "../lib/operator-workstation";
import { controlCentreObservations } from "../lib/observation-readings";
import { FeederOneLine } from "./feeder-one-line";
import { AbnormalSummary } from "./control-centre-view";
import { DeviceFaceplate } from "./control-centre-faceplate";
import { PointTable } from "./control-centre-points";

function schematic(state = FEEDER_STATE) {
  return render(<FeederOneLine read={controlCentreObservations(state.points)} />).container;
}

it.each(Object.entries(SWITCH_NORMALS))("%s uses solid amber for all known off-normal positions, not stale data", (device, { id, normal }) => {
  for (const value of ["open", "closed", "intermediate", "indeterminate"] as const) {
    const state = structuredClone(FEEDER_STATE);
    const p = state.points.find((p) => p.id === id)!;
    p.value = value;
    const offNormal = value !== normal;
    const root = schematic(state);
    const node = root.querySelector(`[data-device="${device}"]`)!;
    expect(node.getAttribute("data-off-normal")).toBe(String(offNormal));
    const rect = device === "F2" ? node : node.querySelector('rect[width="40"]')!;
    expect(rect.getAttribute("stroke")).toBe(offNormal ? "#fbbf24" : "#cbd5e1");
    expect(rect.getAttribute("stroke-dasharray")).toBeNull();
    expect(rect.getAttribute("fill")).toBe(value === "closed" ? "#cbd5e1" : "#0f172a");
    const label = [...(device === "F2" ? node.parentElement! : node).querySelectorAll("text")].find((n) => n.textContent?.includes(`${value.toUpperCase()} · GOOD`))!;
    expect(label.getAttribute("fill")).toBe(offNormal ? "#fbbf24" : "#cbd5e1");
    p.quality.nt = true;
    const staleNode = schematic(state).querySelector(`[data-device="${device}"]`)!;
    expect(staleNode.getAttribute("data-off-normal")).toBe("false");
    const staleRect = device === "F2" ? staleNode : staleNode.querySelector('rect[width="40"]')!;
    expect(staleRect.getAttribute("stroke")).toBe("#fbbf24");
    expect(staleRect.getAttribute("stroke-dasharray")).toBe("5 3");
    expect(staleRect.getAttribute("fill")).toBe("url(#cc-unknown)");
  }
});

it.each([
  ["T1", "oltc-1-auto"], ["T1", "oltc-1-remote"], ["RMU1", "rmu-1-remote"], ["RMU2", "rmu-2-remote"],
])("%s highlights GOOD MANUAL/LOCAL from %s independently of position", (device, id) => {
  const state = structuredClone(FEEDER_STATE);
  const p = state.points.find((p) => p.id === id)!;
  p.value = false;
  expect(schematic(state).querySelector(`[data-device="${device}"]`)?.getAttribute("data-off-normal")).toBe("true");
  p.quality.iv = true;
  expect(schematic(state).querySelector(`[data-device="${device}"]`)?.getAttribute("data-off-normal")).toBe("false");
});

it("counts, orders and distinguishes alarms from off-normal conditions, including cached snapshots", () => {
  const state = structuredClone(FEEDER_STATE);
  for (const id of ["feeder-breaker-position", "f2-position"]) state.points.find((p) => p.id === id)!.value = "open";
  state.points.find((p) => p.id === "rmu-1-source-kv")!.value = 0;
  const tree = render(<AbnormalSummary state={state} cached />);
  const section = tree.container.querySelector("section")!;
  expect(section.getAttribute("data-alarm-count")).toBe("1");
  expect(section.getAttribute("data-off-normal-count")).toBe("2");
  const entries = [...tree.container.querySelectorAll("li")];
  expect(entries.map((n) => n.getAttribute("data-condition-kind"))).toEqual(["alarm", "off-normal", "off-normal"]);
  expect(entries[0].className).toBe("text-rose-300");
  expect(entries[1].className).toBe("text-amber-200");
  expect(renderToStaticMarkup(<AbnormalSummary state={state} cached />)).toContain("1 alarm · 2 off-normal in cached snapshot");
  expect(renderToStaticMarkup(<AbnormalSummary state={FEEDER_STATE} />)).toContain("No alarms in GOOD observations");
});

it("uses the same IEC quality definitions on one-line labels, faceplates, commands and Points", () => {
  const client = new QueryClient();
  try {
    const html = renderToStaticMarkup(<QueryClientProvider client={client}><DeviceFaceplate state={FEEDER_STATE} selected="CB1" unavailable={false} /></QueryClientProvider>);
    expect(html.match(new RegExp(`title="${QUALITY_DEFINITIONS.GOOD}"`, "g"))!.length).toBeGreaterThan(1);
    expect(html).toContain("Reported: ");
    expect(renderToStaticMarkup(<PointTable points={FEEDER_STATE.points} now={0} />)).toContain(`title="${QUALITY_DEFINITIONS.GOOD}"`);
    expect(renderToStaticMarkup(<FeederOneLine read={controlCentreObservations(FEEDER_STATE.points)} />)).toContain(`<title>${QUALITY_DEFINITIONS.GOOD}</title>`);
    expect(qualityDefinition("never")).toBe(QUALITY_DEFINITIONS.UNKNOWN);
    for (const condition of ["good", "stale", "unknown", "invalid"] as const) expect(qualityDefinition(condition)).toContain("IEC 104 data quality:");
  } finally { client.clear(); }
});
