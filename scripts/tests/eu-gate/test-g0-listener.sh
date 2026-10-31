#!/usr/bin/env bash
# G0: the GPS keeps its existing wildcard :8080 listener; exactly one is allowed.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SS_OUTPUT=""
docker() {
  [ "$1 $3 $4" = "exec ss -ltn" ] || return 99
  printf '%s\n' "$SS_OUTPUT"
}
ss_table() {
  printf 'State  Recv-Q Send-Q Local Address:Port  Peer Address:Port\n'
  printf 'LISTEN 0      4096   %s   0.0.0.0:*\n' "$@"
}

SS_OUTPUT=$(cat "$T_FIXTURES/gps-ss-run1.txt")
t_run check_listener "$GPS" "*:8080" "GPS"
t_expect_fails 0 "run-1 GPS ss output (*:8080) passes the wildcard check"
t_run check_listener "$GPS" "10.30.30.50:8080" "GPS"
t_expect_fails 1 "run-1 GPS ss output still fails an exact-address requirement"

SS_OUTPUT=$(ss_table "0.0.0.0:8080")
t_run check_listener "$GPS" "*:8080" "GPS"
t_expect_fails 0 "0.0.0.0:8080 counts as the wildcard listener"
SS_OUTPUT=$(ss_table "[::]:8080")
t_run check_listener "$GPS" "*:8080" "GPS"
t_expect_fails 0 "[::]:8080 counts as the wildcard listener"

SS_OUTPUT=$(ss_table "*:8080" "10.30.30.50:8080")
t_run check_listener "$GPS" "*:8080" "GPS"
t_expect_fails 1 "a second listener on :8080 fails"
SS_OUTPUT=$(ss_table "10.30.30.50:8080")
t_run check_listener "$GPS" "*:8080" "GPS"
t_expect_fails 1 "a specific-address listener is not the wildcard"
SS_OUTPUT=$(ss_table "10.30.30.20:2404")
t_run check_listener "$RTU" "$RTU_IP:2404" "RTU"
t_expect_fails 0 "exact-address listeners keep their exact check"

t_finish
