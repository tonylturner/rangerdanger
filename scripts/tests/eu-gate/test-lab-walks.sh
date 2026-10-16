#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called indirectly by sourced helpers
# Served-scenario walks, exact authored IDs, validators and package curriculum.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

t_run jq -e '
  .firewall.defaultAction == "DENY" and
  ([.firewall.rules[] | select(.id == "weak-foreign-field-modbus")] |
    length == 1 and all(.[];
      .sourceZones == ["wan","dmz"] and .destZones == ["lan2"] and
      .protocols == [{"name":"tcp","port":"502"}] and .action == "ALLOW" and
      (.description | startswith("WEAK teaching exposure:"))))
' "$PACKAGE/firewall/weak.json"
t_expect_eq "$T_STATUS" 0 "weak exposes only field TCP/502 to enterprise and vendor"
t_run jq -e '
  [.firewall.rules[] | select(.id == "deny-unauthorized-modbus")] |
  length == 1 and all(.[];
    (.sourceZones | index("wan")) != null and
    (.sourceZones | index("dmz")) != null and .destZones == ["lan2"] and
    .protocols == [{"name":"tcp","port":"502"}] and
    .action == "DENY" and .log == true)
' "$PACKAGE/firewall/improved.json"
t_expect_eq "$T_STATUS" 0 "improved retains the logged foreign field-Modbus deny"

CALLS=""
RESPONSE='{"exit_code":0,"stdout":"ok","stderr":""}'
STATUS=200
eu_api_post() {
  CALLS="$CALLS POST:$1"
  POST_BODY="$2"
  HTTP_STATUS="$STATUS"
  API_BODY="$RESPONSE"
}
t_run eu_lab_exec vendor-jump-1 'mbpoll -m tcp target'
t_expect_eq "$T_STATUS" 0 "successful student command executes"
t_expect_eq "$CALLS" " POST:/api/workshop/nodes/vendor-jump-1/exec" "Run uses backend node exec, not docker exec"
t_run jq -e '.command == "mbpoll -m tcp target" and .timeout_sec == 25' <<<"$POST_BODY"
t_expect_eq "$T_STATUS" 0 "command and bounded timeout survive JSON encoding"
for RESPONSE in '{"exit_code":127,"stdout":"","stderr":"missing tool"}' \
  '{"exit_code":1,"stdout":"","stderr":"timeout"}' '{"stdout":"ok","stderr":""}'; do
  t_run eu_lab_exec vendor-jump-1 'mbpoll target'
  t_expect_fails 1 "nonzero or missing exit status cannot pass"
done
RESPONSE='{"exit_code":1,"stdout":"","stderr":"Connection timed out"}'
t_run eu_lab_exec vendor-jump-1 'mbpoll target' 1
t_expect_eq "$T_STATUS" 0 "the denied retest expects mbpoll failure without retrying"

eu_wait_for_operator() { :; }
RESPONSE=$(jq -nc '{exit_code:0,stderr:"",stdout:({point:"rmu-1-command",operation:"open",result:"completed"}|tojson)}')
t_run eu_lab_cc rmu-1-command open
t_expect_fails 0 "CC command checks the returned protocol outcome"
t_expect_out "wget -qO-" "CC command uses its shipped BusyBox tool"
RESPONSE=$(jq -nc '{exit_code:0,stderr:"",stdout:({point:"rmu-1-command",operation:"open",result:"unknown"}|tojson)}')
t_run eu_lab_cc rmu-1-command open
t_expect_fails 1 "HTTP success cannot hide unknown command outcome"

# The served scenario has a non-fixed number of steps. The gate resolves the
# authored ID and then uses the current array index expected by the API.
CALLS=""
SCENARIO_STEPS='[{"id":"first","title":"First","action":{"type":"command"}},{"id":"middle","title":"Middle"},{"id":"target-action","title":"Target","action":{"type":"probe"}}]'
eu_api_get() {
  CALLS="$CALLS GET:$1"
  HTTP_STATUS=200
  API_BODY=$(jq -nc --argjson steps "$SCENARIO_STEPS" '{steps:($steps|tojson)}')
}
eu_execute_step() { CALLS="$CALLS EXEC:$1:$3"; }
t_run eu_lab_step served-workshop target-action
t_expect_eq "$T_STATUS" 0 "authored action step resolves and executes"
t_expect_eq "$CALLS" " GET:/api/scenarios/served-workshop EXEC:target-action:served-workshop" "step ID resolves independently of count and position"
t_expect_out "step index 2" "the resolved current API index is reported"
CALLS=""
t_run eu_lab_step served-workshop middle
t_expect_eq "$T_STATUS" 0 "manual steps are served without an executor action"
t_expect_eq "$CALLS" " GET:/api/scenarios/served-workshop" "manual step is not executed"
SCENARIO_STEPS='[{"id":"duplicate","action":{"type":"command"}},{"id":"duplicate","action":{"type":"command"}}]'
t_run eu_lab_step served-workshop duplicate
t_expect_eq "$T_STATUS" 1 "duplicate authored IDs fail closed"
t_expect_fails 1 "ambiguous steps are reported"

# Every validator attempt is preceded by a reset, and PASS has its own reset
# and policy state rather than inheriting the failed weak-policy pre-state.
CALLS=""
eu_reset_all() { CALLS="$CALLS reset"; }
eu_apply_named() { CALLS="$CALLS policy:$1"; }
eu_validate_scenario() { CALLS="$CALLS validate:$1:$2"; }
walk_ok() { CALLS="$CALLS walk:$1"; }
prepare_ok() { CALLS="$CALLS prepare:$1"; }
t_run eu_lab_gate eu-firewall-implementation walk_ok improved prepare_ok
t_expect_eq "$T_STATUS" 0 "complete validator walk succeeds"
t_expect_eq "$CALLS" \
  " reset policy:weak validate:FAIL:eu-firewall-implementation walk:eu-firewall-implementation reset policy:improved prepare:eu-firewall-implementation validate:PASS:eu-firewall-implementation policy:weak reset" \
  "FAIL and PASS checks each use the named slot after their own reset"
CALLS=""
walk_bad() {
  CALLS="$CALLS walk-failed:$1"
  return 1
}
eu_api_get() {
  CALLS="$CALLS GET:$1"
  HTTP_STATUS=200
  API_BODY='{"outcome":"FAIL"}'
}
t_run eu_lab_gate eu-vendor-rdp-compromise walk_bad improved prepare_ok
t_expect_eq "$T_STATUS" 1 "incomplete student walk fails"
t_expect_eq "$CALLS" \
  " reset policy:weak validate:FAIL:eu-vendor-rdp-compromise walk-failed:eu-vendor-rdp-compromise GET:/api/scenarios/eu-vendor-rdp-compromise/validate policy:weak reset" \
  "failed walk keeps evidence and restores weak policy and reset without claiming PASS"

# The validation-evidence scenario starts hardened; its student check/report
# walk must not run under the weak FAIL pre-state.
CALLS=""
eu_apply_named() { CALLS="$CALLS policy:$1"; }
eu_lab_step() { CALLS="$CALLS step:$2"; }
eu_api_post() {
  CALLS="$CALLS POST:$1"
  HTTP_STATUS=200
  API_BODY='{"summary":{"result":"PASS","skipped":0}}'
}
t_run eu_lab_validation_walk eu-validation-evidence
t_expect_eq "$T_STATUS" 0 "validation evidence walk establishes its hardened pre-state"
t_expect_eq "$CALLS" \
  " policy:improved step:confirm-hardened-config-is-active step:generate-the-validation-report POST:/api/firewall/validation-report step:reflection-what-did-your-plan-close" \
  "hardened policy is active before the served checks and report"

# Package curriculum binding supplies the action catalog; scenario prompts
# remain authored decisions, rather than duplicating catalog definitions.
REQ_DESCRIPTION=$(printf '%s\n' \
  ':::decision id=current-policy-allowable options=MOSTLY_ALLOW,MOSTLY_DENY' \
  ':::decision id=hardened-changes-rough-count options=FEW_CHANGES,ABOUT_HALF' \
  ':::decision id=enterprise-to-field options=BLOCK,BLOCK and LOG correct=BLOCK' \
  ':::decision id=enterprise-to-station options=BLOCK,BLOCK and LOG correct=BLOCK' \
  ':::decision id=vendor-to-station options=BLOCK,BLOCK and LOG correct=BLOCK' \
  ':::decision id=non-rtu-to-field options=BLOCK,BLOCK and LOG correct=BLOCK' \
  ':::decision id=unauth-control options=BLOCK,BLOCK and LOG correct=BLOCK' \
  ':::decision id=ot-eng-availability options=AVAILABLE,STRETCHED correct=AVAILABLE' \
  ':::decision id=fw-admin-skills options=IN_HOUSE,GAP correct=IN_HOUSE' \
  ':::decision id=vendor-change-window options=SECURED,NEGOTIATING correct=SECURED' \
  ':::decision id=risk-acceptance-authority options=CLEAR,UNCLEAR correct=CLEAR')
REQ_STEPS=$(jq -nc --arg description "$REQ_DESCRIPTION" '[{description:$description}]')
PLAN_STEPS='[{"description":"Plan","action":{"type":"decision"}}]'
eu_api_get() {
  HTTP_STATUS=200
  case "$1" in
    /api/packages)
      API_BODY='[{"id":"eu-iec104-substation","active":true,"curriculum":{"budgetHours":500,"teams":[{},{},{},{}],"catalog":[{}],"roles":{"requirements":"eu-segmentation-requirements","planning":"eu-remediation-planning"}}}]'
      ;;
    /api/scenarios/eu-segmentation-requirements)
      API_BODY=$(jq -nc --argjson steps "$REQ_STEPS" '{steps:($steps|tojson)}')
      ;;
    /api/scenarios/eu-remediation-planning)
      API_BODY=$(jq -nc --argjson steps "$PLAN_STEPS" '{steps:($steps|tojson)}')
      ;;
    *) return 1 ;;
  esac
}
t_run eu_curriculum_slot requirements eu-segmentation-requirements \
  '["enterprise-to-field","enterprise-to-station","vendor-to-station","non-rtu-to-field","unauth-control","ot-eng-availability","fw-admin-skills","vendor-change-window","risk-acceptance-authority","current-policy-allowable","hardened-changes-rough-count"]'
t_expect_eq "$T_STATUS" 0 "requirements gate checks every served decision and package catalog binding"
t_run eu_lab_validation_slot
t_expect_eq "$T_STATUS" 0 "planning gate checks its single served decision action and catalog binding"

# A vendor-path FC6 audit check proves the physical peer, not the user who
# opened the browser terminal. Keep the pass message explicit about both.
eu_management() {
  HTTP_STATUS=200
  if [ "$3" = "/api/audit?after=0" ]; then
    API_BODY='{"latest_seq":7,"entries":[]}'
  else
    API_BODY='{"latest_seq":8,"entries":[{"seq":8,"operation":"open","result":"accepted","after":{"position":"open"},"context":{"protocol":"modbus","peer":"10.20.20.10:41322"}}]}'
  fi
}
eu_lab_exec() { API_BODY='{"stdout":"1 register written"}'; }
t_run eu_lab_vendor 10.40.40.21 1
t_expect_eq "$T_STATUS" 0 "vendor FC6 requires a fresh accepted device audit"
t_expect_out "vendor FC6 RMU1 OPEN is attributed to vendor-jump 10.20.20.10" \
  "success text names the RMU operation and actual vendor-jump peer"

# The validator API has no implicit current scenario.
# shellcheck source-path=SCRIPTDIR source=../../lib/eu-policy.sh
. "$T_REPO/scripts/lib/eu-policy.sh"
VALIDATION='{"outcome":"PASS","checks":[{"status":"pass"},{"status":"fail"}]}'
eu_api_get() {
  CALLS="$CALLS GET:$1"
  API_BODY="$VALIDATION"
  printf '%s\n' "$1"
}
t_run eu_validate_scenario PASS eu-validation-evidence
t_expect_fails 1 "a PASS outcome with a failed check fails closed"
VALIDATION='{"outcome":"PASS","checks":[]}'
t_run eu_validate_scenario PASS eu-validation-evidence
t_expect_fails 1 "empty checks cannot pass"
VALIDATION='{"outcome":"PASS","checks":[{"status":"pass"}]}'
t_run eu_validate_scenario PASS eu-validation-evidence
t_expect_fails 0 "every check passing is accepted for the named scenario"
t_expect_out "/api/scenarios/eu-validation-evidence/validate" \
  "validator GET targets the explicitly named served scenario"
t_run eu_validate_scenario PASS
t_expect_fails 1 "validator rejects an omitted scenario"

t_finish
