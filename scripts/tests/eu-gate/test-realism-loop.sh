#!/usr/bin/env bash
# The positive PLC conduit proof must work after nominal auto has left tap 0.
# shellcheck disable=SC2329 # stubs invoked by sourced helper
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
OPERATOR=$(jq -c '(.points[] | select(.id == "oltc-1-tap") | .value) = {value:2,transient:false}
  | (.points[] | select(.id == "oltc-1-bus-kv") | .value) = 20.429' "$T_FIXTURES/operator-gi-untimed.json")
CALLS=""
eu_lab_exec() { :; }
eu_lab_cc() { CALLS="$CALLS $1:$2"; }
eu_get_operator() {
  # shellcheck disable=SC2034 # read by sourced eu_lab_positive_conduits
  API_BODY="$OPERATOR"
}
eu_fresh_points() {
  printf '%s' "$OPERATOR" | jq -e "$EU_JQ_DEFS $1" >/dev/null
}
t_run eu_lab_positive_conduits
t_expect_eq "$T_STATUS" 0 "positive conduit loop accepts an already regulated +2 baseline"
t_expect_eq "$CALLS" ' oltc-1-mode:manual oltc-1-step:lower oltc-1-step:lower oltc-1-mode:auto' \
  "two completed lower commands precede the automatic correction"
set_tap() {
  OPERATOR=$(printf '%s' "$OPERATOR" | jq -c --argjson tap "$1" \
    '(.points[] | select(.id == "oltc-1-tap") | .value.value) = $tap')
}
set_tap 1
t_run eu_lab_wait_regulated_cc 2 2026-10-15T12:00:00Z 20.196 20.604 84
t_expect_eq "$T_STATUS" 0 "daily profile may settle at a different in-band tap"
set_tap 0
t_run eu_lab_wait_regulated_cc 2 2026-10-15T12:00:00Z 20.196 20.604 84
t_expect_eq "$T_STATUS" 1 "uncorrected perturbation cannot satisfy recovery on a retained voltage"
set_tap 2
OPERATOR=$(printf '%s' "$OPERATOR" | jq -c '(.points[] | select(.id == "oltc-1-bus-kv") | .value) = 20.0')
t_run eu_lab_wait_regulated_cc 2 2026-10-15T12:00:00Z 20.196 20.604 84
t_expect_eq "$T_STATUS" 1 "a final tap without in-band voltage cannot prove regulation"
t_finish
