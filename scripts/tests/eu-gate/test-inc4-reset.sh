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

RECEIPT_STATE=protecting
eu_utc() { printf '2026-10-15T12:00:00Z\n'; }
eu_api_post() {
  case "$1" in
    /api/workshop/reset)
      HTTP_STATUS=200
      API_BODY='{"containd":{"applyId":"reset-id","protection":"'"$RECEIPT_STATE"'"},"actions":[{"success":true},{"success":true},{"success":true},{"success":true},{"success":true},{"success":true},{"success":true}]}' ;;
    /api/operator/gi)
      # shellcheck disable=SC2034 # consumed by the sourced GI helper
      HTTP_STATUS=202
      # shellcheck disable=SC2034 # consumed by the sourced GI helper
      API_BODY='{"trigger":"operator","requested_at":"2026-10-15T12:00:00.1Z"}' ;;
    *) return 1 ;;
  esac
}
eu_settle_for_gi() { :; }
OPERATOR=$(jq -c '.link.gi = {state:"complete",trigger:"operator",started_at:"2026-10-15T12:00:00.2Z",last_complete_at:"2026-10-15T12:00:01Z",next_due_at:"2026-10-15T12:15:00.2Z"}
  | (.points[].received_at) = "2026-10-15T12:00:00.5Z"' "$T_FIXTURES/operator-gi-untimed.json")
eu_wait_for_operator() {
  # shellcheck disable=SC2034 # consumed by the sourced GI helper
  WAIT_BODY="$OPERATOR"
  printf '%s' "$OPERATOR" | jq -e "$EU_JQ_DEFS $1" >/dev/null || { fail 'GI barrier not met'; return 1; }
}
t_run eu_reset_all
t_expect_fails 0 "reset accepts operator GI data received before completion"
OPERATOR=$(printf '%s' "$OPERATOR" | jq -c '.points[30].received_at = "2026-10-15T11:59:59Z"')
t_run eu_reset_all
t_expect_fails 0 "reset never waits for every receipt to advance"
OPERATOR=$(printf '%s' "$OPERATOR" | jq -c '.link.gi.started_at = "2026-10-15T11:59:59Z"')
t_run eu_reset_all
t_expect_fails 1 "a GI started before settling cannot pass recovery"
OPERATOR=$(printf '%s' "$OPERATOR" | jq -c '.link.gi.started_at = "2026-10-15T12:00:00.2Z" | .points[0].quality.sb = true')
t_run eu_reset_all
t_expect_fails 1 "reset preserves full GOOD quality requirements"
for RECEIPT_STATE in degraded pending failed disabled; do
  t_run eu_reset_all
  t_expect_eq "$T_STATUS" 1 "reset fails on $RECEIPT_STATE even with seven successful actions"
  t_expect_out "$RECEIPT_STATE" "reset failure retains the observed receipt"
done
t_finish
