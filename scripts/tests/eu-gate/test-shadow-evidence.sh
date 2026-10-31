#!/usr/bin/env bash
# shellcheck disable=SC2329 # stubs are called by sourced gate functions
# Offline monitor policy, applied counters/events and G29 sequencing regressions.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# shellcheck disable=SC2034 # consumed by eu_policy_has_no_ephemeral_allow
EPHEMERAL_LOW=32768 EPHEMERAL_HIGH=60999
POLICY="$TMP_DIR/monitor-policy.json"
jq --slurpfile monitors "$T_FIXTURES/monitor-rules.json" '
  .dataplane.dpiEnabled = true
  | .firewall.rules |= (map(select(.id | startswith("observe-cc-") | not)) + $monitors[0])
' "$PACKAGE/firewall/improved.json" >"$POLICY"
t_run eu_policy_has_no_ephemeral_allow "$POLICY"
t_expect_eq "$T_STATUS" 0 "explicit-monitor IEC104 observations coexist with safe L4 rules"
for mutation in \
  '.firewall.rules[-1].ics.mode="enforce"' \
  'del(.firewall.rules[-1].ics.mode)' \
  '.firewall.rules[-1].ics.protocol="modbus"' \
  '.firewall.rules[-1].ics={}' \
  '.firewall.rules[-1].ics=null' \
  '.firewall.rules[-1].ics=[]' \
  '.firewall.rules[-1].ics=false' \
  '.extra.ics={"protocol":"iec104","mode":"monitor"}' \
  '.firewall.rules[0].protocols=[{"name":"tcp","port":"32768-60999"}]'; do
  jq "$mutation" "$POLICY" >"$TMP_DIR/invalid.json"
  t_run eu_policy_has_no_ephemeral_allow "$TMP_DIR/invalid.json"
  t_expect_eq "$T_STATUS" 1 "policy rejects unsafe or misplaced ICS/L4: $mutation"
done
# A monitor ALLOW is not an admission grant, including at an ephemeral port.
jq '.firewall.rules[-1].protocols[0].port="40000"' "$POLICY" >"$TMP_DIR/observe-port.json"
t_run eu_policy_has_no_ephemeral_allow "$TMP_DIR/observe-port.json"
t_expect_eq "$T_STATUS" 0 "observation is not counted as an ephemeral L4 grant"

BASE="$TMP_DIR/base.json"
jq '{available:true,snapshotVersion:"installed",since:"2026-10-15T11:00:00Z",
  unit:"messages",rules:map({ruleId:.id,shadowMatches:0,wouldAllow:0,wouldDeny:0})}' \
  "$T_FIXTURES/monitor-rules.json" >"$BASE"
t_run eu_shadow_check baseline "$BASE"
t_expect_eq "$T_STATUS" 0 "zero baseline is valid only with availability and all applied IDs"
for mutation in '.available=false' 'del(.available)' '.rules=.rules[:2]' \
  '.rules += [.rules[0]]' 'del(.rules[0].wouldDeny)' \
  '.rules[0].shadowMatches=-1' '.rules[0].wouldAllow=true' \
  '.since="bad"' '.snapshotVersion=""' '.unit="objects"'; do
  jq "$mutation" "$BASE" >"$TMP_DIR/invalid.json"
  t_run eu_shadow_check baseline "$TMP_DIR/invalid.json"
  t_expect_eq "$T_STATUS" 1 "baseline fails closed: $mutation"
done
AFTER="$TMP_DIR/after.json"
jq '.rules[0] |= (.shadowMatches=1 | .wouldDeny=1)' "$BASE" >"$AFTER"
t_run eu_shadow_check delta "$BASE" "$AFTER" breaker
t_expect_eq "$T_STATUS" 0 "one breaker wouldDeny message suffices (not exactly two)"
for mutation in '.snapshotVersion += "-changed"' '.since="2026-10-15T11:00:01Z"' \
  '.available=false' '.rules=[]'; do
  jq "$mutation" "$AFTER" >"$TMP_DIR/invalid.json"
  t_run eu_shadow_check delta "$BASE" "$TMP_DIR/invalid.json" breaker
  t_expect_eq "$T_STATUS" 1 "counter lifetime/presence failure is never zero: $mutation"
done
jq '.rules[1] |= (.shadowMatches=1 | .wouldAllow=1)' "$BASE" >"$AFTER"
t_run eu_shadow_check delta "$BASE" "$AFTER" lower
t_expect_eq "$T_STATUS" 0 "lower hits step but not raise-only"
t_run eu_shadow_check delta "$BASE" "$AFTER" raise
t_expect_eq "$T_STATUS" 1 "raise requires the narrower rule too"
jq '.rules[2] |= (.shadowMatches=1 | .wouldAllow=1)' "$AFTER" >"$TMP_DIR/raised.json"
t_run eu_shadow_check delta "$BASE" "$TMP_DIR/raised.json" lower
t_expect_eq "$T_STATUS" 1 "lower must not hit raise-only"
jq '.rules[2] |= (.shadowMatches=1 | .wouldDeny=1)' "$AFTER" >"$TMP_DIR/wrong-decision.json"
t_run eu_shadow_check delta "$BASE" "$TMP_DIR/wrong-decision.json" lower
t_expect_eq "$T_STATUS" 1 "lower requires zero matches, not just zero wouldAllow"
t_run eu_shadow_check delta "$BASE" "$TMP_DIR/raised.json" raise
t_expect_eq "$T_STATUS" 0 "raise hits both independent observations"
t_run eu_shadow_check delta "$TMP_DIR/raised.json" "$BASE" raise
t_expect_eq "$T_STATUS" 1 "regressing counters cannot pass"

EVENT="$TMP_DIR/event.json"
START="2026-10-15T12:00:00Z"
END="2026-10-15T12:00:01Z"
jq -n '{historyTruncated:false,events:[{kind:"ics.decision",
  timestamp:"2026-10-15T12:00:00.5Z",attributes:{mode:"monitor",
  ruleId:"observe-cc-breaker-command",decision:"would_drop",action:"DENY",reason:"rule"}}]}' >"$EVENT"
t_run eu_shadow_check events "$EVENT" observe-cc-breaker-command "$START" "$END"
t_expect_eq "$T_STATUS" 0 "matching monitor decision lies inside the range-clock window"
for mutation in 'del(.historyTruncated)' '.engineError="offline"' \
  '.events[0].timestamp="2026-10-15T11:59:59Z"' \
  '.events[0].timestamp="2026-10-15T12:00:02Z"' \
  '.events[0].attributes.mode="enforce"' \
  '.events[0].attributes.ruleId="other"' \
  '.events[0].attributes.decision="deny"' '.events=[]'; do
  jq "$mutation" "$EVENT" >"$TMP_DIR/invalid.json"
  t_run eu_shadow_check events "$TMP_DIR/invalid.json" observe-cc-breaker-command "$START" "$END"
  t_expect_eq "$T_STATUS" 1 "event evidence fails closed: $mutation"
done
jq '.historyTruncated=true' "$EVENT" >"$TMP_DIR/truncated.json"
t_run eu_shadow_check events "$TMP_DIR/truncated.json" observe-cc-breaker-command "$START" "$END"
t_expect_eq "$T_STATUS" 0 "a retained positive witness remains valid despite conservative overflow warning"
jq '.historyTruncated=true | .events=[]' "$EVENT" >"$TMP_DIR/truncated.json"
t_run eu_shadow_check events "$TMP_DIR/truncated.json" observe-cc-breaker-command "$START" "$END"
t_expect_eq "$T_STATUS" 1 "a retention gap without a positive witness never becomes zero or success"
jq '.events=[] | .nextCursor="older cursor"' "$EVENT" >"$TMP_DIR/paged.json"
t_run eu_shadow_check events "$TMP_DIR/paged.json" observe-cc-breaker-command "$START" "$END"
t_expect_eq "$T_OUT" "older cursor" "documented cursor requests continuation, not a missing-hit success"
t_run eu_shadow_check events "$EVENT" observe-cc-breaker-command \
  "2026-10-15T12:00:00.500000001Z" "$END"
t_expect_eq "$T_STATUS" 1 "event one nanosecond before the command window cannot pass"
t_run eu_shadow_check events "$EVENT" observe-cc-breaker-command \
  "$START" "2026-10-15T12:00:00.499999999Z"
t_expect_eq "$T_STATUS" 1 "event one nanosecond after the command window cannot pass"

# Exercise the real G29 orchestration, phase comparisons and event readers.
ORIGINAL_PACKAGE="$PACKAGE"
PACKAGE="$TMP_DIR/package"
mkdir -p "$PACKAGE/firewall" "$PACKAGE/process"
cp "$POLICY" "$PACKAGE/firewall/improved.json"
cp "$ORIGINAL_PACKAGE/firewall/weak.json" "$PACKAGE/firewall/weak.json"
cp "$ORIGINAL_PACKAGE/process/reference-cases.json" "$PACKAGE/process/reference-cases.json"
CALLS=""
APPLIED_POLICY=weak
COUNTS=$(cat "$BASE")
FAIL_AT=""
HEALTH_MUTATION="."
CONFIG_MUTATION="."
TAP=0
POSITION=closed
AUTO=true
sleep() { :; }
docker() { fail "offline test leaked a Docker command"; return 99; }
eu_apply_named() { CALLS="$CALLS apply:$1"; APPLIED_POLICY="$1"; }
eu_set_profile() { CALLS="$CALLS profile:$1"; }
eu_reset_all() {
  CALLS="$CALLS reset"
  APPLIED_POLICY=weak
  POSITION=closed
  AUTO=true
  TAP=$(jq -er '.initial_tap' "$PACKAGE/process/reference-cases.json")
}
eu_lab_reconnect_cc() { CALLS="$CALLS reconnect"; }
eu_reference_tap() { CALLS="$CALLS reference:$1"; TAP="$1"; AUTO=false; }
eu_firewall_config() { jq "$CONFIG_MUTATION" "$PACKAGE/firewall/$APPLIED_POLICY.json"; }
eu_get_field() {
  API_BODY=$(jq -n --argjson tap "$TAP" --arg position "$POSITION" --argjson auto "$AUTO" \
    "{devices:{\"breaker-1\":{position:\$position},\"oltc-1\":{tap:\$tap,moving:false,auto:\$auto}},
      process:{buses:{s1:{live:(\$position == \"closed\")}}}}")
}
eu_wait_for_field() {
  eu_get_field || return
  printf '%s' "$API_BODY" | jq -e "$1" >/dev/null
}
eu_lab_cc() {
  CALLS="$CALLS command:$1:$2"
  case "$1:$2" in
    feeder-breaker-command:*)
      if [ "$2:$POSITION" = close:closed ]; then
        fail "execute rejected (negative ACT-CON): breaker already closed"
        return 1
      fi
      if [ "$2" = close ]; then POSITION=closed; else POSITION=open; fi
      COUNTS=$(printf '%s' "$COUNTS" | jq '.rules[0] |= (.shadowMatches+=1 | .wouldDeny+=1)') ;;
    oltc-1-step:lower)
      TAP=$((TAP - 1))
      COUNTS=$(printf '%s' "$COUNTS" | jq '.rules[1] |= (.shadowMatches+=1 | .wouldAllow+=1)') ;;
    oltc-1-step:raise)
      TAP=$((TAP + 1))
      COUNTS=$(printf '%s' "$COUNTS" | jq '.rules[1,2] |= (.shadowMatches+=1 | .wouldAllow+=1)') ;;
    oltc-1-mode:auto) AUTO=true ;;
    *) return 99 ;;
  esac
}
eu_shadow_get() {
  CALLS="$CALLS get:$1"
  case "$1" in
    /health)
      printf '%s\n' '{"status":"ok","enforcement":{"state":"enforcing","reason":""},
        "iec104Enforcement":{"healthy":true,"active":false,"reason":""}}' \
        | jq "$HEALTH_MUTATION" >"$2" ;;
    /firewall/shadow-counters)
      printf '%s' "$COUNTS" | jq \
        --arg fail "$FAIL_AT" --arg file "$2" '
        if $fail != "" and ($file | contains($fail)) then .available=false else . end' >"$2" ;;
    /events*)
      jq -n --slurpfile rules "$T_FIXTURES/monitor-rules.json" '{
        historyTruncated:false,events:[$rules[0][] | {kind:"ics.decision",
        timestamp:"2026-10-15T12:00:00.5Z",attributes:{mode:"monitor",ruleId:.id,
        decision:(if .action == "DENY" then "would_drop" else "would_allow" end),
        action:.action,reason:"rule"}}]}' >"$2" ;;
    *) return 99 ;;
  esac
}
t_run eu_gate_G29
t_expect_eq "$T_STATUS" 0 "G29 proves field results, monitor counters and all command-window events"
t_expect_fails 0 "complete synthetic G29 has no gate failures"
t_expect_out "G29 lower monitor deltas" "lower selector comparison is an explicit gate result"
t_expect_eq "${CALLS%% get:/health*}" \
  " profile:false reset apply:improved reconnect reference:0" \
  "reset precedes Improved apply and fresh CC session before health and counter baseline"
t_expect_eq "$TAP:$AUTO:$POSITION" "0:true:closed" "cleanup restores reference tap, auto and CB1 command"
for FAIL_AT in baseline breaker lower raise final; do
  CALLS=""
  COUNTS=$(cat "$BASE")
  t_run eu_gate_G29
  t_expect_eq "$T_STATUS" 1 "G29 rejects unavailable $FAIL_AT counters"
  t_expect_eq "$TAP:$AUTO:$POSITION" "0:true:closed" "failed $FAIL_AT proof still restores field state"
  t_expect_eq "${CALLS##* profile:true}" " apply:weak" "failed $FAIL_AT proof restores profile and weak"
done
FAIL_AT=""
for HEALTH_MUTATION in '.iec104Enforcement.healthy=false' \
  '.enforcement.state="degraded"' 'del(.iec104Enforcement)'; do
  CALLS=""
  t_run eu_gate_G29
  t_expect_eq "$T_STATUS" 1 "G29 refuses unhealthy/unknown engine readiness: $HEALTH_MUTATION"
  t_expect_fails 1 "failed health reports only the health failure"
  t_expect_out "G29 DPI/decoder/queue or applied-engine health is not ready" \
    "health failure remains the reported cause"
  t_expect_eq "${CALLS#* command:feeder-breaker-command:}" "$CALLS" \
    "failed health issues no breaker command, including a redundant close"
  t_expect_eq "$TAP:$AUTO:$POSITION" "0:true:closed" "failed health preserves reset field state"
done
HEALTH_MUTATION="."
for CONFIG_MUTATION in '.dataplane.dpiEnabled=false' '.dataplane.nfqueueGroup=0' \
  '.dataplane.dpiIcsProtocols.iec104=false' '.firewall.rules[-1].ics.mode="enforce"'; do
  t_run eu_gate_G29
  t_expect_eq "$T_STATUS" 1 "G29 refuses unapplied/disabled monitoring: $CONFIG_MUTATION"
done
t_finish
