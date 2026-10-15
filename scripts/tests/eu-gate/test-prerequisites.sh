#!/usr/bin/env bash
# Unmet prerequisites are FAIL: a failed G11 fails G12 (run 1 reported a
# silent G12 PASS), and G8/G9 fail without G7's verified disconnect.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

EPHEMERAL_LOW=32768
EPHEMERAL_HIGH=60999
DOCKER_LOG="$TMP_DIR/docker-calls.log"
OPENDSS_STATE=false
: >"$DOCKER_LOG"
docker() {
  printf '%s\n' "$*" >>"$DOCKER_LOG"
  case "$1" in
    pause) OPENDSS_STATE=true ;;
    unpause) OPENDSS_STATE=false ;;
    inspect) printf '%s\n' "$OPENDSS_STATE" ;;
    *) return 99 ;;
  esac
}
eu_wait_for_operator() {
  printf 'waited for operator: %s\n' "$3"
}
eu_command() { pass "intentional no_measurement rejection (stub)"; }
eu_apply_named() {
  pass "$1 applied (stub)"
}
# The held G7/G11 readback comparison fails as it did in run 1.
eu_apply_custom() {
  fail "containd effective firewall rules differ from custom $2 before session kill"
  return 1
}
gate() {
  gate_start "$1"
  "eu_gate_$1"
  gate_end "$1"
}

t_run gate G11
t_expect_out "GATE G11 FAIL" "the held custom-policy readback failure fails G11"
t_expect_eq "$g11_loss_verified" 0 "G11 records no verified Modbus loss"
t_run gate G12
t_expect_fails 1 "G12 fails when G11 did not verify loss and recovery"
t_expect_out "GATE G12 FAIL" "G12 is reported as FAIL, not PASS"
t_expect_out "without G11's verified Modbus loss" "the G12 failure names the unmet G11 prerequisite"
t_expect_eq "$(cat "$DOCKER_LOG")" "" "G12 neither pauses nor unpauses OpenDSS without its prerequisite"

g11_loss_verified=2
t_run gate G12
t_expect_fails 0 "G12 runs when G11 verified loss and recovery"
t_expect_out "GATE G12 PASS" "G12 passes with its prerequisite and both quality transitions"
t_expect_eq "$OPENDSS_STATE" false "G12 unpaused the OpenDSS container it paused"

g7_loss_verified=0
t_run gate G8
t_expect_fails 1 "G8 fails without the verified G7 disconnect"
t_run gate G9
t_expect_fails 1 "G9 fails without the verified G7 disconnect"
t_expect_out "without the verified G7 CC disconnect" "the G9 failure names the unmet G7 prerequisite"
t_expect_no_out "waited for operator" "G9 does not wait for a recovery that cannot be attributed"

t_finish
