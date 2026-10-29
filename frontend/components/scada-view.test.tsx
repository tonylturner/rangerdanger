// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/react";
import { expect, it } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { BOOT_STATE, FEEDER_STATE, LOST_LINK_STATE, NOW, WATCHDOG_STATE } from "../lib/operator-fixtures.test-util";
import { devicePoints, DEVICES } from "../lib/operator-workstation";
import { AbnormalSummary, ControlCentreScreen, ScadaViewBanner } from "./control-centre-view";

it.each(["control-centre", "physical-system"] as const)("keeps a single persistent annunciator above %s and every records tab", (view) => {
  const client = new QueryClient();
  const page = (state = FEEDER_STATE, stateError: unknown = null) => <QueryClientProvider client={client}>
    <ControlCentreScreen state={state} stateError={stateError} stateUpdatedAt={NOW} journal={undefined} now={NOW} view={view} physicalSystem={<section aria-label="Independent physical observations">Independent physical observations</section>} />
  </QueryClientProvider>;
  const tree = render(page());
  const banner = () => tree.container.querySelector<HTMLElement>("[data-view-state]");
  try {
    expect(tree.container.querySelectorAll("[data-view-state]")).toHaveLength(1);
    expect(banner()?.getAttribute("data-view-state")).toBe("ok");
    expect(banner()?.children).toHaveLength(0);
    tree.rerender(page(LOST_LINK_STATE));
    expect(banner()?.getAttribute("data-view-state")).toBe("lost");
    expect(banner()?.getAttribute("role")).toBe("alert");
    expect(banner()?.className).toContain("sticky");
    expect(banner()?.querySelector("h2")?.textContent).toBe("SCADA VIEW LOST — retained values are not current");
    expect(banner()?.querySelector("p")?.textContent).toContain("42/42 inventory points not current");
    if (view === "control-centre") {
      for (const tab of tree.getAllByRole("tab")) {
        fireEvent.click(tab);
        expect(banner()?.getAttribute("data-view-state")).toBe("lost");
      }
    }
    const recovering = structuredClone(FEEDER_STATE);
    recovering.link.gi.last_complete_at = null;
    tree.rerender(page(recovering));
    expect(banner()?.getAttribute("data-view-state")).toBe("recovering");
    expect(banner()?.querySelector("h2")?.textContent).toBe("VIEW RECOVERING — GI/GOOD observations pending");
    const open = structuredClone(FEEDER_STATE);
    open.points.find((p) => p.id === "feeder-breaker-position")!.value = "open";
    open.points.find((p) => p.id === "feeder-current")!.value = 0;
    const pf = open.points.find((p) => p.id === "feeder-pf")!;
    pf.value = 0;
    pf.quality = { ...pf.quality, iv: true, nt: false };
    pf.stale_reason = "measurement_invalid";
    tree.rerender(page(open));
    expect(banner()?.getAttribute("data-view-state")).toBe("ok");
    expect(banner()?.children).toHaveLength(0);
    if (view === "control-centre") {
      expect(tree.container.querySelector("[data-observation-uncertainty='1']")?.textContent).toContain("1 non-GOOD measurement");
    }
    pf.stale_reason = "device_unreachable";
    tree.rerender(page(open));
    expect(banner()?.getAttribute("data-view-state")).toBe("degraded");
    expect(banner()?.getAttribute("role")).toBe("status");
    expect(banner()?.querySelector("h2")?.textContent).toBe("SCADA VIEW DEGRADED — 1/42 points not current");
    expect(banner()?.querySelector("p")?.textContent).toContain("Affected devices: CB1.");
    tree.rerender(page(WATCHDOG_STATE));
    expect(banner()?.getAttribute("data-view-state")).toBe("degraded");
    expect(banner()?.getAttribute("role")).toBe("status");
    expect(banner()?.className).toContain("border-amber-400");
    expect(banner()?.querySelector("h2")?.textContent).toBe("SCADA VIEW DEGRADED — 1/42 points not current");
    expect(banner()?.querySelector("p")?.textContent).toContain("Affected devices: T1. Retained values for these points are not current.");
    for (const device of DEVICES) {
      const degraded = structuredClone(FEEDER_STATE);
      const points = devicePoints(degraded.points, device);
      for (const p of points) {
        p.quality = { ...p.quality, iv: true, nt: true };
        p.stale_reason = "device_unreachable";
      }
      tree.rerender(page(degraded));
      expect(banner()?.getAttribute("data-view-state")).toBe("degraded");
      expect(banner()?.getAttribute("role")).toBe("status");
      expect(banner()?.querySelector("h2")?.textContent).toBe(`SCADA VIEW DEGRADED — ${points.length}/42 points not current`);
      expect(banner()?.querySelector("p")?.textContent).toContain(`Affected devices: ${device}. Retained values for these points are not current.`);
      expect(banner()?.querySelector("h2")?.textContent).not.toContain("pending");
    }
    tree.rerender(page(FEEDER_STATE, new Error("offline")));
    expect(banner()?.getAttribute("data-view-state")).toBe("unavailable");
    tree.rerender(page(BOOT_STATE));
    expect(banner()?.getAttribute("data-view-state")).toBe("not-established");
    tree.rerender(page());
    expect(banner()?.getAttribute("data-view-state")).toBe("ok");
    expect(banner()?.children).toHaveLength(0);
    tree.rerender(page(LOST_LINK_STATE));
    const contentLabel = view === "control-centre" ? "CC observation snapshot" : "Independent physical observations";
    const sections = tree.container.querySelectorAll(`[data-view-state], [aria-label="${contentLabel}"]`);
    expect(sections).toHaveLength(2);
    expect(sections[0].getAttribute("data-view-state")).toBe("lost");
    expect(sections[1].getAttribute("aria-label")).toBe(contentLabel);
  } finally {
    tree.unmount();
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
