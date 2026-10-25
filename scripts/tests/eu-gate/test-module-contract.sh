#!/usr/bin/env bash
# Splitting modules must not turn sourcing or plan-only CLI modes into a run.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SOURCE_CALLS="$TMP_DIR/source-calls"
: >"$SOURCE_CALLS"
source_definitions() (
  # shellcheck disable=SC2329 # invoked only if a guarded command leaks through
  unexpected_command() {
    printf '%s\n' "$1" >>"$SOURCE_CALLS"
    exit 99
  }
  # shellcheck disable=SC2329 # command guards for the newly sourced entry point
  docker() { unexpected_command docker; }
  # shellcheck disable=SC2329
  curl() { unexpected_command curl; }
  # shellcheck disable=SC2329
  tshark() { unexpected_command tshark; }
  # shellcheck disable=SC2329
  python3() { unexpected_command python3; }
  # shellcheck disable=SC2329
  sleep() { unexpected_command sleep; }
  # shellcheck source-path=SCRIPTDIR source=../../eu-iec104-smoke.sh
  . "$T_REPO/scripts/eu-iec104-smoke.sh"
  for gate in G0 G1 G2 G3 G4 G5 G6 G7 G8 G9 G10 G11 G12 G13 G14 G15 G16 G17 G18 G19 G20 G21 G22 G23 G24 G25 G26 G27 G28; do
    declare -F "eu_gate_$gate" >/dev/null || return 1
  done
  if [ "$#" -gt 0 ]; then eu_gate_main "$@"; fi
)

t_run source_definitions
t_expect_eq "$T_STATUS" 0 "sourcing defines all twenty-nine gates without dispatch"
t_expect_eq "$T_OUT" "" "sourcing has no output"
t_expect_eq "$(cat "$SOURCE_CALLS")" "" "sourcing never calls range or capture tools"
t_run source_definitions --list
PLAN="$T_OUT"
t_expect_eq "$T_STATUS" 0 "--list exits successfully without preflight"
t_expect_out "EU IEC104 gate plan" "--list retains the gate plan"
t_expect_out "G20 eu-baseline-assessment validator" "--list includes the baseline validator walk"
t_expect_out "G21 eu-firewall-implementation validator" "--list includes the firewall implementation walk"
t_expect_out "G22 eu-hardening-configurations validator" "--list includes the hardening walk"
t_expect_out "G23 Embedded containd" "--list retains the management gate"
t_expect_out "G24 eu-vendor-rdp-compromise validator" "--list includes the vendor walk"
t_expect_out "G25 eu-validation-evidence validator" "--list includes the validation evidence walk"
t_expect_out "G26 eu-segmentation-requirements" "--list includes the requirements planning slot"
t_expect_out "G27 eu-remediation-planning" "--list includes the remediation planning slot"
t_expect_out "G28 Full and partial compiled plans" "--list includes the golden-backed plan proof gate"
t_run source_definitions --dry-run
t_expect_eq "$T_STATUS" 0 "--dry-run exits successfully without preflight"
t_expect_eq "$T_OUT" "$PLAN" "--dry-run and --list are identical"
t_expect_eq "$(cat "$SOURCE_CALLS")" "" "plan modes never call range or capture tools"
run_order() (
  # shellcheck disable=SC2329 # dispatched by eu_gate_main
  eu_gate_preflight() { :; }
  # shellcheck disable=SC2329
  eu_run_gate() { printf '%s ' "$1"; }
  eu_gate_main
)
t_run run_order
t_expect_eq "$T_STATUS" 0 "stubbed full dispatch completes"
t_expect_out "G19 G20 G21 G22 G23 G24 G25 G26 G27 G28 G14" "existing gates retain order and plan proof runs before final reset"
t_finish
