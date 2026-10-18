#!/usr/bin/env bash
# Real G8 regulation phase with synthetic field/audit traces and a fake clock.
# shellcheck disable=SC2329 # stubs are invoked by sourced gate helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OPERATOR=$(jq -c '.link.state = "disconnected"
  | .points[] |= (.quality.iv = true | .quality.nt = true | .stale_reason = "link_lost")' \
  "$T_FIXTURES/operator-gi-untimed.json")
EPOCH=$(jq -r '.cache_epoch' <<<"$OPERATOR")
POINTS=$(eu_get_point_snapshot "$OPERATOR" station)
BASE_FIELD=$(cat "$T_FIXTURES/field-baseline.json")
BASE_AUDIT='{"generation":42,"oldest_seq":1,"latest_seq":22,"entries":[
  {"seq":21,"operation":"plc_request","result":"accepted",
   "context":{"protocol":"modbus","peer":"10.30.30.30:43000"},
   "before":{"tap":2},"after":{"tap":2,"target_tap":1,"moving":true,"revision":11}},
  {"seq":22,"operation":"motion_complete","result":"accepted",
   "before":{"tap":2,"target_tap":1,"moving":true},
   "after":{"tap":1,"moving":false,"revision":12}}]}'

eu_now_ms() { printf '%s\n' "$CLOCK"; }
sleep() { CLOCK=$((CLOCK + CLOCK_STEP)); }
eu_get_operator() {
  API_BODY="$OPERATOR"
  if [ "$CLOCK" -ge "$CHANGE_AT" ]; then
    API_BODY=$(jq -c "$CACHE_EDIT" <<<"$API_BODY")
  fi
  eu_assert_operator_shape "$API_BODY" "G8 test"
}
eu_get_field() {
  SOLVE_SEQ=$((SOLVE_SEQ + 1))
  # shellcheck disable=SC2034 # consumed by the real G8 helpers
  API_BODY=$(jq -c --argjson clock "$CLOCK" --argjson trace "$TRACE" \
    --argjson source "$SOURCE" --argjson profile "$PROFILE" --argjson tripped "$TRIPPED" \
    --argjson seq "$SOLVE_SEQ" --argjson bus "$BUS" '
    ($trace | map(select(.at <= $clock)) | last) as $step
    | .process.source_pu = $source | .process.profile.enabled = $profile
    | .process.solve_seq = $seq
    | .devices["oltc-1"] = {tap:$step.tap,moving:($step.moving // false),generation:42,
        revision:12,auto:true,remote_enabled:true,blocked:false,
        measurements:{valid:true,voltage_kv_ll:$bus}}
    | .devices["breaker-1"] |=
        (.position = (if $tripped then "open" else "closed" end)
         | .lockout = $tripped | .protection_tripped = $tripped
         | .measurements.current_a = (if $tripped then 0 else 28.88 end))
    | .process.buses.s1.live = ($tripped | not)
  ' <<<"$BASE_FIELD")
}
eu_set_profile() { ACTIONS="$ACTIONS profile:$1"; PROFILE="$1"; }
eu_action() {
  [ "$1:$2" = process:set_source ] || return 99
  ACTIONS="$ACTIONS source:$3"
  SOURCE="$3"
}
eu_management() {
  [ "$1:$2" = oltc-1:GET ] || return 99
  case "$3" in
    '/api/audit?after=0') API_BODY='{"generation":42,"latest_seq":20}' ;;
    '/api/audit?after=20')
      CLOCK=$((CLOCK + AUDIT_DELAY))
      API_BODY=$(jq -c "$AUDIT_EDIT" <<<"$AUDIT")
      ;;
    *) return 99 ;;
  esac
}
# Fail immediately if the down-CC phase tries a reset or a CC command.
eu_reset_all() { fail "forbidden CC-dependent reset"; return 99; }
eu_reference_tap() { fail "forbidden CC-dependent tap positioning"; return 99; }
eu_command() { fail "forbidden CC command"; return 99; }

reset_trace() {
  TRACE='[{"at":0,"tap":2},{"at":30000,"tap":2,"moving":true},{"at":32000,"tap":1}]'
  if [ "$#" -gt 0 ]; then TRACE="$1"; fi
  CLOCK=0 CLOCK_STEP=13000 SOURCE=1 PROFILE=true ACTIONS="" SOLVE_SEQ=23
  TRIPPED=false BUS=21.5 CHANGE_AT=13000 CACHE_EDIT='.' AUDIT_EDIT='.'
  AUDIT="$BASE_AUDIT" AUDIT_DELAY=0
}
phase() { eu_g8_regulate_without_cc "$EPOCH" "$POINTS"; }
expect_restored() {
  t_expect_eq "$SOURCE" 1 "source restored without CC"
  t_expect_eq "$PROFILE" true "profile restored without another demand cycle"
}

reset_trace
t_run phase
t_expect_eq "$T_STATUS" 0 "a completed PLC-origin step passes with the CC cache unchanged"
t_expect_fails 0 "autonomous regulation has no failed assertions"
t_expect_out "PLC-origin tap step toward the band completed" "G8 reports protocol-attributed completion"
t_expect_eq "$CLOCK" 39000 "G8 stops after the first observed completed step"
t_expect_eq "$ACTIONS" ' profile:false source:1.10 source:1 profile:true' \
  "only management-side drivers are used and both are restored"
expect_restored

reset_trace '[{"at":0,"tap":2}]'
t_run phase
t_expect_eq "$T_STATUS" 1 "no completed step fails"
t_expect_out "within one demand cycle" "stall failure names the single shared budget"
t_expect_eq "$CLOCK" 52000 "reference delay plus motion plus observation slack bounds the phase"
expect_restored

for CACHE_EDIT in \
  '.points[0].value = "open"' \
  '.points[-1].received_at = "2026-10-18T12:00:00Z"' \
  '.points[0].source_time = "2026-10-18T12:00:00Z"' \
  '.cache_epoch = "restarted"' \
  '.link.state = "started"'; do
  saved_edit="$CACHE_EDIT"
  reset_trace
  CACHE_EDIT="$saved_edit"
  t_run phase
  t_expect_eq "$T_STATUS" 1 "cache mutation or reconnect fails during regulation: $CACHE_EDIT"
  t_expect_out "operator cache changed or CC link reconnected" "isolation failure is explicit"
  expect_restored
done

for AUDIT_EDIT in \
  '.entries[0].context.peer = "10.30.30.20:43000"' \
  '.entries[0].result = "ignored"' \
  '.entries[0].seq = 20' \
  '.generation = 43' \
  '.oldest_seq = 22' \
  '.entries[0].after.target_tap = 3' \
  '.entries |= .[0:1]' \
  '.entries[1].after.tap = 2' \
  '.entries[1].after.moving = true'; do
  saved_edit="$AUDIT_EDIT"
  reset_trace
  AUDIT_EDIT="$saved_edit"
  t_run phase
  t_expect_eq "$T_STATUS" 1 "field tap alone cannot replace PLC completion evidence: $AUDIT_EDIT"
  expect_restored
done

reset_trace '[{"at":0,"tap":2},{"at":32000,"tap":3}]'
t_run phase
t_expect_eq "$T_STATUS" 1 "wrong-way field movement is not regulation toward the band"
expect_restored

reset_trace '[{"at":0,"tap":2},{"at":32000,"tap":1,"moving":true}]'
t_run phase
t_expect_eq "$T_STATUS" 1 "a moving tap is not a completed step"
expect_restored

reset_trace
BUS=20.4
t_run phase
t_expect_eq "$T_STATUS" 1 "source acceptance without observed out-of-band voltage cannot prove demand"
expect_restored

reset_trace
AUDIT_DELAY=14000
t_run phase
t_expect_eq "$T_STATUS" 1 "completion evidence arriving beyond the budget fails"
t_expect_out "exceeded the demand-cycle deadline" "audit latency counts against the same budget"
expect_restored

# Initial motion is allowed to finish, but it is not the test's new PLC step.
reset_trace '[{"at":0,"tap":1,"moving":true},{"at":2000,"tap":2},
  {"at":34000,"tap":1}]'
CLOCK_STEP=1000
t_run phase
t_expect_eq "$T_STATUS" 0 "already accepted motion is drained without CC commands"
t_expect_eq "$CLOCK" 34000 "preparation and the new PLC step share the deadline"

# The low-source alternative leaves headroom at the lower tap limit.
reset_trace '[{"at":0,"tap":-8},{"at":32000,"tap":-7}]'
BUS=19
AUDIT=$(jq -c '.entries[0].before.tap = -8 | .entries[0].after.target_tap = -7
  | .entries[1].before.target_tap = -7 | .entries[1].after.tap = -7' <<<"$BASE_AUDIT")
t_run phase
t_expect_eq "$T_STATUS" 0 "a low-source raise proves regulation from the lower tap limit"
t_expect_eq "$ACTIONS" ' profile:false source:0.90 source:1 profile:true' \
  "source direction is selected from the current field tap"

REFERENCE_PACKAGE="$PACKAGE"
mkdir -p "$TMP_DIR/reference/process" "$TMP_DIR/reference/protocols"
cp "$PACKAGE/protocols/points.json" "$TMP_DIR/reference/protocols/points.json"
jq '.configured_delay_s = 2 | .motion_s = 1' "$PACKAGE/process/reference-cases.json" \
  >"$TMP_DIR/reference/process/reference-cases.json"
PACKAGE="$TMP_DIR/reference"
reset_trace '[{"at":0,"tap":2}]'
CLOCK_STEP=1000
t_run phase
t_expect_eq "$T_STATUS" 1 "a changed timing reference still detects a stall"
t_expect_eq "$CLOCK" 23000 "the demand-cycle budget follows reference-cases.json"
PACKAGE="$REFERENCE_PACKAGE"

# Exercise the G8 call site: autonomous regulation precedes the fault sequence,
# and the existing local-protection and solver-progression checks still run.
FAULTS=0
eu_action_step() {
  [ "$SOURCE:$PROFILE" = 1:true ] || { fail "fault loop ran before driver restoration"; return 1; }
  if [ "$1:$2" = process:fault_s1 ]; then
    FAULTS=$((FAULTS + 1))
    TRIPPED=true
  elif [ "$1:$2" = breaker-1:reset ]; then TRIPPED=false; fi
}
eu_wait_for_field() {
  eu_get_field || return
  jq -e "$1" <<<"$API_BODY" >/dev/null
}
reset_trace
# shellcheck disable=SC2034 # consumed by G8
g7_loss_verified=1
t_run eu_gate_G8
t_expect_fails 0 "G8 integrates regulation before local faults without weakening existing checks"
t_expect_eq "$FAULTS" 2 "the original true/false/true fault loop still runs"
t_expect_out "solver solve_seq advances independently" "G8 retains independent solver progression"

FAULTS=0
reset_trace '[{"at":0,"tap":2}]'
t_run eu_gate_G8
t_expect_fails 1 "G8 fails the gate when the PLC never completes a step"
t_expect_eq "$FAULTS" 0 "the fault loop is not run after an unproved regulation phase"
expect_restored

reset_trace
CACHE_EDIT='.cache_epoch = "restarted"'
t_run eu_gate_G8
t_expect_fails 1 "G8 fails the gate if the operator cache changes during regulation"
t_expect_eq "$FAULTS" 0 "local faults cannot obscure an earlier isolation failure"
expect_restored
t_finish
