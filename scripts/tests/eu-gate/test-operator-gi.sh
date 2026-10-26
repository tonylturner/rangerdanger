#!/usr/bin/env bash
# Offline GI barrier and full-GOOD checks.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
OPERATOR=$(cat "$T_FIXTURES/operator-gi-untimed.json")
REQUESTED=2026-10-09T21:02:00Z
POSTS=0
GI_STATUS=202
RESET_EPOCH=0
eu_api_post() {
  if [ "$1" != /api/operator/gi ] || [ -n "$2" ]; then return 1; fi
  POSTS=$((POSTS + 1))
  # shellcheck disable=SC2034 # consumed by the sourced GI helper
  HTTP_STATUS="$GI_STATUS"
  # shellcheck disable=SC2034 # consumed by the sourced GI helper
  API_BODY="{\"trigger\":\"operator\",\"requested_at\":\"$REQUESTED\"}"
}
eu_wait_for_operator() {
  # shellcheck disable=SC2034 # consumed by the sourced GI helper
  WAIT_BODY="$OPERATOR"
  if [ "$POSTS" -gt 0 ]; then
    WAIT_BODY=$(printf '%s' "$WAIT_BODY" | jq -c --arg epoch "$RESET_EPOCH" '
      .link.gi = {state:"complete",trigger:"operator",started_at:"2026-10-09T21:02:00.1Z",
        last_complete_at:"2026-10-09T21:02:00.5Z",next_due_at:"2026-10-09T21:17:00.1Z"}
      | if $epoch == "1" then .cache_epoch = "restarted" else . end')
  fi
  printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $1" >/dev/null 2>&1 || { fail 'GI barrier'; return 1; }
}
t_run eu_interrogate_since 2026-10-09T21:02:00Z 1 fixture
t_expect_eq "$T_STATUS" 0 "GI start after settling and completion pass without receipt advancement"
t_expect_eq "$POSTS" 1 "operator GI requested once, with no body"
POSTS=0 RESET_EPOCH=1
t_run eu_interrogate_since 2026-10-09T21:02:00Z 1 fixture
t_expect_fails 1 "a new epoch cannot complete the accepted GI"
POSTS=0 RESET_EPOCH=0 GI_STATUS=409
t_run eu_interrogate_since 2026-10-09T21:02:00Z 1 fixture
t_expect_fails 1 "GI refusal fails explicitly without retry"
t_expect_eq "$POSTS" 1 "a refused GI is not retried"
POSTS=0 GI_STATUS=202 REQUESTED=2026-10-09T21:01:59Z
t_run eu_interrogate_since 2026-10-09T21:02:00Z 1 fixture
t_expect_fails 1 "GI acceptance before settling fails"
for flag in iv nt sb bl ov; do
  body=$(printf '%s' "$OPERATOR" | jq -c --arg flag "$flag" '.points[0].quality[$flag] = true')
  t_run jq -e "$EU_JQ_DEFS station_good" <<<"$body"
  t_expect_eq "$T_STATUS" 1 "GOOD rejects $flag"
done
# Exercise the real settle predicate without polling a live range.
FIELD=$(cat "$T_FIXTURES/field-baseline.json")
eu_wait_for_field() {
  printf '%s' "$FIELD" | jq -e "$1" >/dev/null 2>&1 || { fail 'unsettled field'; return 1; }
}
sleep() { :; }
t_run eu_settle_for_gi 1 fixture
t_expect_fails 0 "settling requires delivered valid measurements at current revisions"
for expression in '.devices["breaker-1"].moving = true' \
  '.process.truth_stale = true' '.devices["breaker-1"].measurements.valid = false' \
  '.process.devices["feeder-breaker"].solved_revision = -1'; do
  FIELD=$(jq -c "$expression" "$T_FIXTURES/field-baseline.json")
  t_run eu_settle_for_gi 1 fixture
  t_expect_fails 1 "cannot request baseline GI before settling: $expression"
done
t_finish
