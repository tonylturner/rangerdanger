#!/usr/bin/env bash
# shellcheck disable=SC2034 # fixture globals consumed by sourced gate functions
# Commit-only loss: a retained ESTABLISHED row cannot substitute for delivery.
# shellcheck disable=SC2329 # indirect gate stubs
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CALLS=""
DROP=1
eu_window_start() { CALLS="$CALLS capture:$1"; }
eu_window_finish() { CALLS="$CALLS end:$1"; }
COMMITTED=0
eu_apply_named() {
  CALLS="$CALLS apply:$1"
  COMMITTED=1
  EU_READY_MS=1000 EU_READY_EPOCH=1760529600500000000
}
eu_monitor_wait_event() { :; }
eu_monitor_asdu_count() { MONITOR_ASDUS=1; }
eu_monitor_read() {
  MONITOR_LINES=3
  MONITOR_BODY=$(printf '%s\n' \
    "{\"event\":\"connected\",\"local\":null,\"peer\":\"$RTU_IP:2404\"}" \
    '{"event":"gi_complete"}' \
    '{"event":"asdu","at":"2025-10-15T12:00:00.4Z"}')
  if [ "$COMMITTED" -eq 1 ] && [ "$DROP" -eq 0 ]; then
    MONITOR_BODY="$MONITOR_BODY
{\"event\":\"asdu\",\"at\":\"2025-10-15T12:00:01Z\"}"
  fi
}
eu_read_sessions() {
  CALLS="$CALLS tuple:$1"
  SESSION_BODY='{"count":1,"sessions":[{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":44000,"dport":2404,"state":"ESTABLISHED","timeout":400000}]}'
}
eu_monitor_finish() {
  CALLS="$CALLS finish:$1:$3"
  [ "$COMMITTED" = 1 ] && [ "$DROP" = 1 ] || { fail "monitor still receives traffic"; return 1; }
}
eu_kill_tuple() { fail "commit enforcement must not kill conntrack"; return 1; }

t_run eu_g6_commit_monitor_loss
t_expect_eq "$T_STATUS" 0 "commit alone stops monitor although conntrack remains ESTABLISHED"
t_expect_eq "$CALLS" " tuple:2404 capture:G6 apply:improved finish:3:1000 end:G6" "tuple captured before commit; exit bound starts at readiness"
t_expect_out "no ASDU was received after policy readiness" "ASDU delivery, not Sessions presence, is the oracle"
COMMITTED=0 DROP=0 CALLS=""
t_run eu_g6_commit_monitor_loss
t_expect_eq "$T_STATUS" 1 "retained tuple with continued delivery fails"
t_expect_fails 1 "continued delivery is not counted as established-session enforcement"
# Even if the process exits 3, post-readiness ASDUs fail independently.
eu_monitor_finish() { return 0; }
t_run eu_g6_commit_monitor_loss
t_expect_eq "$T_STATUS" 1 "exit 3 cannot hide later ASDUs"

# G7 and G11 must capture/snapshot before commit, never revoke a session.
eu_get_operator() { API_BODY='{"link":{"state":"started"},"points":[]}'; }
eu_make_custom_policy() { :; }
eu_policy_has_no_ephemeral_allow() { return 0; }
eu_session_tuple() { SESSION_TUPLE='[{"state":"ESTABLISHED"}]'; }
eu_window_start() { CALLS="$CALLS capture:$1"; }
eu_window_finish() { CALLS="$CALLS end:$1"; [ "$1" != G11 ]; } # stop G11 before later independent recovery checks
eu_manual_policy_readback() { :; }
LOSS=1
eu_commit_manual_policy() { CALLS="$CALLS commit"; EU_READY_MS=2000; }
eu_check_nft_chain() { return 0; }
eu_wait_for_operator() {
  CALLS="$CALLS loss:$4"
  [ "$LOSS" = 1 ] || { fail "delivery continues with retained row"; return 1; }
}
eu_reset_all() { :; }
eu_wait_for_baseline() { :; }
CALLS=""
t_run eu_gate_G7
t_expect_eq "$g7_loss_verified" 1 "G7 proves commit-only loss without deleting retained row"
t_expect_eq "$CALLS" " tuple:2404 capture:G7 commit loss:2000 end:G7" "G7 capture precedes commit; loss measured from readiness"
LOSS=0 CALLS=""
t_run eu_gate_G7
t_expect_eq "$g7_loss_verified" 0 "G7 cannot count retained entry as passing loss"
LOSS=1 CALLS=""
t_run eu_gate_G11
t_expect_eq "$g11_loss_verified" 1 "G11 proves commit-only Modbus loss without killing tuple"
t_expect_eq "$CALLS" " tuple:502 capture:G11 commit loss:2000 end:G11" "G11 snapshot and capture precede commit; loss bound uses readiness"
LOSS=0 CALLS=""
t_run eu_gate_G11
t_expect_eq "$g11_loss_verified" 0 "G11 cannot count retained dead entry alone as passing"
t_finish
