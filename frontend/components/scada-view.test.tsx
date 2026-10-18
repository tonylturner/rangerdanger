import { expect, it } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { BOOT_STATE, FEEDER_STATE, LOST_LINK_STATE, NOW, WATCHDOG_STATE } from "../lib/operator-fixtures.test-util";
import { AbnormalSummary, ControlCentreScreen } from "./control-centre-view";

it.each(["control-centre", "physical-system"] as const)("keeps a single persistent annunciator above %s and every records tab", (view) => {
  const client = new QueryClient();
  let tree!: ReactTestRenderer;
  const page = (state = FEEDER_STATE, stateError: unknown = null) => <QueryClientProvider client={client}>
    <ControlCentreScreen state={state} stateError={stateError} stateUpdatedAt={NOW} journal={undefined} now={NOW} view={view} physicalSystem={<section>Independent physical observations</section>} />
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
    expect(banner()[0].findByType("p").children.join("")).toContain("42/42 inventory points not GOOD");
    if (view === "control-centre") {
      for (const tab of tree.root.findAllByProps({ role: "tab" })) {
        act(() => tab.props.onClick());
        expect(banner()[0].props["data-view-state"]).toBe("lost");
      }
    }
    act(() => tree.update(page(WATCHDOG_STATE)));
    expect(banner()[0].props["data-view-state"]).toBe("recovering");
    expect(banner()[0].findByType("p").children.join("")).toContain("1/42");
    act(() => tree.update(page(FEEDER_STATE, new Error("offline"))));
    expect(banner()[0].props["data-view-state"]).toBe("unavailable");
    act(() => tree.update(page(BOOT_STATE)));
    expect(banner()[0].props["data-view-state"]).toBe("not-established");
    act(() => tree.update(page()));
    expect(banner()[0].props["data-view-state"]).toBe("ok");
    expect(banner()[0].children).toHaveLength(0);
    const html = renderToStaticMarkup(page(LOST_LINK_STATE));
    expect(html.indexOf('data-view-state="lost"')).toBeLessThan(html.indexOf(view === "control-centre" ? 'aria-label="CC observation snapshot"' : "<section>Independent physical observations"));
  } finally {
    act(() => tree.unmount());
    client.clear();
  }
});

it("separates uncertain measurements from the GOOD-observation empty summary", () => {
  const state = { ...FEEDER_STATE, points: FEEDER_STATE.points.map((p) => p.id === "oltc-1-bus-kv" ? { ...p, quality: { ...p.quality, iv: true } } : p) };
  const html = renderToStaticMarkup(<AbnormalSummary state={state} />);
  expect(html).toContain('data-condition="none"');
  expect(html).toContain("No alarms in GOOD observations");
  expect(html).toContain('data-observation-uncertainty="1"');
  expect(html).toContain("1 non-GOOD measurements");
});
