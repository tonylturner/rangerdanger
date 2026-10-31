#!/usr/bin/env bash
# Exercise the real progress waiter with a fake clock and protocol observations.
# shellcheck disable=SC2329 # stubs invoked by sourced helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SINCE=2026-10-15T12:00:00Z
OPERATOR=$(cat "$T_FIXTURES/operator-gi-untimed.json")
CLOCK=0
CLOCK_STEP=13000
POSTS=0
GI_DELAY=0
POST_DELAY=0
GET_DELAY=0
TRACE='[]'
CALLS=""
LAST_TIMEOUT=""
eu_utc() { printf '%s\n' "$SINCE"; }
eu_now_ms() { printf '%s\n' "$CLOCK"; }
sleep() { CLOCK=$((CLOCK + CLOCK_STEP)); }
eu_lab_exec() { :; }
eu_lab_cc() { CALLS="$CALLS $1:$2"; }
eu_get_operator() {
  API_BODY=$(printf '%s' "$OPERATOR" | jq -c '
    (.points[] | select(.id == "oltc-1-tap") | .value) = {value:2,transient:false}')
}
eu_api_post() {
  [ "$1" = /api/operator/gi ] && [ -z "$2" ] || return 1
  POSTS=$((POSTS + 1))
  LAST_TIMEOUT="$3"
  CLOCK=$((CLOCK + POST_DELAY))
  # shellcheck disable=SC2034 # consumed by the sourced GI request helper
  HTTP_STATUS=202
  API_BODY="{\"trigger\":\"operator\",\"requested_at\":\"$SINCE\"}"
}
eu_api_get() {
  [ "$1" = /api/operator/state ] || return 1
  LAST_TIMEOUT="$2"
  CLOCK=$((CLOCK + GET_DELAY))
  # shellcheck disable=SC2034 # consumed by the sourced progress waiter
  API_BODY=$(printf '%s' "$OPERATOR" | jq -c --argjson trace "$TRACE" \
    --argjson clock "$CLOCK" --argjson posts "$POSTS" --argjson gi_delay "$GI_DELAY" \
    --arg since "$SINCE" '
    ($trace | map(select(.at <= $clock)) | last) as $sample
    | (($since | fromdateiso8601) + ($clock / 1000) | floor | todateiso8601) as $received
    | .points[].received_at = $received
    | (.points[] | select(.id == "oltc-1-tap")) |=
        (.value = {value:$sample.tap,transient:($sample.transient // false)}
         | .received_at = ($sample.received // $received)
         | .quality.iv = ($sample.invalid // false))
    | (.points[] | select(.id == "oltc-1-bus-kv")) |=
        (.value = $sample.kv | .quality.iv = ($sample.bus_invalid // $sample.invalid // false)
         | .stale_reason = (if .quality.iv then "measurement_invalid" else null end)
         | .received_at = ($sample.bus_received // $received))
    | if $posts > 0 then
        .link.gi = {state:(if $clock >= $gi_delay then "complete" else "running" end),
          trigger:"operator",started_at:$since,last_complete_at:$since,next_due_at:$since}
      else . end
    | if $sample.reset_epoch then .cache_epoch = "restarted" else . end
  ')
}
reset_trace() {
  TRACE="$1"
  CLOCK=0 POSTS=0 GI_DELAY=0 POST_DELAY=0 GET_DELAY=0 CALLS=""
  CLOCK_STEP=13000
}
wait_trace() { eu_wait_for_regulation true "$SINCE" "test regulation"; }

reset_trace '[{"at":0,"tap":2,"kv":20.429}]'
t_run eu_lab_positive_conduits
t_expect_eq "$T_STATUS" 0 "positive conduit loop accepts an already regulated +2 baseline"
t_expect_eq "$CALLS" ' oltc-1-mode:manual oltc-1-step:lower oltc-1-step:lower oltc-1-mode:auto' \
  "two lower commands precede the automatic correction"
t_expect_eq "$POSTS" 2 "both baseline and restore request a fresh GI"
t_expect_eq "$CLOCK" 0 "already in-band observations require no sleep"

reset_trace '[{"at":0,"tap":0,"kv":20.0},{"at":39000,"tap":1,"kv":20.05},
  {"at":78000,"tap":2,"kv":20.1},{"at":117000,"tap":3,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "steady progress beyond two steps passes"
t_expect_eq "$CLOCK" 117000 "three completed steps can exceed the old baseline and restore budgets"

reset_trace '[{"at":0,"tap":0,"kv":21.0},{"at":39000,"tap":-1,"kv":20.9},
  {"at":78000,"tap":-2,"kv":20.8},{"at":117000,"tap":-3,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "steady completed lower steps also renew the window"

reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":39000,"tap":1,"kv":20.0,"bus_invalid":true},
  {"at":65000,"tap":1,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "a completed raise renews the window even with a simultaneous IV bus"
t_expect_eq "$CLOCK" 65000 "GOOD voltage may recover after the original deadline"

reset_trace '[{"at":0,"tap":0,"kv":21.0},
  {"at":39000,"tap":-1,"kv":21.0,"bus_invalid":true},
  {"at":65000,"tap":-1,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "a completed lower also uses demand from before an IV bus sample"

reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":13000,"tap":0,"kv":20.0,"bus_invalid":true},
  {"at":39000,"tap":1,"kv":20.0,"bus_invalid":true},
  {"at":65000,"tap":1,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "invalid bus samples do not erase the last known demand"

reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":39000,"tap":0,"kv":20.177},
  {"at":78000,"tap":0,"kv":20.177},{"at":117000,"tap":1,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "near-low-edge reports renew the timer hidden by the reporting deadband"
t_expect_eq "$CLOCK" 117000 "ambiguous bus reports can outlast the original stall window"

reset_trace '[{"at":0,"tap":0,"kv":21.0},
  {"at":39000,"tap":0,"kv":20.65},{"at":78000,"tap":-1,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "near-high-edge observations also renew the window"

reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":39000,"tap":0,"kv":20.177,"bus_invalid":true}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "an IV near-edge bus cannot renew the window without tap progress"
t_expect_eq "$CLOCK" 52000 "ambiguous voltage must be GOOD"

reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":39000,"tap":0,"kv":20.177,"bus_received":"2026-10-15T11:59:59Z"}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "a near-edge bus predating this attempt cannot renew the window"
t_expect_eq "$CLOCK" 52000 "ambiguous voltage must be fresh"

reset_trace '[{"at":0,"tap":0,"kv":20.0}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "a stall out of band fails"
t_expect_eq "$CLOCK" 52000 "a stalled regulator fails within one delay + motion + slack window"
t_expect_out stalled "stall failure is explicit"

reset_trace '[{"at":0,"tap":7,"kv":20.0},{"at":39000,"tap":8,"kv":20.1}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "reaching the upper limit below band fails"
t_expect_eq "$CLOCK" 39000 "limit failure does not consume another window"
t_expect_out 'tap limit' "limit failure is explicit"

reset_trace '[{"at":0,"tap":-7,"kv":21.0},{"at":39000,"tap":-8,"kv":20.9}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "reaching the lower limit above band fails"
t_expect_eq "$CLOCK" 39000 "lower limit is also terminal"

reset_trace '[{"at":0,"tap":8,"kv":21.0}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "either out-of-band direction at a tap limit fails"
reset_trace '[{"at":0,"tap":8,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "an in-band tap limit can still pass"

reset_trace '[{"at":0,"tap":2,"kv":20.4}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "an already in-band state passes"
t_expect_eq "$CLOCK" 0 "an already in-band state passes immediately after fresh GI"
t_expect_eq "$LAST_TIMEOUT" 52.000 "the API request is bounded by the shared window"

# Keep the bus beyond the reporting deadband here to isolate tap motion and
# freshness from the separate near-edge renewal rule.
reset_trace '[{"at":0,"tap":0,"kv":20.0},
  {"at":39000,"tap":1,"kv":20.05,"transient":true}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "motion alone cannot renew the deadline"
t_expect_eq "$CLOCK" 52000 "transient observations do not hide a stall"

reset_trace '[{"at":0,"tap":0,"kv":20.0},{"at":39000,"tap":-1,"kv":19.8}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "a completed wrong-way step cannot renew the deadline"
t_expect_eq "$CLOCK" 52000 "wrong-way movement fails within the original window"

reset_trace '[{"at":0,"tap":0,"kv":20.0,"received":"2026-10-15T12:00:00Z"},
  {"at":39000,"tap":1,"kv":20.05,"received":"2026-10-15T12:00:00Z"}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "a retained tap observation is not fresh progress"
t_expect_eq "$CLOCK" 52000 "receipt time must advance before a step can renew the window"

reset_trace '[{"at":0,"tap":2,"kv":20.4,"invalid":true}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "in-band voltage with bad quality cannot pass"
t_expect_eq "$CLOCK" 52000 "bad quality cannot prolong the deadline"

reset_trace '[{"at":0,"tap":2,"kv":20.4,"received":"2026-10-15T11:59:59Z"}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "an in-band tap received before the boundary cannot pass"

reset_trace '[{"at":0,"tap":0,"kv":20.0},{"at":39000,"tap":1,"kv":20.05}]'
GI_DELAY=65000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "GI must complete before success can be reported"
t_expect_eq "$CLOCK" 91000 "a fresh completed step during GI extends only the shared deadline"

reset_trace '[{"at":0,"tap":2,"kv":20.4}]'
GI_DELAY=65000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "an unfinished GI cannot certify an in-band retained snapshot"
t_expect_eq "$CLOCK" 52000 "GI cannot add a separate timeout budget"

reset_trace '[{"at":0,"tap":2,"kv":20.4}]'
POST_DELAY=39000
GI_DELAY=65000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "GI POST latency counts against the inactivity window"
t_expect_eq "$CLOCK" 52000 "POST latency does not start a fresh window"
t_expect_eq "$LAST_TIMEOUT" 13.000 "GET is bounded by the remaining time after POST"

reset_trace '[{"at":0,"tap":2,"kv":20.4}]'
GET_DELAY=53000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "an in-band response arriving after the deadline cannot pass"
t_expect_eq "$LAST_TIMEOUT" 52.000 "network reads receive only the remaining window"

reset_trace '[{"at":0,"tap":2,"kv":20.4},
  {"at":13000,"tap":2,"kv":20.4,"reset_epoch":true}]'
GI_DELAY=13000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "a new cache epoch cannot satisfy the original GI"

reset_trace '[{"at":0,"tap":1,"kv":20.429}]'
t_run eu_lab_wait_regulated_cc 2 "$SINCE"
t_expect_eq "$T_STATUS" 0 "daily profile may settle at a different in-band tap"
reset_trace '[{"at":0,"tap":0,"kv":20.429}]'
t_run eu_lab_wait_regulated_cc 2 "$SINCE"
t_expect_eq "$T_STATUS" 1 "uncorrected perturbation cannot satisfy recovery on a retained voltage"
reset_trace '[{"at":0,"tap":2,"kv":20.0}]'
t_run eu_lab_wait_regulated_cc 2 "$SINCE"
t_expect_eq "$T_STATUS" 1 "a final tap without in-band voltage cannot prove regulation"

# Demand reverses as the profile crosses the band between observations. Real
# progress may continue indefinitely without settling, but cannot evade the cap.
reset_trace '[]'
TRACE=$(jq -nc '[range(0;23) as $i | {at:($i * 39000),
  tap:($i % 2), kv:(if $i % 2 == 0 then 20.0 else 21.0 end)}]')
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "continuing regulation cannot evade the absolute cap"
t_expect_eq "$CLOCK" 832000 "full symmetric tap span times the window caps each wait"
t_expect_out 'absolute cap' "absolute cap failure is explicit"

reset_trace '[{"at":0,"tap":0,"kv":20.177}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "perpetual near-edge ambiguity cannot certify regulation"
t_expect_eq "$CLOCK" 832000 "near-edge renewals remain bounded by the absolute cap"
t_expect_out 'absolute cap' "near-edge observations do not loosen the pass band"

# The gate tolerance is fixed, but changes in the physics reference must flow
# through to the window, band and limits without editing the waiter.
REFERENCE_PACKAGE="$PACKAGE"
mkdir -p "$TMP_DIR/reference/process" "$TMP_DIR/reference/protocols"
cp "$PACKAGE/protocols/points.json" "$TMP_DIR/reference/protocols/points.json"
jq '.configured_delay_s = 2 | .motion_s = 1 | .band_kv = [18,19]
  | .source_0_90[-1].tap = 4' "$PACKAGE/process/reference-cases.json" \
  >"$TMP_DIR/reference/process/reference-cases.json"
PACKAGE="$TMP_DIR/reference"
reset_trace '[{"at":0,"tap":0,"kv":18.5}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "the operating band comes from the physics reference"
reset_trace '[{"at":0,"tap":4,"kv":17.5}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "the tap limit comes from the physics reference"
t_expect_eq "$CLOCK" 0 "reference-derived limit fails immediately"
reset_trace '[{"at":0,"tap":0,"kv":17.5}]'
CLOCK_STEP=1000
t_run wait_trace
t_expect_eq "$T_STATUS" 1 "the modified timing reference still detects a stall"
t_expect_eq "$CLOCK" 23000 "reference delay and motion determine the inactivity window"
# A wider configured reporting deadband makes this otherwise far-out value
# ambiguous. Read it from points.json, independently of the physics reference.
jq '(.points[] | select(.id == "oltc-1-bus-kv") | .deadband) = 0.5' \
  "$REFERENCE_PACKAGE/protocols/points.json" >"$PACKAGE/protocols/points.json"
reset_trace '[{"at":0,"tap":0,"kv":17.6},{"at":39000,"tap":1,"kv":18.5}]'
t_run wait_trace
t_expect_eq "$T_STATUS" 0 "near-edge tolerance follows the bus point reporting deadband"
PACKAGE="$REFERENCE_PACKAGE"
t_finish
