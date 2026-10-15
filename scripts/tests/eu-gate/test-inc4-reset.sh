#!/usr/bin/env bash
# EXIT must restore all device/process state, not just the old breaker.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CALLS="$TMP_DIR/reset-calls"
outer_tmp="$TMP_DIR"
(
  # shellcheck disable=SC2034 # flags consumed by sourced EXIT handler
  failures=0 passes=0 state_touched=1
  TMP_DIR=$(mktemp -d)
  # shellcheck disable=SC2034 # owned tracer cleanup flag
  PLC_SCAN_HELPER=owned
  # shellcheck disable=SC2329 # called indirectly by eu_cleanup
  eu_stop_plc_scan() { printf 'scan resumed\n' >>"$CALLS"; }
  # shellcheck disable=SC2329 # called indirectly by eu_cleanup
  eu_apply_named() { printf '%s\n' "$1" >>"$CALLS"; }
  # shellcheck disable=SC2329 # called indirectly by eu_cleanup
  eu_reset_all() { printf 'all devices and process\n' >>"$CALLS"; }
  # shellcheck disable=SC2329 # called indirectly by eu_cleanup
  eu_get_field() { API_BODY=$(cat "$T_FIXTURES/field-baseline.json"); }
  eu_cleanup 0
) >"$outer_tmp/cleanup.out" 2>&1
T_STATUS=$?
T_OUT=$(cat "$outer_tmp/cleanup.out")
t_expect_eq "$T_STATUS" 0 "successful EXIT restoration exits zero"
t_expect_eq "$(cat "$CALLS")" "scan resumed
weak
all devices and process" "EXIT restores weak and the ordered multi-device reset"
t_expect_out "cleanup ended on weak policy + device reset" "restoration is verified and reported"

eu_utc() { printf '2026-10-15T12:00:00Z\n'; }
eu_api_post() {
  # shellcheck disable=SC2034 # read by sourced reset helper
  HTTP_STATUS=200
  API_BODY='{"actions":[{"success":true},{"success":true},{"success":true},{"success":true},{"success":true},{"success":true},{"success":true}]}'
}
OPERATOR=$(jq -c '(.points[].received_at) = "2026-10-15T12:00:01Z"' "$T_FIXTURES/operator-gi-untimed.json")
eu_wait_for_operator() {
  printf '%s' "$OPERATOR" | jq -e "$EU_JQ_DEFS $1" >/dev/null || { fail 'not fresh'; return 1; }
}
t_run eu_reset_all
t_expect_fails 0 "reset requires new good observations for all 31 points"
OPERATOR=$(printf '%s' "$OPERATOR" | jq -c '.points[30].received_at = "2026-10-15T11:59:59Z"')
t_run eu_reset_all
t_expect_fails 1 "one cached pre-reset point cannot pass recovery"
t_finish
