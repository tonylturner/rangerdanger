#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called indirectly by sourced gates
# G13: the audit generation is compared as a number, and an empty or
# first_solve-less selection fails instead of passing vacuously.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

AUDIT=$(cat "$T_FIXTURES/audit-g13.json")
edit() {
  printf '%s' "$AUDIT" | jq -c "$1"
}
check() {
  t_run eu_check_g13_audit "$1" 42 9 20 "[10,11]"
}

# shellcheck disable=SC2016
t_run jq -n --argjson audit "$AUDIT" --arg generation 42 '$audit.generation == $generation'
t_expect_eq "$T_OUT" "false" "the run-1 string comparison of a numeric generation is false"

check "$AUDIT"
t_expect_fails 0 "numeric generation, cursor-bounded entries and a first_solve per revision pass"
t_expect_out "PASS G13 solver first_solve audit covers device revision 11" "each revision's first_solve is reported"

check "$(edit '.generation = 43')"
t_expect_fails 1 "a different numeric generation fails"
check "$(edit '.entries = []')"
t_expect_fails 1 "an empty audit selection fails instead of passing vacuously"
t_expect_eq "$T_STATUS" 1 "an empty audit selection stops the audit checks"
check "$(edit '.entries |= map(select(.operation != "first_solve"))')"
t_expect_fails 2 "a selection without first_solve entries fails for both revisions"
check "$(edit '.entries |= map(select(.operation != "first_solve" or .detail.revision != 11))')"
t_expect_fails 1 "a missing first_solve for one revision fails"
t_expect_out "lacks first_solve audit for device revision 11" "the unsolved revision is named"
check "$(edit '.entries[0].seq = 20')"
t_expect_fails 1 "an entry at or before the cursor fails"
check "$(edit '.oldest_seq = 23')"
t_expect_fails 1 "entries lost to the ring after the cursor fail"
check "$(edit '.entries += [{"seq":26,"operation":"first_solve","before":{"revision":9},"after":{"revision":9},"detail":{"revision":9}}]')"
t_expect_fails 1 "a first_solve of the baseline revision after the cursor fails"
check "$(edit '.entries |= map(select(.operation != "reset" or .after.revision != 11))')"
t_expect_fails 1 "a mutation missing from the audit fails"
t_run eu_check_g13_audit "$AUDIT" 42 9 20 "[10]"
t_expect_fails 1 "an audited truth change the gate did not make fails"

# Gate flow uses distinct per-device generations, revisions and cursors. Every
# wait evaluates the real predicate against offline field/operator/audit data.
CALLS="$TMP_DIR/g13-calls.log"
FIELD_BASE=$(cat "$T_FIXTURES/field-g13.json")
OPERATOR_BASE=$(cat "$T_FIXTURES/operator-gi-untimed.json")
init_flow() {
  : >"$CALLS"
  FIELD="$FIELD_BASE"
  OPERATOR="$OPERATOR_BASE"
  RECOVERY_OPERATOR="$OPERATOR_BASE"
  AUDITS=$(printf '%s' "$FIELD" | jq -c '.devices | to_entries | map({key:.key,
    value:{generation:.value.generation, latest_seq:(.value.revision * 10), oldest_seq:1,
      entries:[{seq:(.value.revision * 10),operation:"first_solve",detail:{revision:.value.revision}}]}}) | from_entries')
  RTU_STATE=false
  WAS_PAUSED=false
  RESET_FAIL=false
  RESET_DONE=false
  BAD_DEVICE=""
  BAD_AUDIT="."
  NOOP_ACTION=""
  BASELINE_PREDICATE=""
  EXTRA_TIMING=""
  EXTRA_ACTOR=plc
}
eu_apply_named() { printf 'policy %s\n' "$1" >>"$CALLS"; }
eu_reset_all() {
  printf 'reset\n' >>"$CALLS"
  if [ "$RESET_FAIL" = true ]; then fail "reset failed (stub)"; return 1; fi
  RESET_DONE=true
}
docker() {
  printf 'docker %s\n' "$1" >>"$CALLS"
  case "$1" in
    pause) RTU_STATE=true; WAS_PAUSED=true ;;
    unpause) RTU_STATE=false ;;
    inspect) printf '%s\n' "$RTU_STATE" ;;
    *) fail "unexpected docker call"; return 1 ;;
  esac
}
eu_wait_for_operator() {
  printf 'operator %s\n' "$1" | tr -s ' \n' ' ' >>"$CALLS"
  printf '\n' >>"$CALLS"
  WAIT_BODY="$OPERATOR"
  if [ "$RTU_STATE" = true ]; then
    if [ "$EXTRA_TIMING" = tail ]; then autonomous_tap; fi
    LOSS_BUDGET="$2"
    WAIT_BODY=$(printf '%s' "$WAIT_BODY" | jq -c '.link.state = "disconnected"')
  elif [ "$WAS_PAUSED" = true ]; then
    if [ "$EXTRA_TIMING" = recovery ]; then autonomous_tap; fi
    WAIT_BODY=$(printf '%s' "$RECOVERY_OPERATOR" | jq -c '.link.connection_seq += 1')
  fi
  printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $1" >/dev/null || { fail "operator predicate rejected fixture"; return 1; }
}
eu_wait_for_field() {
  printf 'field\n' >>"$CALLS"
  if [ -z "$BASELINE_PREDICATE" ]; then BASELINE_PREDICATE="$1"; fi
  WAIT_BODY="$FIELD"
  printf '%s' "$WAIT_BODY" | jq -e "$1" >/dev/null || { fail "unsettled field (stub)"; return 1; }
}
mutate_device() {
  local node="$1" op="$2" edit="${3:-.}" before after actor
  before=$(printf '%s' "$FIELD" | jq -c --arg node "$node" '.devices[$node]')
  FIELD=$(printf '%s' "$FIELD" | jq -c --arg node "$node" "
    .devices[\$node] |= ($edit | .revision += 1) | .devices[\$node] as \$device
    | .process.devices[\$device.device_id].solved_revision = \$device.revision")
  after=$(printf '%s' "$FIELD" | jq -c --arg node "$node" '.devices[$node]')
  actor='{"protocol":"lab","peer":"10.99.99.2:1234"}'
  case "$op" in
    plc_arm|plc_request) actor='{"protocol":"modbus","peer":"10.30.30.30:1234"}' ;;
    motion_complete) actor='{"protocol":"local","peer":"actuator"}' ;;
  esac
  if [ "$EXTRA_ACTOR" = unknown ] && [ "$op" = plc_request ]; then actor='{}'; fi
  AUDITS=$(printf '%s' "$AUDITS" | jq -c --arg node "$node" --arg op "$op" \
    --argjson before "$before" --argjson after "$after" --argjson actor "$actor" '
    .[$node] |= (.latest_seq as $seq | .latest_seq += 2 | .entries += [
      {seq:($seq+1),operation:$op,result:"accepted",before:$before,after:$after,context:$actor},
      {seq:($seq+2),operation:"first_solve",detail:{revision:$after.revision}}])')
}
autonomous_tap() {
  mutate_device oltc-1 plc_request '.moving = true | .target_tap = (.tap + 1)'
  mutate_device oltc-1 motion_complete '.moving = false | .tap = .target_tap'
  EXTRA_TIMING=""
}
eu_action() {
  local node="$1" action="$2"
  if [ "$RESET_DONE" != true ] || [ "$RTU_STATE" != true ]; then fail "mutation outside owned outage"; return 1; fi
  printf 'action %s %s\n' "$node" "$action" >>"$CALLS"
  if [ "$node:$action" = "$NOOP_ACTION" ]; then return 0; fi
  case "$node:$action" in
    process:fault_s1)
      FIELD=$(printf '%s' "$FIELD" | jq -c '.devices["breaker-1"].protection_tripped = true')
      mutate_device breaker-1 local_protection_trip
      mutate_device rmu-1 fault_passage ;;
    process:clear_fault) : ;;
    breaker-1:reset)
      FIELD=$(printf '%s' "$FIELD" | jq -c '.devices["breaker-1"].protection_tripped = false')
      mutate_device "$node" "$action" ;;
    oltc-1:block) mutate_device "$node" "$action" '.blocked = true' ;;
    oltc-1:unblock) mutate_device "$node" "$action" '.blocked = false' ;;
    *) mutate_device "$node" "$action" ;;
  esac
  API_BODY=$(printf '%s' "$FIELD" | jq -c --arg node "$node" '.devices[$node]')
  if [ "$node:$action" = oltc-1:unblock ] && [ "$EXTRA_TIMING" = step ]; then
    autonomous_tap
  elif [ "$node:$action" = oltc-1:unblock ] && [ "$EXTRA_TIMING" = arm_step ]; then
    mutate_device oltc-1 plc_arm '.armed = true'
    EXTRA_TIMING=""
  fi
}
eu_action_step() { eu_action "$1" "$2"; }
eu_api_get() {
  local query="${1#*device=}" node after
  node="${query%%&*}"
  after="${1##*after=}"
  printf 'audit %s %s\n' "$node" "$after" >>"$CALLS"
  if [ "$node:$after:$EXTRA_TIMING" = oltc-1:0:cursor ]; then
    mutate_device oltc-1 plc_arm '.armed = true'
    EXTRA_TIMING=""
  fi
  API_BODY=$(printf '%s' "$AUDITS" | jq -c --arg node "$node" --argjson after "$after" \
    '.[$node] | .entries |= map(select(.seq > $after))')
  if [ "$node" = "$BAD_DEVICE" ] && [ "$after" != 0 ]; then API_BODY=$(printf '%s' "$API_BODY" | jq -c "$BAD_AUDIT"); fi
}
eu_wait_for_state() {
  printf 'solve %s paused=%s\n' "$1" "$RTU_STATE" >>"$CALLS"
  eu_api_get "$1"
  printf '%s' "$API_BODY" | jq -e "$EU_JQ_DEFS $2" >/dev/null || { fail "missing first_solve (stub)"; return 1; }
}
init_flow
t_run eu_gate_G13
t_expect_fails 0 "G13 covers every device with independent audit evidence"
t_expect_eq "$LOSS_BUDGET" "$(jq -r '.timers | .t3_s + .t1_s + 10' "$PACKAGE/protocols/points.json")" "G13 loss budget derives from t3+t1 plus ten seconds"
t_expect_eq "$(grep -c 'connection_seq > 1 ' "$CALLS")" 1 "recovery requires a newer connection"
for node in breaker-1 f2-breaker rmu-1 rmu-2 oltc-1; do
  t_expect_out "G13 $node device audit records exactly the" "$node final audit is checked"
  cursor=$(printf '%s' "$FIELD_BASE" | jq -r --arg node "$node" '.devices[$node].revision * 10')
  t_expect_eq "$(grep -c "^audit $node 0$" "$CALLS")" 1 "$node owns its initial cursor"
  t_expect_eq "$(grep -c "^solve /api/field/audit?device=$node&after=$cursor paused=true$" "$CALLS")" \
    "$(printf '%s' "$AUDITS" | jq -r --arg node "$node" '[.[$node].entries[] | select(.operation == "first_solve")] | length - 1')" \
    "$node waits for every changed revision while RTU paused"
done

t_expect_eq "$(grep -E '^(policy|reset|field|audit .* 0$|docker pause)' "$CALLS" | head -9)" \
  "policy improved
reset
field
audit breaker-1 0
audit f2-breaker 0
audit rmu-1 0
audit rmu-2 0
audit oltc-1 0
docker pause" "all baselines and cursors precede the RTU pause"
t_expect_eq "$(grep -E '^(action|solve)' "$CALLS")" \
  "action process fault_s1
solve /api/field/audit?device=breaker-1&after=90 paused=true
solve /api/field/audit?device=rmu-1&after=290 paused=true
action process clear_fault
action breaker-1 reset
solve /api/field/audit?device=breaker-1&after=90 paused=true
action rmu-1 reset
solve /api/field/audit?device=rmu-1&after=290 paused=true
action rmu-2 reset
solve /api/field/audit?device=rmu-2&after=390 paused=true
action f2-breaker reset
solve /api/field/audit?device=f2-breaker&after=190 paused=true
action oltc-1 block
solve /api/field/audit?device=oltc-1&after=490 paused=true
action oltc-1 unblock
solve /api/field/audit?device=oltc-1&after=490 paused=true" \
  "every revision's first_solve precedes the next action that could replace it"
# Polls are observations, not a journal. The device audit accounts for every
# intervening revision, including a tail after the final poll and recovery.
for timing in step tail recovery; do
  init_flow
  EXTRA_TIMING="$timing"
  t_run eu_gate_G13
  t_expect_fails 0 "PLC/actuator-attributed extra changes during $timing pass"
  t_expect_out 'attributed truth revisions [50,51,52,53]' "all four revisions are recorded during $timing"
done
OLTC_AUDIT=$(printf '%s' "$AUDITS" | jq -c '.["oltc-1"] | .entries |= map(select(.seq > 490))')
check_oltc() {
  t_run eu_check_g13_audit "$1" 442 49 490 '[50,51]' oltc-1 \
    '[{"operation":"block","revision":50},{"operation":"unblock","revision":51}]'
}
for mutation in \
  '(.entries[] | select(.operation == "plc_request") | .context.peer) = "10.30.30.31:1234"' \
  '(.entries[] | select(.operation == "plc_request") | .context.peer) = "10.30.30.30:1234-extra"' \
  '(.entries[] | select(.operation == "plc_request") | .context.protocol) = "lab"' \
  '(.entries[] | select(.operation == "motion_complete") | .context.peer) = "unknown"' \
  '(.entries[] | select(.operation == "motion_complete") | .context.protocol) = "modbus"' \
  '(.entries[] | select(.operation == "block") | .context.protocol) = "modbus"' \
  '(.entries[] | select(.operation == "unblock") | .operation) = "reset"' \
  '(.entries[] | select(.operation == "plc_request") | .result) = "ignored"' \
  '.entries |= map(select(.operation != "plc_request"))' \
  '(.entries[] | select(.operation == "first_solve" and .detail.revision == 51))
    |= (.operation = "measurements_valid" | .before = {revision:51} | .after = {revision:51} | del(.detail))'; do
  check_oltc "$(printf '%s' "$OLTC_AUDIT" | jq -c "$mutation")"
  t_expect_fails 1 "OLTC rejects wrong actors/actions, unaudited changes or missing observed solves: $mutation"
done
# Even an apparently complete ring cannot hide an unrecorded revision.
check_oltc "$(printf '%s' "$OLTC_AUDIT" | jq -c '
  .entries |= map(select(.operation != "plc_request"))
  | .entries |= to_entries | .entries |= map(.value + {seq:(491 + .key)})
  | .latest_seq = .entries[-1].seq')"
t_expect_fails 1 "a revision gap fails even with contiguous audit sequence numbers"

init_flow
FIELD=$(printf '%s' "$FIELD" | jq -c '.devices["oltc-1"].armed = false')
EXTRA_TIMING=arm_step
t_run eu_gate_G13
t_expect_fails 0 "unblock then immediate PLC arming within one step passes"
t_expect_out 'attributed truth revisions [50,51,52]' "both within-step revisions are recorded, not just the final poll"
init_flow
EXTRA_TIMING=tail EXTRA_ACTOR=unknown
t_run eu_gate_G13
t_expect_fails 1 "an unattributed extra truth change fails"
t_expect_out "unexplained truth change" "unexplained actor is diagnosed"

# A zero write between the baseline field read and the audit read stays in-window.
init_flow
FIELD=$(printf '%s' "$FIELD" | jq -c '.devices["oltc-1"].armed = false')
EXTRA_TIMING=cursor
t_run eu_gate_G13
t_expect_fails 0 "arming between baseline and cursor is retained and PLC-attributed"
t_expect_out 'attributed truth revisions [50,51,52]' "arming and both lab actions are accounted for"

precondition_matches() { jq "$1" "$T_FIXTURES/field-g13.json" | jq -e "$BASELINE_PREDICATE"; }
for mutation in '.devices["breaker-1"].position = "open"' \
  '.devices["breaker-1"].protection_tripped = true' \
  '.process.devices["feeder-breaker"].solved_revision = -1' \
  'del(.devices["rmu-1"])' '.devices["oltc-1"].measurements.valid = false' \
  '.process.devices["rmu-2"].delivered = false'; do
  t_run precondition_matches "$mutation"
  t_expect_eq "$T_STATUS" 1 "G13 rejects an unsettled precondition: $mutation"
done

# Failures on any peer must be visible, not hidden by a healthy CB1 audit.
for action in process:fault_s1 breaker-1:reset rmu-1:reset rmu-2:reset \
  f2-breaker:reset oltc-1:block oltc-1:unblock; do
  init_flow
  NOOP_ACTION="$action"
  t_run eu_gate_G13
  t_expect_eq "$((T_FAILS > 0))" 1 "an acknowledged no-op cannot replace the required transition: $action"
done
for node in breaker-1 f2-breaker rmu-1 rmu-2 oltc-1; do
  for mutation in '.generation += 1' '.oldest_seq = 9999' \
    '.entries |= map(select(.operation != "first_solve"))' \
    '.entries |= map(select(.operation == "first_solve"))'; do
    init_flow
    BAD_DEVICE="$node" BAD_AUDIT="$mutation"
    t_run eu_gate_G13
    t_expect_eq "$((T_FAILS > 0))" 1 "G13 rejects $node audit damage: $mutation"
  done
done

# Recovery fixtures preserve the total count when substituting a point. This
# catches count-only inventories as well as missing and bad RMU/OLTC telemetry.
for node in rmu-1 rmu-2 oltc-1; do
  for mutation in 'del(.points[] | select(.device == $node))' \
    '(.points[] | select(.device == $node) | .quality.iv) = true' \
    '(.points[] | select(.device == $node) | .id) = "substituted"'; do
    init_flow
    RECOVERY_OPERATOR=$(printf '%s' "$OPERATOR_BASE" | jq -c --arg node "$node" "$mutation")
    t_run eu_gate_G13
    t_expect_eq "$T_FAILS" 1 "G13 recovery rejects $node telemetry: $mutation"
  done
done
for mutation in '.link.connection_seq = 0' '.link.gi.state = "running"'; do
  init_flow
  RECOVERY_OPERATOR=$(printf '%s' "$OPERATOR_BASE" | jq -c "$mutation")
  t_run eu_gate_G13
  t_expect_fails 1 "G13 recovery rejects an old connection or incomplete GI: $mutation"
done
init_flow
RESET_FAIL=true
t_run eu_gate_G13
t_expect_fails 1 "G13 aborts if its own reset fails"
t_expect_eq "$(cat "$CALLS")" "policy improved
reset" "a failed reset cannot reach a cursor or pause"
init_flow
FIELD=$(printf '%s' "$FIELD_BASE" | jq -c '.process.devices["oltc-1"].solved_revision = -1')
t_run eu_gate_G13
t_expect_fails 1 "G13 aborts if reset truth and solver have not converged"
t_expect_eq "$(grep -Ec '^(audit|docker|action)' "$CALLS")" 0 "unsettled truth cannot reach a cursor, pause or fault"
t_finish
