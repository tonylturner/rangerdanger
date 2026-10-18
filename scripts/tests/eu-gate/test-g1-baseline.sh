#!/usr/bin/env bash
# G1: untimed GI status points (null source time) are a GOOD baseline; the
# baseline is polled until operator, truth and solver agree.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OPERATOR=$(cat "$T_FIXTURES/operator-gi-untimed.json")
TRUTH=$(cat "$T_FIXTURES/field-baseline.json")
edit_operator() {
  printf '%s' "$OPERATOR" | jq -c "$EU_JQ_DEFS $1"
}
edit_truth() {
  printf '%s' "$TRUTH" | jq -c "$1"
}

t_run jq -e '.process.buses.s1.kv != .devices["breaker-1"].measurements.voltage_kv_ll' <<<"$TRUTH"
t_expect_fails 0 "CB1 line-side VT fixture is distinct from the S1 process bus"
t_run jq -e '[.points[] | select(.id == "feeder-voltage" or .id == "rmu-1-source-kv")
  | [.id, .value]] | from_entries | .["feeder-voltage"] != .["rmu-1-source-kv"]' <<<"$OPERATOR"
t_expect_fails 0 "operator feeder VT is distinct from RMU1's S1 source voltage"

t_run eu_baseline_mismatches "$OPERATOR" "$TRUTH"
t_expect_eq "$T_OUT" "" "untimed GI (COT 20, null source_time) agreeing with truth has no mismatch"
t_run eu_assert_operator_shape "$OPERATOR" "fixture"
t_expect_fails 0 "untimed GI fixture has the operator-state shape"

timed=$(edit_operator '(.points[] | select(.id == "feeder-breaker-position"))
  |= (.asdu = "M_DP_TB_1" | .cot = 3 | .source_time = "2026-10-09T21:02:00.000Z" | .source_time_invalid = true)')
t_run eu_baseline_mismatches "$timed" "$TRUTH"
t_expect_eq "$T_OUT" "" "a timed spontaneous status report is accepted; synchronisation is G2's check"

t_run eu_baseline_mismatches "$(edit_operator '(.points[] | select(.id == "feeder-protection-trip"))
  |= (.source_time = "2026-10-09T21:02:00.000Z" | .source_time_invalid = false)')" "$TRUTH"
t_expect_out "feeder-protection-trip time tag does not fit COT 20" "a GI (COT 20) report with a time tag is a mismatch"
t_run eu_baseline_mismatches "$(edit_operator '(.points[] | select(.id == "feeder-breaker-position"))
  |= (.cot = 3)')" "$TRUTH"
t_expect_out "time tag does not fit COT 3" "a spontaneous report without a time tag is a mismatch"

t_run eu_baseline_mismatches "$OPERATOR" "$(edit_truth '.devices["breaker-1"].revision = 10')"
t_expect_out "solver has not delivered breaker revision 10" "solver lagging the reset revision is a mismatch"
t_run eu_baseline_mismatches "$OPERATOR" "$(edit_truth '.process.truth_stale = true')"
t_expect_out "solver truth_stale" "stale solver truth is reported"
t_run eu_baseline_mismatches "$OPERATOR" "$(edit_truth '.devices["breaker-1"].measurements.current_a = 30.0')"
t_expect_out "current A (tolerance 0.5): operator 28.87, truth 30" "current outside 0.5 A is reported with both values"
t_run eu_baseline_mismatches "$OPERATOR" "$(edit_truth '.devices["breaker-1"].position = "open"')"
t_expect_out 'position: operator "closed", truth "open"' "a position mismatch is reported"
t_run eu_baseline_mismatches "$(edit_operator '(.points[] | select(.id == "feeder-voltage"))
  |= (.quality.iv = true | .stale_reason = "measurement_invalid")')" "$TRUTH"
t_expect_out 'feeder-voltage not GOOD' "a point that is not GOOD is reported"
t_run eu_baseline_mismatches "$(edit_operator '.link.gi.state = "running"')" "$TRUTH"
t_expect_eq "$T_OUT" "" "a later running GI preserves integrity"
t_run eu_baseline_mismatches "$(edit_operator '.link.gi.last_complete_at = null')" "$TRUTH"
t_expect_out 'GI integrity missing' "missing integrity is reported"

# Isolate convergence from the separately tested GI barrier.
eu_settle_for_gi() { :; }
eu_interrogate_since() { :; }
# Poll: the solver catches up with the reset revision on the third read.
POLLS=0
LAGGING=$(edit_truth '.devices["breaker-1"].revision = 10')
CAUGHT_UP=$(edit_truth '.devices["breaker-1"].revision = 10 | .process.devices["feeder-breaker"].solved_revision = 10')
eu_api_get() {
  case "$1" in
    /api/operator/state) API_BODY="$OPERATOR" ;;
    /api/field/state)
      POLLS=$((POLLS + 1))
      # shellcheck disable=SC2034 # read by eu_wait_for_baseline
      if [ "$POLLS" -lt 3 ]; then API_BODY="$LAGGING"; else API_BODY="$CAUGHT_UP"; fi
      ;;
    *) return 1 ;;
  esac
}
t_run eu_wait_for_baseline "G1 test baseline" 5
t_expect_fails 0 "the baseline converges once the solver accepts the reset revision"
t_expect_eq "$POLLS" 3 "the baseline was polled until it converged"
t_expect_eq "$BASELINE_TRUTH" "$CAUGHT_UP" "the converged field snapshot is kept"

POLLS=-1000
t_run eu_wait_for_baseline "G1 test baseline" 1
t_expect_fails 1 "a baseline that never converges fails after its budget"
t_expect_out "mismatch: solver has not delivered breaker revision 10" "the timeout prints each mismatch"
t_expect_out "operator snapshot: {" "the timeout prints the operator snapshot"
t_expect_out "field snapshot: {" "the timeout prints the field snapshot"

t_finish
