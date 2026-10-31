#!/usr/bin/env bash
# Historian silence is bracketed by confirmed pause, not CLI completion time.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HISTORY=$(cat "$T_FIXTURES/history-gap.json")
check() {
  eu_historian_gap "$1" 8ac2cdad2dcdc605 2026-10-15T21:37:24.540675Z 2026-10-15T21:37:29.5Z
}
t_run check "$HISTORY"
t_expect_eq "$T_STATUS" 0 "real samples and gap span the confirmed-paused interval"
for expression in '.gaps = []' '.segments[0].cache_epoch = "other"' \
  '.segments[0].samples[0].ingested_at = "2026-10-15T21:37:26Z"' \
  '.segments[0].samples[0].quality.ov = null' '.segments[0].samples = []' \
  '.gaps[0].to = "2026-10-15T21:37:28Z"'; do
  changed=$(printf '%s' "$HISTORY" | jq -c "$expression")
  t_run check "$changed"
  t_expect_eq "$T_STATUS" 1 "cannot fake continuity or silence: $expression"
done
t_finish
