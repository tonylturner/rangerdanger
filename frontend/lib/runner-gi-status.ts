import type { OperatorJournal, OperatorState } from "./operator-api";

// The operator API does not expose eu_lab2_evidence.go's conduitAttemptLimit.
// Keep this display-only fallback separate from command/telemetry health.
export const RUNNER_GI_EVIDENCE_WINDOW_MS = 10 * 60 * 1000;

export function runnerGiStatus(state: OperatorState, journal: OperatorJournal | undefined, now: number) {
  const complete = state.link.gi.last_complete_at;
  const completedAt = complete === null ? NaN : Date.parse(complete);
  const age = now - completedAt;
  const ageText = Number.isFinite(age) && age >= 0 ? `${Math.floor(age / 60000)}m ${Math.floor(age / 1000) % 60}s ago` : "age unavailable";
  let start: number | undefined;
  let matched = false;
  let recent = false;
  if (journal?.cache_epoch === state.cache_epoch && state.link.state === "started" && state.link.connection_seq > 0) {
    for (const entry of [...journal.entries].sort((a, b) => a.seq - b.seq)) {
      if (entry.detail.connection_seq !== state.link.connection_seq || entry.seq < (journal.oldest_seq ?? 0)) continue;
      const at = Date.parse(entry.at);
      if (entry.kind === "gi_started") start = at;
      if (entry.kind === "gi_complete" && start !== undefined && at >= start && at <= now) {
        matched = true;
        recent ||= now - start <= RUNNER_GI_EVIDENCE_WINDOW_MS;
      }
    }
  }
  return {
    value: `${complete ? "GI complete" : state.link.gi.state.toUpperCase()} · ${ageText} · ${matched ? `current connection #${state.link.connection_seq}` : "current connection unverified"}`,
    fresh: matched && recent && age >= 0 && age <= RUNNER_GI_EVIDENCE_WINDOW_MS,
  };
}
