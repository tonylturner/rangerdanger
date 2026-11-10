import { describe, expect, it } from "vitest";
import { FRESH_STATE, NOW } from "./operator-fixtures.test-util";
import type { OperatorJournal } from "./operator-api";
import { runnerGiStatus, RUNNER_GI_EVIDENCE_WINDOW_MS } from "./runner-gi-status";

const journal: OperatorJournal = {
  cache_epoch: FRESH_STATE.cache_epoch, oldest_seq: 1, latest_seq: 2,
  entries: [
    { seq: 1, kind: "gi_started", at: FRESH_STATE.link.gi.started_at!, detail: { connection_seq: FRESH_STATE.link.connection_seq } },
    { seq: 2, kind: "gi_complete", at: FRESH_STATE.link.gi.last_complete_at!, detail: { connection_seq: FRESH_STATE.link.connection_seq } },
  ],
};

describe("runner GI evidence hint", () => {
  it("shows age and a matched current connection", () => {
    expect(runnerGiStatus(FRESH_STATE, journal, NOW)).toEqual({
      value: "GI complete · 0m 31s ago · current connection #3", fresh: true,
    });
  });
  it("expires when the GI start leaves the evidence window, even if completion is newer", () => {
    const at = Date.parse(FRESH_STATE.link.gi.started_at!) + RUNNER_GI_EVIDENCE_WINDOW_MS + 1;
    expect(runnerGiStatus(FRESH_STATE, journal, at).fresh).toBe(false);
  });
  it.each([
    undefined,
    { ...journal, cache_epoch: "another-epoch" },
    { ...journal, entries: journal.entries.slice(1) },
    { ...journal, oldest_seq: 2 },
    { ...journal, entries: journal.entries.map((entry) => ({ ...entry, detail: { connection_seq: FRESH_STATE.link.connection_seq + 1 } })) },
  ])("does not claim current-connection evidence without a retained matched pair %#", (evidence) => {
    expect(runnerGiStatus(FRESH_STATE, evidence, NOW)).toMatchObject({ fresh: false, value: expect.stringContaining("unverified") });
  });
  it("does not present a disconnected link as current evidence", () => {
    expect(runnerGiStatus({ ...FRESH_STATE, link: { ...FRESH_STATE.link, state: "disconnected" } }, journal, NOW).fresh).toBe(false);
  });
});
