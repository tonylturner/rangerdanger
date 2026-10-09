import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiError, getRange, getWorkshopGraph, requestRange } from "./api";
import {
  INITIAL_OBSERVATION,
  IDLE_POLL_MS,
  RANGE_QUERY_KEY,
  RangeNotReadyError,
  TRANSITION_POLL_MS,
  confirmationConsequences,
  isRangeNotReady,
  isRangeScopedQuery,
  logLineFor,
  confirmationFor,
  observeRange,
  onRangeNotReady,
  parseRangeNotReady,
  rangePollInterval,
  requestKindFor,
  resetRangeScopedQueries,
  transitionPackage,
  type RangeObservation,
  type RangePhase,
  type RangeStatus,
} from "./range";

function status(phase: RangePhase, generation = 1, pkg = "us-dnp3-substation", extra: Partial<RangeStatus> = {}): RangeStatus {
  return { generation, phase, package: pkg, target: "", mode: "source", error: "", updated_at: "2026-10-09T00:00:00Z", ...extra };
}

// Feeds a sequence of GET /api/range answers through observeRange and
// returns, per answer, whether the page would be reset.
function replay(...answers: RangeStatus[]): boolean[] {
  let obs: RangeObservation = INITIAL_OBSERVATION;
  return answers.map((answer) => {
    const { next, changed } = observeRange(obs, answer);
    obs = next;
    return changed;
  });
}

describe("observeRange", () => {
  it("takes the first answer as a baseline", () => {
    expect(replay(status("ready", 4))).toEqual([false]);
  });

  it("keeps the range across steady polls", () => {
    expect(replay(status("ready", 4), status("ready", 4), status("ready", 4))).toEqual([false, false, false]);
  });

  it("resets when the old range stops and again when the new one is ready", () => {
    expect(
      replay(
        status("ready", 4, "us"),
        status("preflight", 4, "us", { target: "eu" }),
        status("stopping", 5, "eu"),
        status("starting", 5, "eu"),
        status("configuring", 5, "eu"),
        status("ready", 5, "eu"),
        status("ready", 5, "eu"),
      ),
    ).toEqual([false, false, true, false, false, true, false]);
  });

  it("does not reset when preflight fails and the old range keeps serving", () => {
    expect(
      replay(status("ready", 4), status("preflight", 4, "us", { target: "eu" }), status("ready", 4, "us", { error: "missing image" })),
    ).toEqual([false, false, false]);
  });

  it("resets on a new ready generation even when the transition was never seen", () => {
    expect(replay(status("ready", 4), status("ready", 6))).toEqual([false, true]);
  });

  it("resets when a page opened mid-transition sees ready", () => {
    expect(replay(status("starting", 5), status("ready", 5))).toEqual([false, true]);
  });

  it("resets when a running range fails", () => {
    expect(replay(status("ready", 4), status("failed", 4))).toEqual([false, true]);
  });

  it("resets only on ready after a start from none or a retried failure", () => {
    expect(replay(status("none", 0, ""), status("preflight", 0, "", { target: "us" }), status("starting", 1), status("ready", 1))).toEqual([
      false,
      false,
      false,
      true,
    ]);
    expect(replay(status("failed", 3), status("stopping", 4), status("ready", 4))).toEqual([false, false, true]);
  });
});

describe("polling", () => {
  it("polls fast only while a transition runs", () => {
    for (const phase of ["preflight", "stopping", "starting", "configuring"] as const) {
      expect(rangePollInterval(phase)).toBe(TRANSITION_POLL_MS);
    }
    for (const phase of ["none", "ready", "failed", undefined] as const) {
      expect(rangePollInterval(phase)).toBe(IDLE_POLL_MS);
    }
  });

  it("names the package being brought up", () => {
    expect(transitionPackage(status("preflight", 1, "us", { target: "eu" }))).toBe("eu");
    expect(transitionPackage(status("stopping", 2, "eu"))).toBe("eu");
  });
});

describe("requests", () => {
  it("offers start, restart, switch and retry by phase", () => {
    expect(requestKindFor(status("none", 0, ""), "us")).toBe("start");
    expect(requestKindFor(status("ready", 1, "us"), "us")).toBe("restart");
    expect(requestKindFor(status("ready", 1, "us"), "eu")).toBe("switch");
    expect(requestKindFor(status("failed", 1, "us"), "us")).toBe("retry");
    expect(requestKindFor(status("failed", 1, "us"), "eu")).toBe("switch");
    expect(requestKindFor(status("starting", 1, "us"), "eu")).toBeNull();
  });

  it("confirms only what replaces a running range", () => {
    expect(confirmationFor("restart", "ready")).toBe("restart");
    expect(confirmationFor("switch", "ready")).toBe("switch");
    expect(confirmationFor("switch", "failed")).toBeNull();
    expect(confirmationFor("start", "none")).toBeNull();
    expect(confirmationFor("retry", "failed")).toBeNull();
  });

  it("states every consequence of a switch", () => {
    const text = confirmationConsequences("switch", "EU IEC 104").join(" ");
    expect(text).toMatch(/container restarts/);
    expect(text).toMatch(/terminals close/i);
    expect(text).toMatch(/firewall policy resets to the EU IEC 104 default/);
    expect(text).toMatch(/progress is kept for each package/);
  });
});

describe("range-scoped queries", () => {
  it("scopes every query except the range status", () => {
    expect(isRangeScopedQuery(RANGE_QUERY_KEY)).toBe(false);
    for (const key of [
      ["packages"],
      ["packages", "active"],
      ["scenarios", "us-dnp3-substation"],
      ["workshop", "graph"],
      ["workshop", "status"],
      ["workshop-status"],
      ["substation-health"],
      ["firewall-rules"],
      ["firewall-active"],
      ["traffic-status"],
      ["pcap-status"],
      ["segmentation", "live-events"],
      ["lab-instances"],
    ]) {
      expect(isRangeScopedQuery(key)).toBe(true);
    }
  });

  it("drops old range data and keeps the range status", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(RANGE_QUERY_KEY, status("ready", 5, "eu"));
    client.setQueryData(["workshop", "graph"], { nodes: ["old"], edges: [] });
    client.setQueryData(["traffic-status"], { generating: true });
    client.setQueryData(["packages", "active"], { id: "us" });

    await resetRangeScopedQueries(client);

    expect(client.getQueryData(RANGE_QUERY_KEY)).toEqual(status("ready", 5, "eu"));
    expect(client.getQueryData(["workshop", "graph"])).toBeUndefined();
    expect(client.getQueryData(["traffic-status"])).toBeUndefined();
    expect(client.getQueryData(["packages", "active"])).toBeUndefined();
    client.clear();
  });
});

describe("not-ready answers", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function respond(statusCode: number, body: unknown) {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(body), { status: statusCode }));
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  it("parses only the range body", () => {
    expect(parseRangeNotReady({ error: "range not ready", phase: "starting" })?.phase).toBe("starting");
    expect(parseRangeNotReady({ error: "range not ready", phase: "bogus" })?.phase).toBeNull();
    expect(parseRangeNotReady({ error: "range is stopping" })?.phase).toBe("stopping");
    expect(parseRangeNotReady({ error: "upstream down" })).toBeNull();
    expect(parseRangeNotReady(null)).toBeNull();
  });

  it("turns a 503 range answer into a calm error and notifies listeners", async () => {
    respond(503, { error: "range not ready", phase: "stopping" });
    const seen: RangeNotReadyError[] = [];
    const stop = onRangeNotReady((e) => seen.push(e));
    const err = await getWorkshopGraph().catch((e: unknown) => e);
    stop();
    expect(isRangeNotReady(err)).toBe(true);
    expect((err as RangeNotReadyError).phase).toBe("stopping");
    expect(seen).toHaveLength(1);
    expect(logLineFor(err)).toMatch(/^\[RANGE\] The range is being replaced \(stopping the current range\)/);
  });

  it("keeps any other 503 an ordinary failure", async () => {
    respond(503, { error: "containd unavailable" });
    const err = await getWorkshopGraph().catch((e: unknown) => e);
    expect(isRangeNotReady(err)).toBe(false);
    expect(err).toBeInstanceOf(ApiError);
    expect(logLineFor(err, "Reset failed: ")).toBe("[ERROR] Reset failed: Request failed: 503");
  });

  it("reads and requests the range", async () => {
    const fetchMock = respond(202, status("preflight", 1, "us", { target: "eu" }));
    const res = await requestRange("eu");
    expect(res.target).toBe("eu");
    expect(fetchMock).toHaveBeenCalledWith("/api/range", expect.objectContaining({ method: "POST", body: '{"package":"eu"}' }));

    respond(200, status("ready", 1));
    expect((await getRange()).phase).toBe("ready");
  });

  it("carries the server's reason on a rejected request", async () => {
    respond(409, { error: "a range transition is already running" });
    const err = await requestRange("eu").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).detail).toBe("a range transition is already running");
  });
});
