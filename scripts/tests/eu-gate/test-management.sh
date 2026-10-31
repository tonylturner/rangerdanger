#!/usr/bin/env bash
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
python3 "$ROOT/scripts/tests/eu-gate/test_management.py" || exit 1

# Exercise G23 with the real waiter, but no live probes or wall-clock delays.
# shellcheck disable=SC2329 # stubs invoked by the sourced gate and waiter
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CLOCK=0
POLLS=0
RESETS=0
MODE=delayed
PENDING='{"capturing":true,"file_ready":false,"duration_sec":2}'
READY='{"capturing":false,"file_ready":true,"duration_sec":2}'
STOPPED='{"capturing":false,"file_ready":false,"duration_sec":2}'
eu_now_ms() { printf '%s\n' "$CLOCK"; }
sleep() { CLOCK=$((CLOCK + 1000)); }
python3() {
  case "$1" in
    "$SCRIPT_DIR/lib/containd-ui-probe.py"|"$SCRIPT_DIR/lib/eu-management-probes.py") return 0 ;;
    *) command python3 "$@" ;;
  esac
}
eu_apply_named() { :; }
eu_reset_all() { RESETS=$((RESETS + 1)); }
eu_api_post() {
  [ "$1" = /api/pcap/start ] || return 1
  # shellcheck disable=SC2034 # consumed by the sourced gate
  HTTP_STATUS=200
  API_BODY='{"status":"capturing"}'
}
# shellcheck disable=SC2034 # API_BODY is consumed by the sourced gate and waiter
eu_api_get() {
  case "$1" in
    /api/substation/network-events) API_BODY='{"source":"containd"}' ;;
    /api/firewall/flows) API_BODY='{}' ;;
    /api/pcap/status)
      POLLS=$((POLLS + 1))
      API_BODY="$PENDING"
      case "$MODE" in
        immediate) API_BODY="$READY" ;;
        delayed) if [ "$CLOCK" -ge 4000 ]; then API_BODY="$READY"; fi ;;
        stopped) if [ "$POLLS" -ge 2 ]; then API_BODY="$STOPPED"; fi ;;
      esac
      ;;
    *) return 1 ;;
  esac
}
reset_pcap() {
  MODE="$1"
  CLOCK=0 POLLS=0 RESETS=0
}

reset_pcap delayed
t_run eu_gate_G23
t_expect_eq "$T_STATUS" 0 "G23 accepts readiness on a later poll"
t_expect_eq "$POLLS" 5 "G23 polls again beyond the former three-second sleep"
t_expect_eq "$CLOCK" 4000 "G23 stops polling as soon as the file is ready"
t_expect_fails 0 "delayed readiness produces no failure"
t_expect_out 'G23 backend pcap ready after improved' "delayed readiness is reported"
t_expect_eq "$RESETS" 1 "G23 continues to reset after readiness"

reset_pcap never
t_run eu_gate_G23
t_expect_eq "$T_STATUS" 1 "G23 fails a capture that never becomes ready"
t_expect_eq "$CLOCK" 20000 "G23 bounds the readiness wait to twenty seconds"
t_expect_fails 1 "timeout records a failure"
t_expect_out 'within 20s; last response:' "timeout explains the exhausted budget"
t_expect_out "$PENDING" "timeout includes the last status body"
t_expect_eq "$RESETS" 0 "G23 does not continue after timeout"

reset_pcap stopped
t_run eu_gate_G23
t_expect_eq "$T_STATUS" 1 "G23 fails a stopped capture with no file"
t_expect_eq "$POLLS" 2 "terminal failure does not wait out the budget"
t_expect_eq "$CLOCK" 1000 "terminal failure is immediate on observation"
t_expect_fails 1 "terminal state records a failure"
t_expect_out "$STOPPED" "terminal failure includes the last status body"
t_expect_eq "$RESETS" 0 "G23 does not continue after terminal failure"

reset_pcap immediate
t_run eu_gate_G23
t_expect_eq "$T_STATUS" 0 "G23 accepts an already ready file"
t_expect_eq "$POLLS" 1 "already ready file needs only one poll"
t_expect_eq "$CLOCK" 0 "already ready file needs no sleep"
t_finish
