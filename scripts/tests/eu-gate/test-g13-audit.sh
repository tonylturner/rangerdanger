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

# Gate flow: each mutation waits for its own first_solve while the RTU is
# paused, the CC must lose the link, and recovery needs a newer connection_seq.
CALLS="$TMP_DIR/g13-calls.log"
: >"$CALLS"
REVISION=9
RTU_STATE=false
RESET_DONE=false
RESET_FAIL=false
FIELD_FAIL=false
FIELD_PREDICATE=""
eu_apply_named() {
  printf 'policy %s\n' "$1" >>"$CALLS"
}
eu_reset_all() {
  printf 'reset\n' >>"$CALLS"
  if [ "$RESET_FAIL" = true ]; then
    fail "reset failed (stub)"
    return 1
  fi
  RESET_DONE=true
}
docker() {
  printf 'docker %s\n' "$1" >>"$CALLS"
  case "$1" in
    pause) RTU_STATE=true ;;
    unpause) RTU_STATE=false ;;
    inspect) printf '%s\n' "$RTU_STATE" ;;
  esac
}
eu_wait_for_operator() {
  printf 'operator %s\n' "$1" | tr -s ' \n' ' ' >>"$CALLS"
  printf '\n' >>"$CALLS"
  WAIT_BODY=$(cat "$T_FIXTURES/operator-gi-untimed.json")
}
eu_wait_for_field() {
  printf 'field\n' >>"$CALLS"
  FIELD_PREDICATE="$1"
  WAIT_BODY=$(cat "$T_FIXTURES/field-baseline.json")
  if [ "$FIELD_FAIL" = true ] || ! printf '%s' "$WAIT_BODY" | jq -e "$1" >/dev/null; then
    fail "reset truth not settled (stub)"
    return 1
  fi
}
eu_action_step() {
  local node="$1" action="$2"
  if [ "$node:$action" = process:fault_s1 ] && [ "$RESET_DONE" != true ]; then
    fail "process fault action without reset"
    return 1
  fi
  case "$node:$action" in
    process:fault_s1|breaker:reset) REVISION=$((REVISION + 1)) ;;
  esac
  printf 'action %s %s revision %s\n' "$node" "$action" "$REVISION" >>"$CALLS"
  API_BODY="{\"revision\":$REVISION}"
}
eu_wait_for_state() {
  printf 'wait %s\n' "$4" >>"$CALLS"
}
eu_api_get() {
  printf 'audit\n' >>"$CALLS"
  # shellcheck disable=SC2034 # read by the sourced gate helpers
  HTTP_STATUS=200
  API_BODY="$AUDIT"
  # shellcheck disable=SC2034 # consumed by eu_check_g13_audit
  [ "$1" != "/api/field/audit?after=0" ] || API_BODY='{"generation":42,"entries":[],"latest_seq":20,"oldest_seq":1}'
}
t_run eu_gate_G13
t_expect_fails 0 "the G13 flow passes against an audit with both first_solve entries"
t_run grep -E '^(policy|reset|field|audit|action|wait|docker (pause|unpause)|operator .link.state != )' "$CALLS"
t_expect_eq "$T_OUT" "policy improved
reset
field
audit
docker pause
action process fault_s1 revision 10
wait G13 solver first_solve of device revision 10 while RTU paused
action process clear_fault revision 10
action breaker reset revision 11
wait G13 solver first_solve of device revision 11 while RTU paused
operator .link.state != \"started\" 
docker unpause
audit" "reset and solver agreement precede the cursor and pause; each mutation awaits its first_solve"
t_run grep -c 'connection_seq > 1 ' "$CALLS"
t_expect_eq "$T_OUT" 1 "recovery requires a connection_seq newer than the pre-outage link"
precondition_matches() {
  jq "$1" "$T_FIXTURES/field-baseline.json" | jq -e "$FIELD_PREDICATE"
}
for mutation in '.devices["breaker-1"].position = "open"' \
  '.devices["breaker-1"].protection_tripped = true' \
  '.process.devices["feeder-breaker"].solved_revision = -1'; do
  t_run precondition_matches "$mutation"
  t_expect_eq "$T_STATUS" 1 "G13 rejects an unsettled precondition: $mutation"
done

: >"$CALLS"
RESET_DONE=false
RESET_FAIL=true
t_run eu_gate_G13
t_expect_fails 1 "G13 aborts if its own reset fails"
t_expect_eq "$(cat "$CALLS")" "policy improved
reset" "a failed reset cannot reach the audit cursor, RTU pause or direct driver"

: >"$CALLS"
RESET_FAIL=false
FIELD_FAIL=true
t_run eu_gate_G13
t_expect_fails 1 "G13 aborts if reset truth and solver have not converged"
t_run grep -Ec '^(audit|docker|action)' "$CALLS"
t_expect_eq "$T_OUT" 0 "unsettled reset cannot reach the audit cursor, pause or trip"

t_finish
