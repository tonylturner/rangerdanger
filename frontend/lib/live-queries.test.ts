import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  FIREWALL_ACTIVE_KEY,
  SUBSTATION_KEY,
  WORKSHOP_STATUS_KEY,
  substationAuditQuery,
  substationStateQuery,
} from "./live-queries";
import { EU_PACKAGE, US_PACKAGE } from "./operator-fixtures.test-util";
import { resolveOperatorView, usFeederPolling } from "./operator-view";
import { isRangeScopedQuery, resetRangeScopedQueries } from "./range";

const LIVE_KEYS = [
  [...SUBSTATION_KEY, "state"],
  [...SUBSTATION_KEY, "audit"],
  [...SUBSTATION_KEY, "network-events"],
  FIREWALL_ACTIVE_KEY,
  WORKSHOP_STATUS_KEY,
];

describe("live queries", () => {
  it("are range-scoped, so a range change resets them", async () => {
    const client = new QueryClient();
    for (const key of LIVE_KEYS) {
      expect(isRangeScopedQuery(key)).toBe(true);
      client.setQueryData(key, { from: "old range" });
    }
    await resetRangeScopedQueries(client);
    for (const key of LIVE_KEYS) expect(client.getQueryData(key)).toBeUndefined();
    client.clear();
  });

  it("refresh by the substation prefix without touching other reads", async () => {
    const client = new QueryClient();
    client.setQueryData([...SUBSTATION_KEY, "state"], 1);
    client.setQueryData(["substation-health"], 1);
    await client.invalidateQueries({ queryKey: SUBSTATION_KEY });
    expect(client.getQueryState([...SUBSTATION_KEY, "state"])?.isInvalidated).toBe(true);
    expect(client.getQueryState(["substation-health"])?.isInvalidated).toBe(false);
    client.clear();
  });
});

describe("US RTAC polling", () => {
  afterEach(() => vi.unstubAllGlobals());

  // Subscribes like a mounted component would and counts the requests
  // that reach fetch.
  async function requestsFor(enabled: boolean): Promise<string[]> {
    const urls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        urls.push(url);
        return new Response(JSON.stringify({ entries: [] }), { status: 200 });
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const observers = [
      new QueryObserver(client, substationStateQuery(60_000, enabled)),
      new QueryObserver(client, substationAuditQuery(60_000, enabled)),
    ];
    const unsubscribe = observers.map((observer) => observer.subscribe(() => {}));
    await new Promise((resolve) => setTimeout(resolve, 20));
    unsubscribe.forEach((u) => u());
    client.clear();
    return urls;
  }

  it("requests the RTAC state and audit for the US operator view", async () => {
    const urls = await requestsFor(usFeederPolling(resolveOperatorView(US_PACKAGE, null)));
    expect(urls.sort()).toEqual(["/api/substation/audit", "/api/substation/state"]);
  });

  it("sends no RTAC request for the EU operator view or while the view loads", async () => {
    expect(await requestsFor(usFeederPolling(resolveOperatorView(EU_PACKAGE, null)))).toEqual([]);
    expect(await requestsFor(usFeederPolling(resolveOperatorView(undefined, null)))).toEqual([]);
  });
});
