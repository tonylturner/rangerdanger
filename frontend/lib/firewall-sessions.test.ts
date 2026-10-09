import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import {
  SessionLookupError,
  controlCentreMatchText,
  cutFailureText,
  findControlCentreSession,
  isControlCentreSession,
  killResultText,
  matchControlCentreSession,
  sessionTuple,
} from "./firewall-sessions";
import type { FirewallSession } from "./operator-api";
import { SESSIONS } from "./operator-fixtures.test-util";

const [CC, KALI] = SESSIONS.sessions;
const CC_TUPLE = { proto: "tcp", src: "10.60.60.20", dst: "10.30.30.20", sport: 40312, dport: 2404 };

function answer(sessions: FirewallSession[]) {
  return new Response(JSON.stringify({ sessions, count: sessions.length }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("firewall session tuples", () => {
  it("turns a listed session into a kill tuple only when both ports are known", () => {
    expect(sessionTuple(CC)).toEqual(CC_TUPLE);
    expect(sessionTuple({ ...KALI, sport: null })).toBeNull();
    expect(sessionTuple({ ...KALI, dport: null })).toBeNull();
  });

  it("recognises the control centre's session by source, RTU and port", () => {
    expect(isControlCentreSession(CC)).toBe(true);
    expect(isControlCentreSession(KALI)).toBe(false);
    expect(isControlCentreSession({ ...CC, dst: "10.30.30.21" })).toBe(false);
  });

  it("requires exactly one control-centre session", () => {
    expect(matchControlCentreSession(SESSIONS.sessions)).toEqual({ kind: "one", tuple: CC_TUPLE });
    expect(matchControlCentreSession([KALI])).toEqual({ kind: "none" });
    expect(matchControlCentreSession([CC, { ...CC, sport: 40313 }])).toEqual({ kind: "several", count: 2 });
    expect(matchControlCentreSession([{ ...CC, sport: null }])).toEqual({ kind: "incomplete" });
    expect(controlCentreMatchText({ kind: "several", count: 2 })).toMatch(/lists 2 tcp 10\.60\.60\.20 → 10\.30\.30\.20:2404 sessions/);
  });

  it("says what a kill did", () => {
    const t = { proto: "tcp", src: "10.10.10.50", dst: "10.30.30.20", sport: 40000, dport: 2404 };
    expect(killResultText(t, null)).toMatch(/Deleted tcp 10\.10\.10\.50:40000 → 10\.30\.30\.20:2404.*only if the policy denies it/);
    expect(killResultText(t, new ApiError(404, "not found"))).toMatch(/already gone/);
    expect(killResultText(t, new ApiError(502, "upstream"))).toMatch(/failed/);
  });
});

describe("findControlCentreSession", () => {
  it("asks containd for the control centre's tcp/2404 sessions and returns the one tuple", async () => {
    const fetchMock = vi.fn().mockResolvedValue(answer([CC]));
    vi.stubGlobal("fetch", fetchMock);
    await expect(findControlCentreSession()).resolves.toEqual(CC_TUPLE);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/firewall\/sessions\?dport=2404&src=10\.60\.60\.20$/);
  });

  it("refuses zero or several matches and says nothing was killed", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(answer([])));
    const none = await findControlCentreSession().catch((e: unknown) => e);
    expect(none).toBeInstanceOf(SessionLookupError);
    expect(cutFailureText(null, none)).toBe("containd lists no tcp 10.60.60.20 → 10.30.30.20:2404 session. Nothing was killed.");

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(answer([CC, { ...CC, sport: 40999 }])));
    const several = await findControlCentreSession().catch((e: unknown) => e);
    expect(several).toBeInstanceOf(SessionLookupError);
    expect(cutFailureText(null, several)).toMatch(/lists 2 .* sessions; the control centre's cannot be told apart\. Nothing was killed\.$/);
  });

  it("reports a failed lookup and a failed kill differently", () => {
    expect(cutFailureText(null, new ApiError(502, "list conntrack returned 502"))).toBe(
      "Could not read the firewall sessions: 502 list conntrack returned 502. Nothing was killed.",
    );
    expect(cutFailureText(CC_TUPLE, new ApiError(404, "no conntrack entry"))).toMatch(/already gone/);
  });
});
