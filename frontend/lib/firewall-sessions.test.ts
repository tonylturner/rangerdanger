import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { controlCentreTuple, killResultText, sameTuple, sessionTuple } from "./firewall-sessions";
import { FRESH_STATE, STALE_STATE } from "./operator-fixtures.test-util";

describe("firewall session tuples", () => {
  it("builds the control centre's session from link.local to the RTU", () => {
    expect(controlCentreTuple(FRESH_STATE.link)).toEqual({
      proto: "tcp",
      src: "10.60.60.20",
      dst: "10.30.30.20",
      sport: 49152,
      dport: 2404,
    });
    expect(controlCentreTuple(STALE_STATE.link)).toBeNull();
    expect(controlCentreTuple(undefined)).toBeNull();
  });

  it("matches a listed session to the control centre's exactly", () => {
    const cc = controlCentreTuple(FRESH_STATE.link);
    const listed = sessionTuple({ proto: "tcp", src: "10.60.60.20", dst: "10.30.30.20", sport: 49152, dport: 2404, state: "ESTABLISHED" });
    const kali = sessionTuple({ proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20", sport: 49152, dport: 2404 });
    expect(sameTuple(listed, cc)).toBe(true);
    expect(sameTuple(kali, cc)).toBe(false);
    expect(sessionTuple({ proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20" })).toBeNull();
  });

  it("says what a kill did", () => {
    const t = { proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20", sport: 40000, dport: 2404 };
    expect(killResultText(t, null)).toMatch(/Deleted tcp 10\.10\.10\.50:40000 → 10\.30\.30\.20:2404.*only if the policy denies it/);
    expect(killResultText(t, new ApiError(404, "not found"))).toMatch(/already gone/);
    expect(killResultText(t, new ApiError(502, "upstream"))).toMatch(/failed/);
  });
});
