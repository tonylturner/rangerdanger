#!/usr/bin/env bash
# shellcheck disable=SC2329 # stubs called by sourced helpers
# Generic helpers must not confuse RESET/refusal with DENY drop evidence.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GI_ERROR='connect failed: dial tcp 10.30.30.20:2404: i/o timeout'
EVENT_ACTION=DENY
MODE=gi
sleep() { :; }
docker() {
  if [ "$MODE" = gi ]; then
    printf '%s\n' "$GI_ERROR" >&2
    return 2
  fi
  jq -nc --arg action "$EVENT_ACTION" '[{
    kind:"firewall.rule.hit", timestamp:"2026-11-11T12:00:00Z",
    srcIp:"10.10.10.50", dstIp:"10.30.30.20", dstPort:2404,
    attributes:{action:$action,ruleId:"manual-reset"}
  }]'
  printf '200'
}
t_run eu_run_kali_gi RESET-helper 2
t_expect_eq "$T_STATUS" 0 "explicit connect timeout satisfies the DENY helper"
for GI_ERROR in \
  'connect failed: dial tcp 10.30.30.20:2404: connection refused' \
  'connect failed: no route to host' \
  'connect failed' \
  'connect failed: dial tcp 10.30.30.20:2404: i/o timeout; connection refused'; do
  t_run eu_run_kali_gi RESET-helper 2
  t_expect_eq "$T_STATUS" 1 "RESET/refusal or generic failure cannot prove drop: $GI_ERROR"
done

MODE=events
# shellcheck disable=SC2034 # credentials consumed by eu_fresh_deny_event
FW_TOKEN=offline-token
t_run eu_fresh_deny_event 10.10.10.50 0 1
t_expect_eq "$T_STATUS" 0 "fresh DENY event satisfies the event contract"
EVENT_ACTION=RESET
t_run eu_fresh_deny_event 10.10.10.50 0 1
t_expect_eq "$T_STATUS" 1 "fresh RESET event with ruleId does not replace DENY evidence"
t_expect_out "no fresh firewall.rule.hit DENY" "event helper keeps requiring DENY"
t_finish
