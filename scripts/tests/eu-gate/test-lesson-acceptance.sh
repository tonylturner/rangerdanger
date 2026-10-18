#!/usr/bin/env bash
# shellcheck disable=SC2329 # stubs are invoked indirectly by the sourced gates
# Offline post-commit admission, timer budget and bad-change/recovery contract.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

PACKAGE_ORIGINAL="$PACKAGE"
PACKAGE="$TMP_DIR/package"
mkdir -p "$PACKAGE/protocols" "$PACKAGE/firewall"
cp "$PACKAGE_ORIGINAL/protocols/points.json" "$PACKAGE/protocols/points.json"
cp "$PACKAGE_ORIGINAL/firewall/improved.json" "$PACKAGE/firewall/improved.json"
t_run eu_cc_loss_deadline
t_expect_eq "$T_OUT" 47 "G7 allows t1+t3, reconnect and ten seconds margin"
jq '.timers.t1_s=40 | .timers.t3_s=60 | .reconnect_interval_ms=2500 | .points=.points[:2]' \
  "$PACKAGE_ORIGINAL/protocols/points.json" >"$PACKAGE/protocols/points.json"
t_run eu_cc_loss_deadline
t_expect_eq "$T_OUT" 113 "G7 follows changed timers and rounds reconnect up"

STATE=$(jq -nc --slurpfile inventory "$PACKAGE/protocols/points.json" '{
 link:{state:"started",since:"2026-10-09T12:00:02Z",
 gi:{state:"complete",started_at:"2026-10-09T12:00:03Z"}},
 points:[$inventory[0].points[] | {ioa:.ioa,value:true,received_at:"2026-10-09T12:00:03Z",
 quality:{iv:false,nt:false,sb:false,bl:false,ov:null},stale_reason:null}]}')
VALID_STATE="$STATE"
POST_STATUS=202
eu_api_post() {
  [ "$1" = /api/operator/reconnect ] && [ -z "$2" ] || return 1
  HTTP_STATUS="$POST_STATUS"
  API_BODY='{"trigger":"operator","requested_at":"2026-10-09T12:00:01Z"}'
}
eu_wait_for_operator() {
  printf '%s' "$STATE" | jq -e "$EU_JQ_DEFS $1" >/dev/null || {
    fail "lesson predicate failed"
    return 1
  }
}
t_run eu_lab_reconnect_cc
t_expect_eq "$T_STATUS" 0 "fresh session/GI passes with changed inventory size"
for mutation in '.link.since="2026-10-09T12:00:00Z"' \
  '.link.gi.state="running"' '.points[0].quality.nt=true' \
  '.points[0].received_at="2026-10-09T12:00:02Z"' '.points=.points[:1]' \
  '.points=[]'; do
  STATE=$(printf '%s' "$VALID_STATE" | jq -c "$mutation")
  t_run eu_lab_reconnect_cc
  t_expect_eq "$T_STATUS" 1 "fresh proof fails closed: $mutation"
done
POST_STATUS=409
t_run eu_lab_reconnect_cc
t_expect_eq "$T_STATUS" 1 "refused reconnect cannot establish new admission"

# Exercise real custom-policy construction: preserve logged CC deny coverage.
t_run eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$TMP_DIR/bad.json"
t_expect_eq "$T_STATUS" 0 "bad request removes only the CC allow"
t_run jq -e '
  all(.firewall.rules[]; .id != "control-centre-to-rtu-iec104")
  and any(.firewall.rules[]; .id == "deny-unauthorized-iec104"
    and (.sourceZones | index("lan4")) != null and .action == "DENY"
    and .log == true and .protocols == [{"name":"tcp","port":"2404"}])
' "$TMP_DIR/bad.json"
t_expect_eq "$T_STATUS" 0 "new rejected CC sessions hit an explicit logged deny"

CALLS=""
FAIL_VALIDATION=0
eu_reset_all() { CALLS="$CALLS reset"; }
eu_apply_named() { CALLS="$CALLS policy:$1"; }
eu_apply_custom() { CALLS="$CALLS bad-policy"; }
eu_lab_reconnect_cc() { CALLS="$CALLS reconnect"; }
eu_lab_wait_good_cc() { CALLS="$CALLS good"; }
eu_wait_for_operator() { CALLS="$CALLS lost"; }
eu_api_post() {
  CALLS="$CALLS close"
  # shellcheck disable=SC2034 # consumed by the sourced bad-change gate
  HTTP_STATUS=202
}
eu_validate_scenario() {
  CALLS="$CALLS validate:$1:$2"
  API_BODY='{"checks":[{"name":"CC link, GI and GOOD station observations","status":"fail"}]}'
  [ "$FAIL_VALIDATION" -eq 0 ]
}
prepare() { CALLS="$CALLS prepare:$1"; }
for slot in eu-firewall-implementation eu-hardening-configurations; do
  CALLS=""
  t_run eu_lab_bad_change "$slot" prepare
  t_expect_eq "$T_STATUS" 0 "$slot grades bad change FAIL and restoration PASS"
  t_expect_eq "$CALLS" \
    " reset policy:improved reconnect bad-policy close lost validate:FAIL:$slot policy:improved good prepare:$slot validate:PASS:$slot policy:weak reset" \
    "bad-change proof and recovery are ordered and cleaned up"
done
# shellcheck disable=SC2034 # consumed by the sourced visibility assertion
API_BODY='{"checks":[{"name":"PLC request","status":"fail"}]}'
t_run eu_lab_visibility_failed
t_expect_eq "$T_STATUS" 1 "an unrelated failed check cannot stand in for lost CC visibility"
CALLS=""
FAIL_VALIDATION=1
t_run eu_lab_bad_change eu-firewall-implementation prepare
t_expect_eq "$T_STATUS" 1 "failure to prove bad-change FAIL stops recovery claims"
t_expect_eq "$CALLS" \
  " reset policy:improved reconnect bad-policy close lost validate:FAIL:eu-firewall-implementation policy:weak reset" \
  "failed proof still restores weak and resets without claiming PASS"
t_finish
