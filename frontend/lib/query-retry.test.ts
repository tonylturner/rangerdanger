import { describe, expect, it } from "vitest";
import { OperatorStateSchemaError } from "./operator-api";
import { retryQuery } from "./query-retry";
import { RangeNotReadyError } from "./range";

describe("portal retry policy", () => {
  it("never retries deterministic operator schema errors or range transitions", () => {
    expect(retryQuery(0, new OperatorStateSchemaError())).toBe(false);
    expect(retryQuery(0, new RangeNotReadyError("starting"))).toBe(false);
  });
  it("preserves three retries for network failures", () => {
    const error = new TypeError("Failed to fetch");
    expect([0, 1, 2, 3].map((count) => retryQuery(count, error))).toEqual([true, true, true, false]);
  });
});
