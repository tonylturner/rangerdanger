import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { FIREWALL_ACTIVE_KEY, SUBSTATION_KEY } from "./live-queries";
import { isRangeScopedQuery, resetRangeScopedQueries } from "./range";

const LIVE_KEYS = [
  [...SUBSTATION_KEY, "state"],
  [...SUBSTATION_KEY, "audit"],
  [...SUBSTATION_KEY, "network-events"],
  FIREWALL_ACTIVE_KEY,
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
