import { afterEach, describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BREAKER, FEEDER_STATE, LOST_LINK_STATE } from "../lib/operator-fixtures.test-util";
import { pointLabel } from "../lib/operator-format";
import { ControlCentreOneLine } from "./control-centre-one-line";
import { DeviceFaceplate } from "./control-centre-faceplate";
import { PointTable } from "./control-centre-points";
import { DEVICES } from "../lib/operator-workstation";

const mounted: ReactTestRenderer[] = [];
function faceplate(props: Parameters<typeof DeviceFaceplate>[0]) {
  const client = new QueryClient();
  const html = renderToStaticMarkup(<QueryClientProvider client={client}><DeviceFaceplate {...props} /></QueryClientProvider>);
  client.clear();
  return html;
}
function schematic(points = FEEDER_STATE.points) {
  let tree!: ReactTestRenderer;
  act(() => { tree = create(<ControlCentreOneLine points={points} />); });
  mounted.push(tree);
  return tree.root;
}
afterEach(() => { act(() => mounted.splice(0).forEach((tree) => tree.unmount())); });

describe("functional one-line observations", () => {
  it.each([
    [0, "DEAD", "#94a3b8"], [0.000004679, "DEAD", "#94a3b8"], [0.099, "DEAD", "#94a3b8"],
    [0.1, "LIVE", "#38bdf8"], [-1, "UNKNOWN", "#fbbf24"], [NaN, "UNKNOWN", "#fbbf24"], [Infinity, "UNKNOWN", "#fbbf24"],
  ])("colours S1 at %s kV as %s while retaining its measured value", (value, state, color) => {
    const points = FEEDER_STATE.points.map((p) => p.id === "rmu-1-source-kv" ? { ...p, value: value as number } : p);
    const tree = schematic(points);
    const s1 = tree.findAllByType("g").find((n) => n.props["aria-label"]?.startsWith("S1 residential:"))!;
    expect(s1.findAllByType("text")[0].children.join("")).toBe(`S1 residential · ${state}`);
    expect(tree.findAllByType("line").find((n) => n.props.x1 === 330)!.props.stroke).toBe(color);
    if (value === 0.000004679) expect(s1.props["aria-label"]).toContain("0.00 kV");
  });
  it.each([
    ["rmu-1-remote", "RMU1"],
    ["rmu-2-remote", "RMU2"],
    ["oltc-1-remote", "T1 OLTC"],
  ])("shows LOCAL on %s only for GOOD authority and marks uncertainty explicitly", (id, device) => {
    const points = structuredClone(FEEDER_STATE.points);
    const authority = points.find((p) => p.id === id)!;
    authority.value = false;
    const tag = schematic(points).findAllByType("g").find((n) => n.props["data-authority"] === id)!;
    expect(tag.props["aria-label"]).toContain(`${device} authority: LOCAL`);
    expect(tag.findByType("text").children.join("")).toBe("LOCAL");
    expect(tag.findByType("rect").props.stroke).toBe("#fbbf24");
    authority.value = true;
    expect(schematic(points).findAllByType("g").filter((n) => n.props["data-authority"] === id)).toHaveLength(0);
    for (const value of [false, true]) {
      authority.value = value;
      for (const flag of ["iv", "nt", "sb", "bl"] as const) {
        const bad = points.map((p) => p.id === id ? { ...p, quality: { ...p.quality, [flag]: true } } : p);
        const stale = schematic(bad).findAllByType("g").find((n) => n.props["data-authority"] === id)!;
        expect(stale.findByType("text").children.join("")).toBe("AUTHORITY STALE");
        expect(stale.props["aria-label"]).not.toMatch(/LOCAL|REMOTE/);
        expect(stale.findByType("rect").props.fill).toBe("url(#cc-unknown)");
        expect(stale.findByType("rect").props.strokeDasharray).toBeDefined();
      }
      authority.stale_reason = "not_updated";
      const stale = schematic(points).findAllByType("g").find((n) => n.props["data-authority"] === id)!;
      expect(stale.findByType("text").children.join("")).toBe("AUTHORITY STALE");
      expect(stale.findByType("title").children.join("")).toContain("not_updated");
      authority.stale_reason = null;
    }
    authority.value = null;
    for (const unknown of [points, points.filter((p) => p.id !== id)]) {
      const tag = schematic(unknown).findAllByType("g").find((n) => n.props["data-authority"] === id)!;
      expect(tag.findByType("text").children.join("")).toBe("AUTHORITY UNKNOWN");
      expect(tag.props["aria-label"]).not.toMatch(/LOCAL|REMOTE/);
    }
  });

  it("names the simplified functional model and retains the bounded schematic scroller", () => {
    const tree = schematic();
    expect(tree.findByType("svg").props["aria-label"]).toContain("simplified functional one-line");
    expect(tree.findByType("svg").props.className).toContain("min-w-[1060px]");
    expect(tree.findAllByType("div").some((n) => n.props.className === "max-w-full overflow-x-auto")).toBe(true);
    expect(tree.findByType("figure").props.className).toContain("min-w-0");
  });

  it("uses RMU1 S1-side voltage for S1 and its cable, not the CB1 VT", () => {
    const points = FEEDER_STATE.points.map((p) => p.id === "feeder-voltage" ? { ...p, value: 0 } : p);
    const tree = schematic(points);
    const s1 = tree.findAllByType("g").find((n) => n.props["aria-label"]?.startsWith("S1 residential:"))!;
    expect(s1.props["aria-label"]).toContain("19.95 kV");
    const cable = tree.findAllByType("line").find((n) => n.props.x1 === 330 && n.props.x2 === 570)!;
    expect(cable.props.stroke).toBe("#38bdf8");
    expect(renderToStaticMarkup(<ControlCentreOneLine points={points} />)).toContain("CB1 line-side VT");
  });

  it("colours the F2 run only from received S3 VT, with provenance", () => {
    const tree = schematic();
    const lower = tree.findAllByType("line").find((n) => n.props.x1 === "337")!;
    expect(lower.props.stroke).toBe("#38bdf8");
    expect(lower.props.strokeDasharray).toBeUndefined();
    expect(lower.props["aria-label"]).toContain("S3 received VT, not proof of path");
    const html = renderToStaticMarkup(<ControlCentreOneLine points={FEEDER_STATE.points} />);
    expect(html).toContain("S3 industrial · LIVE");
    expect(html).toContain("19.97 kV");
  });

  it("establishes a GOOD F2 path separately from measured voltage", () => {
    const tree = schematic();
    const s3 = tree.findAllByType("g").find((n) => n.props["aria-label"]?.startsWith("S3 industrial:"))!;
    expect(s3.props["aria-label"]).toContain("GOOD");
    expect(s3.findAllByType("text").map((n) => n.children.join("")).join("|")).toContain("via F2");
    expect(s3.findByType("title").children.join("")).toContain("Observed F2 path");
    expect(tree.findAllByType("text").filter((n) => n.props["data-supply-path"] === "via CB1")).toHaveLength(2);
  });

  it("shows no GOOD readings or live-blue conductors and all unknown paths on link loss", () => {
    const tree = schematic(LOST_LINK_STATE.points);
    const html = renderToStaticMarkup(<ControlCentreOneLine points={LOST_LINK_STATE.points} />);
    expect(html).not.toContain("GOOD");
    expect(html).not.toContain("via CB1");
    expect(tree.findAllByType("line").filter((n) => n.props.stroke === "#38bdf8")).toHaveLength(0);
    expect(tree.findAllByType("text").filter((n) => n.props["data-supply-path"] === "unknown")).toHaveLength(3);
    for (const selected of DEVICES) {
      const html = faceplate({ state: LOST_LINK_STATE, selected, unavailable: false });
      // The explanatory "not GOOD" regulation message is not a GOOD badge.
      expect(html).not.toContain("✓</span>GOOD");
    }
  });

  it("shows both observed feeder paths when the tie closes", () => {
    const points = FEEDER_STATE.points.map((p) => p.id === "rmu-2-position" ? { ...p, value: "closed" as const } : p);
    const tree = schematic(points);
    const s3 = tree.findAllByType("g").find((n) => n.props["aria-label"]?.startsWith("S3 industrial:"))!;
    expect(s3.findAllByType("text").map((n) => n.children.join("")).join("|")).toContain("via CB1 + F2");
    expect(s3.findByType("title").children.join("")).toContain("Both observed switch paths");
    expect(renderToStaticMarkup(<ControlCentreOneLine points={points} />)).not.toContain("F2 has no telemetry");
  });
  it("retains uncertain F2 position without treating live S3 VT as proof of its path", () => {
    const points = FEEDER_STATE.points.map((p) => p.id === "f2-position" ? { ...p, quality: { ...p.quality, iv: true, nt: true }, stale_reason: "device_unreachable" as const } : p);
    const tree = schematic(points);
    const f2 = tree.findAllByType("rect").find((n) => n.props["data-device"] === "F2")!;
    expect(f2.props).toMatchObject({ "data-position": "closed", "data-quality": "stale", fill: "url(#cc-unknown)" });
    const s3 = tree.findAllByType("g").find((n) => n.props["data-section"] === "S3")!;
    expect(s3.props).toMatchObject({ "data-state": "LIVE", "data-path": "path unknown" });
    const missing = schematic(points.filter((p) => p.id !== "f2-position"));
    expect(missing.findAllByType("rect").find((n) => n.props["data-device"] === "F2")!.props).toMatchObject({ "data-position": "unknown", "data-quality": "unknown" });
    expect(missing.findAllByType("g").find((n) => n.props["data-section"] === "S3")!.props["data-path"]).toBe("path unknown");
  });

  it("keeps stale CB1 closed feedback hatched amber after an unobserved field trip", () => {
    expect(BREAKER.position).toBe("open");
    const points = FEEDER_STATE.points.map((p) => p.id === "feeder-breaker-position"
      ? { ...p, quality: { ...p.quality, iv: true, nt: true }, stale_reason: "link_lost" as const } : p);
    const tree = schematic(points);
    const cb1 = tree.findAllByType("g").find((n) => n.props.role === "button" && n.props["aria-label"].startsWith("CB1:"))!;
    expect(cb1.props["aria-label"]).toContain("CLOSED · STALE · IV NT");
    const symbol = cb1.findAllByType("rect").find((n) => n.props.width === "40")!;
    expect(symbol.props.fill).toBe("url(#cc-unknown)");
    expect(symbol.props.stroke).toBe("#fbbf24");
    expect(symbol.props.strokeDasharray).toBeDefined();
    expect(cb1.findAllByType("rect").some((n) => n.props.fill === "#cbd5e1")).toBe(false);
    expect(tree.findAllByType("text").filter((n) => n.props["data-supply-path"] === "via CB1")).toHaveLength(0);
  });

  it("shares measurement locations across schematic, faceplates and Points", () => {
    const table = renderToStaticMarkup(<PointTable points={FEEDER_STATE.points} now={0} />);
    for (const selected of DEVICES) {
      const html = faceplate({ state: FEEDER_STATE, selected, unavailable: false });
      const prefix = selected === "F2" ? "f2-" : selected === "T1" ? "oltc-1-" : selected === "CB1" ? "feeder-" : selected === "RMU1" ? "rmu-1-" : "rmu-2-";
      for (const p of FEEDER_STATE.points.filter((p) => p.id.startsWith(prefix) && p.kind === "float")) {
        expect(html).toContain(pointLabel(p.id));
        expect(table).toContain(pointLabel(p.id));
      }
    }
    const html = renderToStaticMarkup(<ControlCentreOneLine points={FEEDER_STATE.points} />);
    for (const id of ["oltc-1-bus-kv", "oltc-1-current", "feeder-current", "feeder-voltage", "rmu-1-current", "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-current", "rmu-2-source-kv", "rmu-2-section-kv"]) {
      expect(html).toContain(pointLabel(id));
    }
  });
});
