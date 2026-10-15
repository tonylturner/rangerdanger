#!/usr/bin/env bash
# Strict schema cutover, attempt-local command evidence and peer poll timing.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
OPERATOR=$(cat "$T_FIXTURES/operator-gi-untimed.json")
edit() { printf '%s' "$OPERATOR" | jq -c "$1"; }
t_run eu_assert_operator_shape "$OPERATOR" fixture
t_expect_fails 0 "all 31 schema-2 points pass"
for expression in '.schema = 1' 'del(.snapshot_seq)' '.points |= .[0:4]' \
  '.points[2].quality.ov = null' '.points[0].quality.ov = false' \
  '(.points[] | select(.ioa == 1301) | .quality.ov) = null' \
  'del(.points[0].cot)' '.link.local = "10.60.60.20:1234"' \
  '(.points[] | select(.ioa == 1301) | .value) = 0'; do
  t_run eu_assert_operator_shape "$(edit "$expression")" fixture
  t_expect_fails 1 "invalid shape fails: $expression"
done
t_run jq -e "$EU_JQ_DEFS four_points and all_good" <<<"$OPERATOR"
t_expect_eq "$T_STATUS" 0 "lab-1 subset survives adding the other 27 points"
stale=$(edit '(.points[] | select(.ioa == 1001 or .ioa == 1002 or .ioa == 2001 or .ioa == 2002)) |= (.quality.iv = true | .quality.nt = true | .stale_reason = "device_unreachable")')
t_run jq -e "$EU_JQ_DEFS all_stale(\"device_unreachable\")" <<<"$stale"
t_expect_eq "$T_STATUS" 0 "one lost breaker conduit does not require stale healthy peers"

RESULT='{"id":"cmd-1","point":"rmu-1-command","operation":"open","result":"completed","reason":null,"selected_at":"2026-10-15T12:00:00.10Z","executed_at":"2026-10-15T12:00:00.20Z","con_at":"2026-10-15T12:00:00.30Z","term_at":"2026-10-15T12:00:00.50Z","feedback":{"ioa":1101,"value":"open","at":"2026-10-15T12:00:00.40Z"}}'
check_result() {
  eu_command_result "$1" rmu-1-command open "${2:-completed}" \
    2026-10-15T12:00:00Z 2026-10-15T12:00:01Z 1101 '"open"'
}
t_run check_result "$RESULT"
t_expect_eq "$T_STATUS" 0 "parsed fractional timestamps prove this attempt (lexical sorting would fail)"
for expression in '.selected_at = "2026-10-15T11:59:59Z"' \
  '.feedback.at = "2026-10-15T12:00:00.01Z"' '.feedback.ioa = 1001' \
  '.feedback.value = "closed"' '.term_at = null' '.con_at = null' \
  '.feedback = null' '.result = "unknown"' '.result = "failed"' \
  '.executed_at = "2026-10-15T12:00:02Z"'; do
  answer=$(printf '%s' "$RESULT" | jq -c "$expression")
  t_run check_result "$answer"
  t_expect_eq "$T_STATUS" 1 "cannot substitute cached, wrong, unknown or unconfirmed evidence: $expression"
done
REJECTED=$(printf '%s' "$RESULT" | jq -c '.result = "rejected" | .feedback = null | .term_at = null')
t_run check_result "$REJECTED" rejected
t_expect_eq "$T_STATUS" 0 "a negative command confirmation is rejection, not failure or unknown"

# Freshness is checked only for the requested observations; a cached final state
# cannot pass, while unrelated devices need not emit a new status ASDU.
eu_wait_for_operator() {
  printf '%s' "$OPERATOR" | jq -e "$EU_JQ_DEFS $1" >/dev/null 2>&1 || {
    fail "not fresh"; return 1;
  }
}
t_run eu_fresh_points 'point("feeder-breaker-position").value == "closed"' 2026-10-09T21:01:55Z 1 fixture
t_expect_fails 0 "a fresh point's actual receive timestamp passes"
t_run eu_fresh_points 'point("feeder-breaker-position").value == "closed"' 2026-10-09T21:02:00Z 1 fixture
t_expect_fails 1 "the same final value received before this attempt fails"

# Isolated timestamp rows represent actual Modbus requests already decoded by
# tshark; duplicates from an any-interface capture must not hide a missing poll.
POLL_ROWS="$TMP_DIR/request-rows.tsv"
jq -r '.devices[] | select(.id != "feeder-breaker") | .host' "$PACKAGE/protocols/points.json" |
while read -r ip; do
  for stamp in 100.1 100.6 101.1 101.6; do printf '%s\t%s\t%s\n' "$stamp" "$ip" "$stamp"; done
done >"$POLL_ROWS"
tshark() { cat "$POLL_ROWS"; }
t_run eu_peer_poll_gaps dummy 100 102
t_expect_fails 0 "every peer has independent request gaps inside its poll budget"
cp "$POLL_ROWS" "$TMP_DIR/requests.good"
grep -v '10.30.30.30' "$TMP_DIR/requests.good" >"$POLL_ROWS"
t_run eu_peer_poll_gaps dummy 100 102
t_expect_fails 1 "a PLC with no actual request cannot pass on a GOOD cached point"
awk '!($1 == "100.6" || $1 == "101.1")' "$TMP_DIR/requests.good" >"$POLL_ROWS"
t_run eu_peer_poll_gaps dummy 100 102
t_expect_fails 1 "a peer delayed beyond poll_ms+250 fails"

t_finish
