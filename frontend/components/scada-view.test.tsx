import { expect, it } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { BOOT_STATE, FEEDER_STATE, LOST_LINK_STATE, NOW, WATCHDOG_STATE } from "../lib/operator-fixtures.test-util";
import { devicePoints, DEVICES } from "../lib/operator-workstation";
import { AbnormalSummary, ControlCentreScreen, ScadaViewBanner } from "./control-centre-view";

it.each(["control-centre", "physical-system"] as const)("keeps a single persistent annunciator above %s and every records tab", (view) => {
  const client = new QueryClient();
  let tree!: ReactTestRenderer;
  const page = (state = FEEDER_STATE, stateError: unknown = null) => <QueryClientProvider client={client}>
    <ControlCentreScreen state={state} stateError={stateError} stateUpdatedAt={NOW} journal={undefined} now={NOW} view={view} physicalSystem={<section aria-label="Independent physical observations">Independent physical observations</section>} />
  </QueryClientProvider>;
  act(() => { tree = create(page()); });
  const banner = () => tree.root.findAll((n) => typeof n.type === "string" && n.props["data-view-state"] !== undefined);
  try {
    expect(banner()).toHaveLength(1);
    expect(banner()[0].props["data-view-state"]).toBe("ok");
    expect(banner()[0].children).toHaveLength(0);
    act(() => tree.update(page(LOST_LINK_STATE)));
    expect(banner()[0].props).toMatchObject({ "data-view-state": "lost", role: "alert" });
    expect(banner()[0].props.className).toContain("sticky");
    expect(banner()[0].findByType("h2").children.join("")).toBe("SCADA VIEW LOST — retained values are not current");
    expect(banner()[0].findByType("p").children.join("")).toContain("42/42 inventory points not current");
    if (view === "control-centre") {
      for (const tab of tree.root.findAllByProps({ role: "tab" })) {
        act(() => tab.props.onClick());
        expect(banner()[0].props["data-view-state"]).toBe("lost");
      }
    }
    const recovering = structuredClone(FEEDER_STATE);
    recovering.link.gi.last_complete_at = null;
    act(() => tree.update(page(recovering)));
    expect(banner()[0].props["data-view-state"]).toBe("recovering");
    expect(banner()[0].findByType("h2").children.join("")).toBe("VIEW RECOVERING — GI/GOOD observations pending");
    const open = structuredClone(FEEDER_STATE);
    open.points.find((p) => p.id === "feeder-breaker-position")!.value = "open";
    open.points.find((p) => p.id === "feeder-current")!.value = 0;
    const pf = open.points.find((p) => p.id === "feeder-pf")!;
    pf.value = 0;
    pf.quality = { ...pf.quality, iv: true, nt: false };
    pf.stale_reason = "measurement_invalid";
    act(() => tree.update(page(open)));
    expect(banner()[0].props["data-view-state"]).toBe("ok");
    expect(banner()[0].children).toHaveLength(0);
    if (view === "control-centre") {
      expect(tree.root.findByProps({ "data-observation-uncertainty": 1 }).children.join("")).toContain("1 non-GOOD measurement");
    }
    pf.stale_reason = "device_unreachable";
    act(() => tree.update(page(open)));
    expect(banner()[0].props).toMatchObject({ "data-view-state": "degraded", role: "status" });
    expect(banner()[0].findByType("h2").children.join("")).toBe("SCADA VIEW DEGRADED — 1/42 points not current");
    expect(banner()[0].findByType("p").children.join("")).toContain("Affected devices: CB1.");
    act(() => tree.update(page(WATCHDOG_STATE)));
    expect(banner()[0].props).toMatchObject({ "data-view-state": "degraded", role: "status" });
    expect(banner()[0].props.className).toContain("border-amber-400");
    expect(banner()[0].findByType("h2").children.join("")).toBe("SCADA VIEW DEGRADED — 1/42 points not current");
    expect(banner()[0].findByType("p").children.join("")).toContain("Affected devices: T1. Retained values for these points are not current.");
    for (const device of DEVICES) {
      const degraded = structuredClone(FEEDER_STATE);
      const points = devicePoints(degraded.points, device);
      for (const p of points) {
        p.quality = { ...p.quality, iv: true, nt: true };
        p.stale_reason = "device_unreachable";
      }
      act(() => tree.update(page(degraded)));
      expect(banner()[0].props).toMatchObject({ "data-view-state": "degraded", role: "status" });
      expect(banner()[0].findByType("h2").children.join("")).toBe(`SCADA VIEW DEGRADED — ${points.length}/42 points not current`);
      expect(banner()[0].findByType("p").children.join("")).toContain(`Affected devices: ${device}. Retained values for these points are not current.`);
      expect(banner()[0].findByType("h2").children.join("")).not.toContain("pending");
    }
    act(() => tree.update(page(FEEDER_STATE, new Error("offline"))));
    expect(banner()[0].props["data-view-state"]).toBe("unavailable");
    act(() => tree.update(page(BOOT_STATE)));
    expect(banner()[0].props["data-view-state"]).toBe("not-established");
    act(() => tree.update(page()));
    expect(banner()[0].props["data-view-state"]).toBe("ok");
    expect(banner()[0].children).toHaveLength(0);
    // Keep this assertion in the mounted renderer: switching this context
    // provider to react-dom/server produces multiple-renderer warnings.
    act(() => tree.update(page(LOST_LINK_STATE)));
    const contentLabel = view === "control-centre" ? "CC observation snapshot" : "Independent physical observations";
    const sections = tree.root.findAll((n) => typeof n.type === "string" && (
      n.props["data-view-state"] !== undefined || n.props["aria-label"] === contentLabel
    ));
    expect(sections).toHaveLength(2);
    expect(sections[0].props["data-view-state"]).toBe("lost");
    expect(sections[1].props["aria-label"]).toBe(contentLabel);
  } finally {
    act(() => tree.unmount());
    client.clear();
  }
});

it("excludes invalid-only measurements from affected device groups and counts without implying a pending GI", () => {
  const state = structuredClone(WATCHDOG_STATE);
  const pf = state.points.find((p) => p.id === "feeder-pf")!;
  pf.value = 0;
  pf.quality = { ...pf.quality, iv: true, nt: false };
  pf.stale_reason = "measurement_invalid";
  for (const p of devicePoints(state.points, "F2")) {
    p.quality = { ...p.quality, iv: true, nt: true };
    p.stale_reason = "device_unreachable";
  }
  const html = renderToStaticMarkup(<ScadaViewBanner state={state} unavailable={false} />);
  expect(html).toContain("SCADA VIEW DEGRADED — 5/42 points not current");
  expect(html).toContain("Affected devices: T1, F2.");
  expect(html).toContain("Retained values for these points are not current.");
  expect(html).not.toContain("pending");
});

it("separates uncertain measurements from the GOOD-observation empty summary", () => {
  const state = { ...FEEDER_STATE, points: FEEDER_STATE.points.map((p) => p.id === "oltc-1-bus-kv" ? { ...p, quality: { ...p.quality, iv: true } } : p) };
  const html = renderToStaticMarkup(<AbnormalSummary state={state} />);
  expect(html).toContain('data-condition="none"');
  expect(html).toContain("No alarms in GOOD observations");
  expect(html).toContain('data-observation-uncertainty="1"');
  expect(html).toContain("1 non-GOOD measurement");
});
