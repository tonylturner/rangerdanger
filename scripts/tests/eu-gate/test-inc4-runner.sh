#!/usr/bin/env bash
# Returning before a protocol attempt completed must never become GATE PASS.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
# shellcheck disable=SC2329 # invoked dynamically by eu_run_gate
eu_gate_G15() { return 1; }
t_run eu_run_gate G15
t_expect_fails 1 "a bare early failure cannot silently pass"
t_expect_out 'GATE G15 FAIL' "the runner names the failed gate"
# shellcheck disable=SC2329 # invoked dynamically by eu_run_gate
eu_gate_G15() { pass 'partial setup'; return 1; }
t_run eu_run_gate G15
t_expect_fails 1 "a successful setup does not hide an incomplete attempt"
# shellcheck disable=SC2329 # invoked dynamically by eu_run_gate
eu_gate_G15() { fail 'specific protocol evidence'; return 1; }
t_run eu_run_gate G15
t_expect_fails 1 "an already explained failure is not counted twice"
calls=""
eu_unpause_owned() { calls="$calls $2:$3"; }
# shellcheck disable=SC2034 # read by the sourced resource teardown helper
gps_paused=1 opendss_paused=1 rtu_paused=1 cc_paused=1
t_run eu_gate_resources_finish G2
t_expect_fails 0 "failed attempts unwind owned paused services"
t_expect_eq "$calls" " gps:1 opendss:1 rtu:1 cc:1" "all four service pauses are unwound between gates"
t_finish
