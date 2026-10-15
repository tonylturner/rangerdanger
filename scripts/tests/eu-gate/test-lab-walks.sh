#!/usr/bin/env bash
# Student command route, fail-closed validators, independent baselines/cleanup.
# shellcheck disable=SC2329 # stubs invoked indirectly by sourced walk helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CALLS=""
RESPONSE='{"exit_code":0,"stdout":"ok","stderr":""}'
STATUS=200
eu_api_post() {
  CALLS="$CALLS POST:$1"
  POST_BODY="$2"
  HTTP_STATUS="$STATUS" API_BODY="$RESPONSE"
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
t_run eu_lab_vendor 10.40.40.22 1 1
t_expect_fails 0 "the denied retest expects mbpoll failure without retrying"

eu_wait_for_operator() { :; }
RESPONSE=$(jq -nc '{exit_code:0,stderr:"",stdout:({point:"rmu-1-command",operation:"open",result:"completed"}|tojson)}')
t_run eu_lab_cc rmu-1-command open
t_expect_fails 0 "CC command checks the returned protocol outcome"
t_expect_out "wget -qO-" "CC command uses its shipped BusyBox tool"
RESPONSE=$(jq -nc '{exit_code:0,stderr:"",stdout:({point:"rmu-1-command",operation:"open",result:"unknown"}|tojson)}')
t_run eu_lab_cc rmu-1-command open
t_expect_fails 1 "HTTP success cannot hide unknown command outcome"

SCENARIO_ID=eu-rmu-switching
eu_api_get() {
  CALLS="$CALLS GET:$1"
  HTTP_STATUS=200
  API_BODY=$(jq -nc '{steps:([range(0;10)|{id:("step-"+tostring),title:"test",action:{type:"command"}}]|tojson)}')
}
eu_execute_step() { CALLS="$CALLS EXEC:$1:$3"; }
CALLS=""
t_run eu_lab_step 3
t_expect_eq "$CALLS" " GET:/api/scenarios/eu-rmu-switching EXEC:step-3:eu-rmu-switching" "step resolves the current lab, not lab 1"
CALLS=""
t_run eu_lab_step 7 defer
t_expect_eq "$CALLS" " GET:/api/scenarios/eu-rmu-switching" "outage step action waits until restrictive policy is active"

eu_apply_named() { CALLS="$CALLS policy:$1"; }
eu_reset_all() { CALLS="$CALLS reset"; }
eu_validate_scenario() { CALLS="$CALLS validate:$SCENARIO_ID:$1"; }
walk_ok() { CALLS="$CALLS walk"; }
walk_bad() { CALLS="$CALLS walk-failed"; return 1; }
CALLS=""
t_run eu_lab_gate eu-rmu-switching walk_ok
t_expect_eq "$T_STATUS" 0 "complete walk succeeds"
t_expect_eq "$CALLS" " policy:weak reset validate:eu-rmu-switching:FAIL walk validate:eu-rmu-switching:PASS policy:weak reset" "every gate owns FAIL baseline, PASS validation and restoration"
CALLS=""
t_run eu_lab_gate eu-oltc-plc-historian walk_bad
t_expect_eq "$T_STATUS" 1 "incomplete walk fails"
t_expect_eq "$CALLS" " policy:weak reset validate:eu-oltc-plc-historian:FAIL walk-failed GET:/api/scenarios/eu-oltc-plc-historian/validate policy:weak reset" "failed walk keeps validation evidence and restores weak and devices, never claims PASS"
t_expect_eq "$SCENARIO_ID" eu-rmu-switching "scenario selection is local to the gate"

# Restore the real validator to test every-check PASS, not only the outcome.
# shellcheck source-path=SCRIPTDIR source=../../lib/eu-policy.sh
. "$T_REPO/scripts/lib/eu-policy.sh"
VALIDATION='{"outcome":"PASS","checks":[{"status":"pass"},{"status":"fail"}]}'
eu_api_get() { API_BODY="$VALIDATION"; }
t_run eu_validate_scenario PASS
t_expect_fails 1 "a PASS outcome with a failed check fails closed"
VALIDATION='{"outcome":"PASS","checks":[]}'
t_run eu_validate_scenario PASS
t_expect_fails 1 "empty checks cannot pass"
VALIDATION='{"outcome":"PASS","checks":[{"status":"pass"}]}'
t_run eu_validate_scenario PASS
t_expect_fails 0 "every check passing is accepted"

# Exercise complete lab-3/4 orchestration without range traffic or sleeps.
STEPS=""
eu_lab_step() { STEPS="$STEPS $1:${2:-execute}"; }
eu_lab_baseline_exports() { :; }
eu_lab_cc() { CALLS="$CALLS CC:$1:$2"; }
eu_lab_exec() { CALLS="$CALLS exec:$1"; }
eu_lab_vendor() { CALLS="$CALLS vendor:$1:$2:${3:-0}"; }
eu_lab_tap() { CALLS="$CALLS tap:$1"; }
eu_make_custom_policy() { CALLS="$CALLS derive-restriction"; }
eu_apply_custom() { CALLS="$CALLS restrict"; }
eu_apply_named() { CALLS="$CALLS policy:$1"; }
eu_api_get() { :; }
sleep() { :; }
CALLS=""
t_run eu_lab3_walk
t_expect_eq "$T_STATUS" 0 "stubbed lab 3 completes all operations"
t_expect_eq "$STEPS" " 0:execute 1:execute 2:execute 3:execute 4:execute 5:execute 6:execute 7:execute 8:execute 9:execute" "lab 3 visits all ten steps in authored order"
t_expect_out "PREDICT RMU1 open loses S2 only" "load-loss predictions precede switching"
t_expect_eq "$(printf '%s' "$CALLS" | grep -o 'vendor:10.40.40.22:1:[01]' | tr '\n' ' ')" \
  "vendor:10.40.40.22:1:0 vendor:10.40.40.22:1:1 " "lab 3 retests identical vendor payload with different expected transport result"
STEPS="" CALLS=""
t_run eu_lab4_walk
t_expect_eq "$T_STATUS" 0 "stubbed lab 4 completes all operations"
t_expect_eq "$STEPS" " 0:execute 1:execute 2:execute 3:execute 4:execute 5:execute 6:execute 7:defer 8:execute 9:execute" "lab 4 visits all ten steps and defers the outage cut"
case "$CALLS" in
  *derive-restriction*restrict*EXEC:source-pin-and-cc-outage*) t_ok "restrictive policy precedes the outage session cut" ;;
  *) t_not_ok "outage must apply restrictive policy before session cut: $CALLS" ;;
esac
t_expect_eq "$(printf '%s' "$CALLS" | grep -o 'vendor:10.40.40.23:1:[01]' | tr '\n' ' ')" \
  "vendor:10.40.40.23:1:0 vendor:10.40.40.23:1:1 " "lab 4 retests identical vendor payload"
eu_api_get() { API_BODY='{"file_ready":true}'; }
eu_api_post() { HTTP_STATUS=200; }
eu_firewall_config() { cat "$PACKAGE/firewall/improved.json"; }
curl() {
  while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then printf 'stub PCAP' >"$2"; return; fi
    shift
  done
  return 1
}
STEPS="" CALLS=""
t_run eu_lab2_walk
t_expect_eq "$T_STATUS" 0 "stubbed lab 2 completes positive/negative proof and rollback"
t_expect_eq "$STEPS" " 0:execute 1:execute 2:execute 3:execute 4:execute 5:execute 5:execute 6:execute 7:execute 8:execute 9:execute" "lab 2 visits all ten steps and reapplies improved after rollback"
eu_firewall_config() { printf '{"firewall":{"rules":[]}}'; }
STEPS=""
t_run eu_lab2_walk
t_expect_fails 1 "missing explicit logged HTTP denies stops at the curriculum hold point"
t_expect_eq "$STEPS" " 0:execute 1:execute 2:execute 3:execute 4:execute 5:execute" "a blocked lab never manufactures subsequent proof"
t_finish
