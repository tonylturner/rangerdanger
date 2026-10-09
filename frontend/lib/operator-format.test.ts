import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { FRESH_POINTS, STALE_STATE, TRUTH, commandResult } from "./operator-fixtures.test-util";
import {
  commandErrorText,
  commandOutcome,
  deviceStateSummary,
  formatAge,
  formatPointValue,
  formatUtcTime,
  journalRangeText,
  journalSummary,
  pointCondition,
  qualityFlags,
  staleReasonText,
  telemetrySummary,
  telemetryText,
} from "./operator-format";
import { RangeNotReadyError } from "./range";

describe("point condition and value", () => {
  it("formats every point kind", () => {
    expect(formatPointValue("feeder-breaker-position", "closed")).toBe("CLOSED");
    expect(formatPointValue("feeder-protection-trip", true)).toBe("TRIPPED");
    expect(formatPointValue("feeder-protection-trip", false)).toBe("NORMAL");
    expect(formatPointValue("feeder-current", 28.93)).toBe("28.9 A");
    expect(formatPointValue("feeder-voltage", 19.94)).toBe("19.94 kV");
    expect(formatPointValue("other", true)).toBe("ON");
  });

  it("keeps the last value of a stale point, never zero", () => {
    for (const [i, point] of STALE_STATE.points.entries()) {
      expect(pointCondition(point)).toBe("stale");
      expect(formatPointValue(point.id, point.value)).toBe(formatPointValue(FRESH_POINTS[i].id, FRESH_POINTS[i].value));
    }
    expect(formatPointValue("feeder-current", STALE_STATE.points[2].value)).toBe("28.9 A");
  });

  it("separates good, stale and never received", () => {
    expect(pointCondition(FRESH_POINTS[0])).toBe("good");
    expect(pointCondition({ ...FRESH_POINTS[2], quality: { ...FRESH_POINTS[2].quality, iv: true }, stale_reason: "measurement_invalid" })).toBe("stale");
    const never = { ...FRESH_POINTS[0], value: null, stale_reason: "never_received" as const };
    expect(pointCondition(never)).toBe("never");
    expect(formatPointValue(never.id, never.value)).toBe("not received");
  });

  it("lists quality flags as text in IEC order and skips OV on status points", () => {
    expect(qualityFlags({ iv: true, nt: true, sb: false, bl: true, ov: null })).toEqual(["IV", "NT", "BL"]);
    expect(qualityFlags({ iv: false, nt: false, sb: true, bl: false, ov: true })).toEqual(["SB", "OV"]);
    expect(qualityFlags(FRESH_POINTS[0].quality)).toEqual([]);
  });

  it("explains every stale reason", () => {
    expect(staleReasonText("link_lost")).toMatch(/link to the RTU lost/);
    expect(staleReasonText("device_unreachable")).toMatch(/Modbus/);
    expect(staleReasonText("measurement_invalid")).toMatch(/invalid/);
    expect(staleReasonText("never_received")).toBe("Never received");
    expect(staleReasonText(null)).toBe("");
  });

  it("summarises telemetry", () => {
    expect(telemetryText(telemetrySummary(FRESH_POINTS))).toBe("4/4 good");
    expect(telemetryText(telemetrySummary(STALE_STATE.points))).toBe("0/4 good · 4 STALE");
  });
});

describe("times", () => {
  it("shows UTC wall time with milliseconds", () => {
    expect(formatUtcTime("2026-10-09T12:00:00.120Z")).toBe("12:00:00.120");
    expect(formatUtcTime(null)).toBe("—");
  });

  it("formats ages from seconds to hours", () => {
    expect(formatAge(null)).toBe("—");
    expect(formatAge(400)).toBe("0.4 s");
    expect(formatAge(42_000)).toBe("42 s");
    expect(formatAge(185_000)).toBe("3 min 05 s");
    expect(formatAge(3_720_000)).toBe("1 h 02 min");
  });
});

describe("commands", () => {
  it("names every command status", () => {
    expect(commandOutcome(commandResult("completed"))).toMatchObject({ tone: "ok", title: "COMPLETED" });
    expect(commandOutcome(commandResult("completed")).explanation).toContain("OPEN");
    expect(commandOutcome(commandResult("rejected"))).toMatchObject({ tone: "bad", title: "REJECTED" });
    expect(commandOutcome(commandResult("rejected")).explanation).toContain("execute");
    expect(commandOutcome({ ...commandResult("rejected"), select: { status: "negative", at: null } }).explanation).toContain("select");
    expect(commandOutcome(commandResult("failed"))).toMatchObject({ tone: "bad", title: "FAILED" });
    expect(commandOutcome(commandResult("failed")).explanation).toContain("select got no confirmation");
    expect(commandOutcome(commandResult("unknown"))).toMatchObject({ tone: "warn", title: "UNKNOWN" });
  });

  it("explains a command the control centre refused", () => {
    expect(commandErrorText(new ApiError(409, "command in flight"))).toMatch(/still in progress: command in flight/);
    expect(commandErrorText(new ApiError(503, "link not started"))).toMatch(/no started link.*Nothing was sent/);
    expect(commandErrorText(new ApiError(400, ""))).toMatch(/malformed/);
    expect(commandErrorText(new ApiError(504, ""))).toMatch(/outcome is not known/);
    expect(commandErrorText(new RangeNotReadyError("starting"))).toMatch(/range is being replaced/);
  });
});

describe("journalSummary", () => {
  it("summarises every journal kind", () => {
    expect(journalSummary("link_up", { peer: "10.30.30.20:2404", connection_seq: 3 })).toBe("Link up #3 to 10.30.30.20:2404");
    expect(journalSummary("link_down", { peer: "10.30.30.20:2404", connection_seq: 3, reason: "t1_timeout" })).toBe(
      "Link down #3: nothing received from the RTU for t1 (t1_timeout)",
    );
    expect(journalSummary("gi_complete", { connection_seq: 4, points_good: 4 })).toContain("4 points good");
    expect(journalSummary("point_stale", { point: "feeder-current", stale_reason: "link_lost", value: 28.93 })).toBe(
      "Feeder current STALE (IEC 104 link to the RTU lost), keeps 28.9 A",
    );
    expect(journalSummary("point_good", { point: "feeder-breaker-position", value: "open" })).toBe("Feeder breaker position good: OPEN");
    expect(
      journalSummary("command", {
        id: "c1",
        point: "feeder-breaker-command",
        operation: "open",
        status: "completed",
        select: { status: "confirmed" },
        execute: { status: "confirmed" },
        feedback: { status: "matched" },
      }),
    ).toBe("Command OPEN COMPLETED (select confirmed, execute confirmed, feedback matched)");
    expect(journalSummary("new_kind", { a: 1 })).toBe('new_kind {"a":1}');
  });

  it("explains every link_down reason the control centre emits", () => {
    const reason = (r: string) => journalSummary("link_down", { connection_seq: 0, reason: r });
    expect(reason("peer_closed")).toBe("Link down #0: the RTU end closed or reset the connection (peer_closed)");
    expect(reason("connect_failed")).toBe("Link down #0: TCP connect or STARTDT failed (connect_failed)");
    expect(reason("io_error")).toContain("failed general interrogation (io_error)");
    expect(reason("later_reason")).toBe("Link down #0: later_reason");
  });

  it("names an empty journal, whose bounds are null", () => {
    expect(journalRangeText(null, null)).toBe("empty");
    expect(journalRangeText(1, 50)).toBe("seq 1–50");
  });
});

describe("instructor", () => {
  it("summarises the device state a fault or reset answered with", () => {
    expect(deviceStateSummary(TRUTH.device)).toBe("breaker OPEN, fault YES, lockout YES, protection tripped YES, revision 5");
  });
});
