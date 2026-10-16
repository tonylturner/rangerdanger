#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called indirectly by sourced helpers
# Student actions resolve authored step IDs and execute once, without
# calling device mutation endpoints or accepting manual/failed results.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

POSTS=0
GETS=0
POST_PATH=""
RESPONSE='{"success":true,"action_type":"command","results":[{"success":true}]}'
RESPONSE_STATUS=200
SCENARIO='{"steps":"[{\"id\":\"observe\"},{\"id\":\"authored-action\",\"action\":{\"type\":\"command\"}}]"}'
eu_api_get() {
  GETS=$((GETS + 1))
  HTTP_STATUS=200
  case "$1" in
    /api/scenarios/served-workshop) API_BODY="$SCENARIO" ;;
    /api/field/state) API_BODY=$(cat "$T_FIXTURES/field-baseline.json") ;;
    *) return 1 ;;
  esac
}
eu_api_post() {
  POSTS=$((POSTS + 1))
  POST_PATH="$1"
  HTTP_STATUS="$RESPONSE_STATUS"
  API_BODY="$RESPONSE"
}

t_run eu_execute_step authored-action "student action" served-workshop
t_expect_fails 0 "a successful authored action passes"
t_expect_eq "$POSTS" 1 "the action executes exactly once"
t_expect_eq "$POST_PATH" "/api/scenarios/served-workshop/steps/1/execute" "the index is resolved from the served curriculum"

t_run eu_device_step authored-action "student action readback" served-workshop
t_expect_fails 0 "device actions retain the physical readback shape check"
t_expect_out "device /api/state shape" "the gate asserts physical state, not just the execution acknowledgement"

for RESPONSE in \
  '{"success":false,"action_type":"command","results":[{"success":false}]}' \
  '{"success":true,"action_type":"manual","results":[{"success":true}]}' \
  '{"success":true,"action_type":"command","results":[]}' \
  '{"success":true,"action_type":"command","results":[{"success":false}]}'; do
  before="$POSTS"
  t_run eu_execute_step authored-action "failed action" served-workshop
  t_expect_fails 1 "failed, manual or empty execution results fail closed"
  t_expect_eq "$POSTS" "$((before + 1))" "failed executions are not retried"
done

before="$POSTS"
t_run eu_execute_step missing "unknown step" served-workshop
t_expect_fails 1 "a missing authored action fails"
t_expect_eq "$POSTS" "$before" "a missing step does not execute"
t_run eu_execute_step observe "manual step" served-workshop
t_expect_fails 1 "a manual step cannot substitute for a device action"
t_expect_eq "$POSTS" "$before" "a manual step does not execute"

SCENARIO='{"steps":"[{\"id\":\"authored-action\",\"action\":{}},{\"id\":\"authored-action\",\"action\":{}}]"}'
t_run eu_execute_step authored-action "ambiguous step" served-workshop
t_expect_fails 1 "ambiguous authored IDs fail before execution"
t_expect_eq "$POSTS" "$before" "an ambiguous step does not execute"

# Calls without a scenario cannot silently fall back to retired curriculum.
before="$GETS"
t_run eu_execute_step authored-action "missing scenario"
t_expect_fails 1 "an omitted scenario is rejected"
t_expect_eq "$GETS" "$before" "no hidden scenario default is queried"

# Direct physical gates preserve breaker-state readback without a scenario step.
DIRECT_ACTION=""
eu_action() { DIRECT_ACTION="$1:$2:${3:-}"; }
before="$POSTS"
t_run eu_action_step process fault_s1 "direct fault readback"
t_expect_fails 0 "direct action keeps the physical readback assertion"
t_expect_eq "$DIRECT_ACTION" "process:fault_s1:" "the range action receives the physical operation"
t_expect_eq "$POSTS" "$before" "direct action does not execute a curriculum step"
t_expect_out "device /api/state shape" "direct path validates the breaker state response"

# Increment-4 management requests carry the frozen vocabulary and numeric value.
unset -f eu_action
# shellcheck source-path=SCRIPTDIR source=../../lib/eu-range.sh
. "$T_REPO/scripts/lib/eu-range.sh"
eu_management() {
  # shellcheck disable=SC2034 # consumed by the sourced action helper
  HTTP_STATUS=200
  API_BODY='{"schema":2}'
  POST_PATH="$1 $2 $3"
  ACTION_BODY="$4"
}
t_run eu_action process set_load 3.0
t_expect_fails 0 "a process action accepts its explicit management acknowledgement"
t_expect_eq "$POST_PATH" "process POST /api/process/actions" "process actions use their single cutover endpoint"
t_run jq -e '.action == "set_load" and .value == 3' <<<"$ACTION_BODY"
t_expect_eq "$T_STATUS" 0 "set_load value is a JSON number"
t_run eu_action rmu-1 set_local
t_expect_eq "$POST_PATH" "rmu-1 POST /api/sim/actions" "device actions use sim actions"
t_run jq -e '.action == "set_local" and (has("value") | not)' <<<"$ACTION_BODY"
t_expect_eq "$T_STATUS" 0 "device actions never send a value"

t_finish
