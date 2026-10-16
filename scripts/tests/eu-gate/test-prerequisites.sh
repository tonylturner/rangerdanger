#!/usr/bin/env bash
# G11/G12 own their healthy baseline even after a failed gate. G8/G9 still
# require G7's verified disconnect to attribute their cache/recovery evidence.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

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
eu_reset_all() { pass "reset (stub)"; }
BASELINE_FAIL=false
eu_wait_for_baseline() {
  if [ "$BASELINE_FAIL" = true ]; then
    fail "$1 unavailable (stub)"
    return 1
  fi
  pass "$1 (stub)"
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
t_expect_fails 0 "G12 establishes its own baseline after G11 fails"
t_expect_out "GATE G12 PASS" "G12 passes with its own baseline and both quality transitions"
t_expect_out "improved applied (stub)" "G12 restores the conduit even if G11 left it blocked"
t_expect_out "reset (stub)" "G12 resets devices rather than trusting earlier gates"
t_expect_out "G12 GOOD baseline before measurement loss" "G12 waits for operator/truth/solver agreement"
t_expect_eq "$OPENDSS_STATE" false "G12 unpaused the OpenDSS container it paused"

: >"$DOCKER_LOG"
BASELINE_FAIL=true
t_run gate G12
t_expect_fails 1 "G12 fails when its own baseline cannot converge"
t_expect_out "GATE G12 FAIL" "a failed baseline cannot silently pass"
t_expect_eq "$(cat "$DOCKER_LOG")" "" "G12 does not pause OpenDSS without its baseline"
t_run gate G11
t_expect_fails 1 "G11 fails when its own baseline cannot converge"
t_expect_no_out "custom" "G11 does not restrict Modbus without its baseline"

# shellcheck disable=SC2034 # read by eu_gate_G8 and eu_gate_G9
g7_loss_verified=0
t_run gate G8
t_expect_fails 1 "G8 fails without the verified G7 disconnect"
t_run gate G9
t_expect_fails 1 "G9 fails without the verified G7 disconnect"
t_expect_out "without the verified G7 CC disconnect" "the G9 failure names the unmet G7 prerequisite"
t_expect_no_out "waited for operator" "G9 does not wait for a recovery that cannot be attributed"

t_finish
