# shellcheck shell=bash
# Offline harness for the EU gate tests: sources the gate (which only defines
# its functions when sourced), gives it a private TMP_DIR, and counts checks.
# Tests stub docker and the API helpers as shell functions; nothing reaches a
# live range.

T_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC2034 # read by the tests that source this file
T_FIXTURES="$T_DIR/fixtures"
T_REPO="$(cd "$T_DIR/../../.." && pwd)"
# shellcheck source-path=SCRIPTDIR source=../../eu-iec104-smoke.sh
. "$T_REPO/scripts/eu-iec104-smoke.sh"
# Evidence time is deterministic/offline, like the API stubs. Clock-specific
# tests reload eu-range.sh and stub docker to exercise the real implementation.
eu_utc() { printf '2026-10-15T12:00:00.500000000Z\n'; }
eu_epoch_seconds() { printf '1760529600.500000000\n'; }
eu_epoch_ns() { printf '1760529600500000000\n'; }

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/eu-gate-test.XXXXXX") || exit 1
T_CHECKS=0
T_ERRORS=0
T_OUT=""
T_FAILS=0
T_STATUS=0

t_cleanup_hook() {
  :
}
# Only call inside a docker stub run by docker_bounded's owned child.
# exec leaves no grandchild holding a command-substitution pipe after timeout.
t_hang_docker() {
  exec python3 -c 'import time; time.sleep(120)'
}
t_cleanup() {
  t_cleanup_hook
  rm -rf "$TMP_DIR"
}
trap t_cleanup EXIT

t_ok() {
  T_CHECKS=$((T_CHECKS + 1))
  printf 'ok     %s\n' "$1"
}
t_not_ok() {
  T_CHECKS=$((T_CHECKS + 1))
  T_ERRORS=$((T_ERRORS + 1))
  printf 'not ok %s\n' "$1" >&2
}

# t_run CMD...: run a gate function in this shell (its globals persist) and
# keep its combined output in T_OUT, its status in T_STATUS and the number of
# FAIL lines it added in T_FAILS.
t_run() {
  local before="$failures"
  "$@" >"$TMP_DIR/t-out" 2>&1
  # shellcheck disable=SC2034 # read by the tests that source this file
  T_STATUS=$?
  T_OUT=$(cat "$TMP_DIR/t-out")
  T_FAILS=$((failures - before))
}
t_expect_fails() {
  if [ "$T_FAILS" -eq "$1" ]; then
    t_ok "$2"
  else
    t_not_ok "$2: expected $1 FAIL line(s), got $T_FAILS; output: $T_OUT"
  fi
}
t_expect_out() {
  case "$T_OUT" in
    *"$1"*) t_ok "$2" ;;
    *) t_not_ok "$2: output lacks '$1'; output: $T_OUT" ;;
  esac
}
t_expect_no_out() {
  case "$T_OUT" in
    *"$1"*) t_not_ok "$2: output contains '$1'; output: $T_OUT" ;;
    *) t_ok "$2" ;;
  esac
}
t_expect_eq() {
  if [ "$1" = "$2" ]; then
    t_ok "$3"
  else
    t_not_ok "$3: expected '$2', got '$1'"
  fi
}
t_finish() {
  printf '%d checks, %d failed\n' "$T_CHECKS" "$T_ERRORS"
  [ "$T_ERRORS" -eq 0 ] && [ "$T_CHECKS" -gt 0 ]
}
