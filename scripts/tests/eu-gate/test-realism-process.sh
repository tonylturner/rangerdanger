#!/usr/bin/env bash
# Exercise reference-driven G16 budgets and restoration without a live range.
# shellcheck disable=SC2329 # stubs called by sourced gates
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CALLS="$TMP_DIR/process-calls"
: >"$CALLS"
SIM_TAP=2
RESET_FAIL=false
eu_reset_all() {
  printf 'reset\n' >>"$CALLS"
  [ "$RESET_FAIL" = false ] || return 1
  # Auto may already have moved while reset waited for GI.
  SIM_TAP=2
}
eu_management() {
  # shellcheck disable=SC2034 # consumed by gate
  HTTP_STATUS=200
  API_BODY='{"generation":42,"latest_seq":10}'
  printf 'management %s %s %s %s\n' "$1" "$2" "$3" "${4:-}" >>"$CALLS"
}
eu_get_field() {
  # shellcheck disable=SC2034 # read by sourced eu_reference_tap
  API_BODY="{\"devices\":{\"oltc-1\":{\"tap\":$SIM_TAP}}}"
}
eu_wait_for_field() { printf 'field %s\n' "$1" >>"$CALLS"; }
eu_action() { printf 'action %s %s %s\n' "$1" "$2" "${3:-}" >>"$CALLS"; }
eu_command() {
  printf 'command %s %s %s %s\n' "$1" "$2" "$3" "${4:-}" >>"$CALLS"
  if [ "$1:$2" = oltc-1-mode:auto ] && [ "$3" = completed ]; then
    SIM_TAP="${tap:-2}"
  elif [ "$1" = oltc-1-step ]; then
    if [ "${4:-}" = limit ]; then
      if [ "$2" = lower ]; then [ "$SIM_TAP" = -8 ]; else [ "$SIM_TAP" = 8 ]; fi
      return
    fi
    [ "$3" = completed ] || return 0
    if [ "$2" = lower ]; then SIM_TAP=$((SIM_TAP - 1)); else SIM_TAP=$((SIM_TAP + 1)); fi
  fi
}
eu_fresh_points() {
  local observation
  printf 'fresh %s %s\n' "$3" "$4" >>"$CALLS"
  # shellcheck disable=SC2154 # key is the dynamically scoped G16 case selector
  observation=$(jq -cn --argjson tap "$SIM_TAP" --argjson kv \
    "$(jq -r --arg key "$key" '.[$key][-1].bus20_kv' "$PACKAGE/process/reference-cases.json")" \
    '{points:[{id:"oltc-1-tap",value:{value:$tap,transient:false},quality:{iv:false,nt:false,sb:false,bl:false,ov:false},stale_reason:null},
      {id:"oltc-1-bus-kv",value:$kv},{id:"oltc-1-at-upper",value:($tap == 8)}]}') || return
  printf '%s' "$observation" | jq -e "$EU_JQ_DEFS $1" >/dev/null
}
eu_plc_requests() { printf 'requests %s %s %s\n' "$1" "$2" "$3" >>"$CALLS"; }
sleep() { printf 'sleep %s\n' "$1" >>"$CALLS"; }

t_run eu_gate_G16
t_expect_eq "$T_STATUS" 0 "G16 accepts the schema-3 reference operating points"
t_expect_eq "$profile_disabled" 0 "successful G16 re-enables the daily profile"
t_run grep -F 'management process POST /api/process/actions {"action":"set_profile","enabled":false}' "$CALLS"
t_expect_eq "$T_STATUS" 0 "profile disable uses enabled, not a numeric value"
t_run grep -F 'management process POST /api/process/actions {"action":"set_profile","enabled":true}' "$CALLS"
t_expect_eq "$T_STATUS" 0 "profile re-enabled explicitly (reset cannot do it)"
for expected in 'fresh 84 G16 nominal reaches tap 2' 'fresh 84 G16 load reaches tap 2' \
  'fresh 276 G16 0.90 reaches tap 8' 'fresh 212 G16 1.10 reaches tap -6'; do
  t_run grep -F "$expected" "$CALLS"
  t_expect_eq "$T_STATUS" 0 "steps × (30 + 2) + 20: $expected"
done
t_expect_eq "$(grep -c '^sleep 37$' "$CALLS")" 4 "each case watches a full extra demand/motion cycle"
t_expect_eq "$(grep '^requests' "$CALLS")" 'requests 10 2 42
requests 10 2 42
requests 10 8 42
requests 10 6 42' "PLC evidence requires two/two/eight/six new requests"
t_expect_eq "$(grep -c '^command oltc-1-step lower rejected limit$' "$CALLS")" 1 \
  "negative settle steps inward to -8 before lower-limit refusal"

# Test actual reference-start helper when reset has already moved off zero.
SIM_TAP=-2
t_run eu_reference_tap 0
t_expect_eq "$T_STATUS" 0 "reference preparation accepts a negative initial tap"
t_expect_eq "$SIM_TAP" 0 "manual preparation returns to reference zero"
RESET_FAIL=true
t_run eu_gate_G16
t_expect_eq "$T_STATUS" 1 "reset failure aborts G16"
t_expect_eq "$profile_disabled" 1 "failed G16 retains profile cleanup ownership"
eu_unpause_owned() { :; }
t_run eu_gate_resources_finish G16
t_expect_eq "$T_STATUS" 0 "aborted gate resource cleanup restores profile"
t_expect_eq "$profile_disabled" 0 "later gates inherit the realistic daily profile"
t_finish
