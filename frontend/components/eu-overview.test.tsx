import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WORKSHOP_STATUS_KEY } from "../lib/live-queries";
import { FEEDER_STATE, EU_PACKAGE, LOST_LINK_STATE } from "../lib/operator-fixtures.test-util";
import { DeviceComms, EuDashboard, EuWorkshopStatus } from "./eu-overview";
import { HISTORY } from "../lib/historian-fixtures.test-util";
import { RangeProvider } from "./range-context";
import { RangeScope } from "./range-scope";

function render(workshop: boolean, state = FEEDER_STATE) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["packages", "active"], EU_PACKAGE);
  client.setQueryData(["operator", "state"], state);
  client.setQueryData(WORKSHOP_STATUS_KEY, { control_centre: { link: "started", gi: "complete" }, firewall_online: true, firewall_config: "improved" });
  client.setQueryData(["historian", "points"], state.points);
  client.setQueryData(["historian", "history", "oltc-1-bus-kv"], HISTORY);
  const html = renderToStaticMarkup(<QueryClientProvider client={client}><RangeProvider><RangeScope>{workshop ? <EuWorkshopStatus title="EU" /> : <EuDashboard title="EU" />}</RangeScope></RangeProvider></QueryClientProvider>);
  client.clear();
  return html;
}
describe("EU overview and workshop comms", () => {
  it("lets the dashboard and trend shrink while comms items wrap inside their available width", () => {
    const html = render(false);
    expect(html).toContain('<section class="min-w-0 flex-1">');
    expect(html).toContain("w-full min-w-0 max-w-6xl");
    expect(html).toContain('aria-label="Historian trend" class="min-w-0');
    expect(html).toContain('<dl class="flex flex-wrap gap-x-6 gap-y-2 text-sm">');
    expect(html).toContain('class="min-w-0 max-w-full wrap-break-word"');
    expect(html).toContain('aria-label="Latest snapshots"');
  });
  it.each([false, true])("shows source-device comms in workshop=%s", (workshop) => {
    const html = render(workshop);
    expect(html).toContain('aria-label="Device comms health"');
    for (const device of ["feeder-breaker", "rmu-1", "rmu-2", "oltc-1", "plc-1"]) expect(html).toContain(`<dt class="text-slate-400">${device}</dt><dd class="text-slate-200">ok</dd>`);
    expect(render(workshop, LOST_LINK_STATE)).toContain("link down");
  });
  it("offers only historian bus and section voltage signals on Overview", () => {
    const html = render(false);
    const select = html.match(/<select[^>]*>(.*?)<\/select>/)?.[1];
    expect(select).toBeDefined();
    for (const id of ["oltc-1-bus-kv", "feeder-voltage", "rmu-1-section-kv", "rmu-2-section-kv"]) expect(select).toContain(`value="${id}"`);
    expect(select).not.toContain('value="feeder-current"');
    expect(html).toContain("Voltage trend · bus and sections");
  });
  it("marks retained comms as cached instead of live when the API fails", () => {
    const html = renderToStaticMarkup(<DeviceComms state={FEEDER_STATE} unavailable />);
    expect(html).toContain("retained snapshot, not live comms health");
  });
});
