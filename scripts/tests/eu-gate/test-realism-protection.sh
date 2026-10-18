#!/usr/bin/env bash
# Protection evidence is solved trip amps, not an instantaneous S2 assumption.
# shellcheck disable=SC2329 # stubs called by sourced gates
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
TRIP_EDIT='.'
F2_CURRENT=2500
eu_apply_named() { :; }
eu_reset_all() { :; }
eu_command() { :; }
eu_action() { :; }
eu_fresh_points() { :; }
eu_wait_for_operator() { :; }
eu_management() {
  # shellcheck disable=SC2034 # consumed by G15 earth-interlock check
  HTTP_STATUS=409
  # shellcheck disable=SC2034 # consumed by G15 earth-interlock check
  API_BODY='{"reason":"way_closed"}'
}
eu_wait_for_field() {
  local field
  case "$3" in
    "G15 s1 trip/FPI and solve")
      field='{"devices":{"breaker-1":{"protection_tripped":true,"lockout":true,
        "protection":{"last_trip_current_a":2700,"instantaneous_a":2000,"pickup_a":400}},
        "rmu-1":{"fault_passage":false,"measurements":{"valid":true}}},"process":{"buses":{"s2":{"live":false}}}}' ;;
    "G15 s2 trip/FPI and solve")
      field='{"devices":{"breaker-1":{"protection_tripped":true,"lockout":true,
        "protection":{"last_trip_current_a":1400,"instantaneous_a":2000,"pickup_a":400}},
        "rmu-1":{"fault_passage":true,"measurements":{"valid":true}}},"process":{"buses":{"s2":{"live":false}}}}' ;;
    "G15 S2 backfeed trips F2")
      field="{\"process\":{\"f2_tripped\":true,\"f2_protection\":{\"last_trip_current_a\":$F2_CURRENT},
        \"buses\":{\"s2\":{\"live\":false},\"s3\":{\"live\":false}}}}" ;;
    *) return 0 ;;
  esac
  field=$(printf '%s' "$field" | jq -c "$TRIP_EDIT") || return
  printf '%s' "$field" | jq -e "$1" >/dev/null
}
t_run eu_gate_G15
t_expect_eq "$T_STATUS" 0 "S1 I>>, delayed S2 I> and F2 back-feed I>> grading pass"
TRIP_EDIT='.devices["breaker-1"].protection.last_trip_current_a = null'
t_run eu_gate_G15
t_expect_eq "$T_STATUS" 1 "a trip latch without solved trip amps cannot pass"
TRIP_EDIT='.devices["breaker-1"].protection.instantaneous_a = null'
t_run eu_gate_G15
t_expect_eq "$T_STATUS" 1 "missing protection settings cannot pass"
TRIP_EDIT='.'
F2_CURRENT=1400
t_run eu_gate_G15
t_expect_eq "$T_STATUS" 1 "F2 back-feed must meet the profile instantaneous setting"
t_finish
