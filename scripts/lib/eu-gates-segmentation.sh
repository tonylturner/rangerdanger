# shellcheck shell=bash
# Firewall denial, exact-session cuts, CC recovery and plan proof (G4-G10, G28).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

# Inventory identity, not a hard-coded point count or a vacuous all([]).
eu_station_shape() {
  local ioas
  ioas=$(jq -ce '[.points[].ioa] | sort | if length > 0 and length == (unique | length) then . else error("invalid station inventory") end' "$PACKAGE/protocols/points.json") || return
  printf '([.points[].ioa] | sort) == %s' "$ioas"
}

# Prove the selected plan independently of hardened acceptance. Every request
# and expected row/count/label/gap comes from the backend-generated goldens.
eu_plan_case() {
  local selection="$1" request
  local golden="$ROOT/frontend/lib/testdata/plans/eu-$selection.json"
  local oracle="$ROOT/scripts/tests/fixtures"
  request=$(jq -ce -L "$oracle" 'include "plan-gate"; plan_request' "$golden") || return 1
  printf '%s\n' "$request" >"$TMP_DIR/G28-$selection-request.json" || return 1
  # shellcheck disable=SC2034 # consumed by eu-cleanup.sh
  state_touched=1
  eu_api_post /api/firewall/apply-plan "$request" 60 || return 1
  [ "$HTTP_STATUS" = 200 ] || return 1
  printf '%s' "$API_BODY" | jq -e -L "$oracle" --slurpfile golden "$golden" \
    'include "plan-gate"; plan_applied($golden[0])' || return 1
  sleep 3
  eu_api_post /api/firewall/validation-report '{}' 90 || return 1
  [ "$HTTP_STATUS" = 200 ] || return 1
  printf '%s' "$API_BODY" | jq -e -L "$oracle" --slurpfile golden "$golden" \
    'include "plan-gate"; plan_report($golden[0])' || return 1
  pass "G28 $selection plan: faithful proof, golden hardened acceptance and exact prevention gaps; no skipped tuples"
}

eu_gate_G28() {
  local selection result=0
  for selection in full partial; do
    if ! eu_plan_case "$selection"; then
      fail "G28 $selection plan apply/report did not match its golden: HTTP ${HTTP_STATUS:-none} ${API_BODY:-}"
      result=1
      break
    fi
  done
  # Named apply also asserts the plan provenance has cleared, even on failure.
  eu_apply_named weak || result=1
  return "$result"
}

# Silent conntrack deletion is detected by TESTFR idle + response timeout.
eu_cc_loss_deadline() {
  jq -er '.timers.t1_s + .timers.t3_s + (.reconnect_interval_ms / 1000 | ceil) + 10' "$PACKAGE/protocols/points.json"
}

eu_gate_G4() {
  if eu_apply_named weak; then
    eu_run_kali_gi G4 0 || true
  else
    fail "G4 cannot perform weak-policy Kali GI"
  fi
}

eu_gate_G5() {
  local event_started
  if ! eu_apply_named improved; then
    fail "G5 cannot test improved-policy denial"
    return
  fi
  event_started=$(eu_utc +%s%N) || event_started=""
  if [ -z "$event_started" ]; then
    fail "G5 cannot establish a precise event timestamp cursor"
  else
    eu_run_kali_gi G5 2 || true
    eu_fresh_deny_event 10.10.10.50 "$event_started" 30 || true
  fi
  if eu_get_operator; then
    eu_assert_jq "$API_BODY" '.link.state == "started"' "G5 CC IEC104 link remains started"
  else
    fail "G5 CC state unavailable"
  fi
  # G5 proves physical denial only; validator slots are exercised by lab walks.
}

eu_gate_G6() {
  local g6_kill_verified=0
  # §13 requires exactly one Kali tuple once the monitor runs, so it starts only
  # after earlier Kali sessions (the G4 one-shot's TIME_WAIT) have expired.
  if ! eu_wait_session_history_drained 10.10.10.50 "$RTU_IP" 2404 "G6 Kali session baseline"; then
    fail "G6 monitor not started without a clean Kali-to-RTU session baseline"
    return
  fi
  if ! eu_apply_named weak || ! eu_monitor_start; then
    fail "G6 could not start a weak-policy persistent monitor"
    return
  fi
  if eu_g6_cut_monitor_session; then
    g6_kill_verified=1
  fi
  if [ "$g6_kill_verified" -eq 1 ]; then
    eu_run_kali_gi G6-reconnect 2 || true
  else
    fail "G6 reconnect denial was not tested because the established-session kill was unverified"
  fi
}

# G6 body: the established monitor survives improved, then its exact tuple is
# killed and it must exit 3 with no later ASDU.
eu_g6_cut_monitor_session() {
  local asdus_before kali_tuple asdu_observation_valid=1 kill_started_ms kill_epoch
  if eu_monitor_wait_event connected 15 && eu_monitor_wait_event gi_complete 15; then
    pass "G6 monitor connected and completed GI under weak"
  else
    fail "G6 monitor failed to establish its IEC104 session"
  fi
  if ! eu_monitor_asdu_count; then
    fail "G6 monitor ASDU JSON could not be parsed"
    return 1
  fi
  asdus_before=$MONITOR_ASDUS
  eu_apply_named improved || return 1
  if ! eu_monitor_read || [ "$MONITOR_LINES" -eq 0 ] \
    || ! printf '%s\n' "$MONITOR_BODY" | jq -s -e --arg peer "$RTU_IP:2404" '
      [.[] | select(.event == "connected")] as $connected
      | ($connected | length) == 1
        and $connected[0].local == null and $connected[0].peer == $peer
    ' >/dev/null 2>&1; then
    fail "G6 monitor must emit exactly one connected event with local=null and peer=$RTU_IP:2404"
    return 1
  fi
  if ! eu_read_sessions 2404 10.10.10.50 \
    || ! eu_session_tuple "$SESSION_BODY" 10.10.10.50 "$RTU_IP" 2404; then
    fail "G6 session lookup did not yield exactly one Kali-to-RTU tuple"
    return 1
  fi
  kali_tuple="$SESSION_TUPLE"
  pass "G6 non-empty dport 2404 session list contains the monitor's exact Kali tuple"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    sleep 1
    if ! eu_monitor_asdu_count; then
      asdu_observation_valid=0
      break
    fi
  done
  if [ "$asdu_observation_valid" -eq 1 ] && [ "${MONITOR_ASDUS:-0}" -gt "$asdus_before" ]; then
    pass "G6 established IEC104 session continued receiving ASDUs for 10s after improved apply"
  else
    fail "G6 established monitor received no new ASDUs during the 10s observation"
  fi
  kill_started_ms=$(eu_now_ms) || kill_started_ms=""
  kill_epoch=$(eu_utc +%s%N) || kill_epoch=""
  if ! [[ "$kill_started_ms" =~ ^[0-9]+$ ]] || [ -z "$kill_epoch" ]; then
    fail "G6 could not timestamp conntrack kill"
    return 1
  fi
  eu_kill_tuple "$kali_tuple" \
    && eu_monitor_finish 3 30 "$kill_started_ms" \
    && eu_monitor_no_asdu_after "$kill_epoch"
}

eu_gate_G7() {
  local before_g7 g7_policy g7_kill_started_ms deadline shape
  deadline=$(eu_cc_loss_deadline) || return
  shape=$(eu_station_shape) || return
  g7_loss_verified=0
  if ! eu_get_operator; then
    fail "G7 operator state unavailable before restrictive policy"
    return
  fi
  before_g7="$API_BODY"
  if ! printf '%s' "$before_g7" | jq -e '.link.state == "started"' >/dev/null 2>&1; then
    fail "G7 operator link state was not started before applying the CC restriction: $before_g7"
    return
  fi
  g7_policy="$TMP_DIR/improved-without-cc.json"
  eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$g7_policy" || return
  if eu_policy_has_no_ephemeral_allow "$g7_policy"; then
    pass "G7 custom policy has no ICS keys or ephemeral destination-port allow"
  else
    fail "G7 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
  fi
  eu_commit_manual_policy "$g7_policy" "G7 improved minus CC allow" || return
  eu_check_nft_chain G7-custom || true
  if ! eu_read_sessions 2404 "$CC_IP" \
    || ! eu_session_tuple "$SESSION_BODY" "$CC_IP" "$RTU_IP" 2404; then
    fail "G7 session lookup did not yield exactly one CC-to-RTU tuple"
    return
  fi
  pass "G7 exact established CC tuple exists before kill"
  g7_kill_started_ms=$(eu_now_ms) || g7_kill_started_ms=""
  if ! [[ "$g7_kill_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G7 cannot establish monotonic timeout before conntrack kill"
  elif eu_kill_tuple "$SESSION_TUPLE" && eu_wait_for_operator \
    "($shape) and .link.state != \"started\" and all(.points[]; .quality.iv and .quality.nt and .stale_reason == \"link_lost\")" \
    "$deadline" "G7 CC link loss with every inventory point link_lost" "$g7_kill_started_ms"; then
    g7_loss_verified=1
    pass "G7 CC link is not started and all points are IV+NT link_lost"
  fi
}

# The same isolation assertion covers autonomous regulation and local faults.
eu_g8_cache_unchanged() {
  local body="$1" epoch="$2" points="$3" label="$4" current
  current=$(eu_get_point_snapshot "$body" station) || current=""
  if [ -z "$points" ] || [ "$current" != "$points" ] \
    || ! printf '%s' "$body" | jq -e --arg epoch "$epoch" \
      '.cache_epoch == $epoch and .link.state != "started"' >/dev/null; then
    fail "G8 operator cache changed or CC link reconnected during $label"
    return 1
  fi
  pass "G8 operator values/received_at/source_time and cache_epoch unchanged; CC link down during $label"
}

# One shared deadline includes acquisition, any already accepted motion, the
# forced demand, and the completed step. Never reset devices or issue CC writes.
eu_g8_wait_plc_step() {
  local epoch="$1" points="$2" deadline="$3" low="$4" high="$5"
  local truth tap="" direction source cursor generation now remaining timeout demanded=false
  while :; do
    eu_get_operator || { fail "G8 operator state unavailable during PLC regulation"; return 1; }
    eu_g8_cache_unchanged "$API_BODY" "$epoch" "$points" "PLC regulation" || return
    eu_get_field || { fail "G8 field truth unavailable during PLC regulation"; return 1; }
    truth="$API_BODY"
    now=$(eu_now_ms) || return
    if [ "$now" -ge "$deadline" ]; then
      fail "G8 no completed PLC-origin tap step within one demand cycle"
      return 1
    fi
    if ! printf '%s' "$truth" | jq -e '
      .devices["breaker-1"].position == "closed"
      and (.devices["oltc-1"] | .auto == true and .remote_enabled == true
        and .blocked == false and (.tap | type == "number" and floor == .))
    ' >/dev/null; then
      fail "G8 PLC regulation requires CB1 closed and an automatic, remote, unblocked OLTC"
      return 1
    fi
    if [ -z "$tap" ]; then
      if printf '%s' "$truth" | jq -e '.devices["oltc-1"].moving == false' >/dev/null; then
        tap=$(printf '%s' "$truth" | jq -er '.devices["oltc-1"].tap') || return
        # Choose the extreme that demands a step away from the nearer limit.
        source=1.10 direction=-1
        if [ "$tap" -lt 0 ]; then source=0.90 direction=1; fi
        eu_management oltc-1 GET '/api/audit?after=0' || return
        cursor=$(printf '%s' "$API_BODY" | jq -er '.latest_seq // 0') || return
        generation=$(printf '%s' "$API_BODY" | jq -er '.generation') || return
        eu_action process set_source "$source" || return
        continue
      fi
    else
      # Demand must be observed in solved field truth, not inferred merely from
      # an accepted process action or the retained operator voltage.
      if printf '%s' "$truth" | jq -e --argjson source "$source" --argjson low "$low" \
        --argjson high "$high" --argjson direction "$direction" '
        .process.truth_stale == false and .process.source_pu == $source
        and .process.profile.enabled == false
        and (.devices["oltc-1"].measurements | .valid == true
          and (.voltage_kv_ll | type == "number")
          and (if $direction == 1 then .voltage_kv_ll < $low else .voltage_kv_ll > $high end))
      ' >/dev/null; then demanded=true; fi
      if [ "$demanded" = true ] && printf '%s' "$truth" | jq -e --argjson tap "$tap" \
        --argjson direction "$direction" --argjson generation "$generation" '
        .devices["oltc-1"] | .generation == $generation and .moving == false
          and .tap == $tap + $direction
      ' >/dev/null; then
        # Stop after the first completed step, rather than spending another
        # cycle settling. The helper checks cursor, generation and PLC peer.
        eu_plc_requests "$cursor" 1 "$generation" G8 || return
        if ! printf '%s' "$API_BODY" | jq -e --argjson cursor "$cursor" --argjson tap "$tap" \
          --argjson direction "$direction" --argjson truth "$truth" '
          [.entries[] | select(.seq > $cursor and .operation == "plc_request"
            and .result == "accepted" and .context.protocol == "modbus"
            and (.context.peer | startswith("10.30.30.30:"))
            and .before.tap == $tap and .after.target_tap == $tap + $direction
            and .after.moving == true)] as $requests
          | ($requests | length) == 1
          and any(.entries[]; .seq > $requests[0].seq and .operation == "motion_complete"
            and .result == "accepted" and .before.moving == true
            and .before.target_tap == $requests[0].after.target_tap
            and .after.moving == false and .after.tap == $truth.devices["oltc-1"].tap
            and .after.revision <= $truth.devices["oltc-1"].revision)
        ' >/dev/null; then
          fail "G8 PLC request lacks a matching completed tap in audit and field truth"
          return 1
        fi
        now=$(eu_now_ms) || return
        if [ "$now" -ge "$deadline" ]; then
          fail "G8 PLC completion evidence exceeded the demand-cycle deadline"
          return 1
        fi
        pass "G8 PLC-origin tap step toward the band completed while CC had no view"
        return 0
      fi
    fi
    now=$(eu_now_ms) || return
    remaining=$((deadline - now))
    [ "$remaining" -gt 0 ] || continue
    [ "$remaining" -le 1000 ] || remaining=1000
    timeout=$(printf '%d.%03d' "$((remaining / 1000))" "$((remaining % 1000))")
    sleep "$timeout"
  done
}

eu_g8_regulate_without_cc() {
  local epoch="$1" points="$2" source profile low high budget start result=0
  read -r low high budget < <(jq -er --argjson slack "$EU_REGULATION_SLACK_S" '
    [.band_kv[0], .band_kv[1], (.configured_delay_s + .motion_s + $slack)] | @tsv
  ' "$PACKAGE/process/reference-cases.json") || return
  eu_get_field || { fail "G8 cannot snapshot process controls before PLC regulation"; return 1; }
  source=$(printf '%s' "$API_BODY" | jq -er '.process.source_pu | numbers') || return
  profile=$(printf '%s' "$API_BODY" | jq -r '.process.profile.enabled | booleans') || return
  if [ "$profile" != true ] && [ "$profile" != false ]; then
    fail "G8 process profile state unavailable before PLC regulation"
    return 1
  fi
  start=$(eu_now_ms) || return
  if ! eu_set_profile false \
    || ! eu_g8_wait_plc_step "$epoch" "$points" "$((start + budget * 1000))" "$low" "$high"; then
    result=1
  fi
  # Restore both drivers even on timeout or isolation failure, before G8 trips
  # CB1. Neither restoration waits for a second PLC demand cycle.
  eu_action process set_source "$source" || result=1
  eu_set_profile "$profile" || result=1
  if [ "$result" -eq 0 ]; then
    if ! eu_get_operator; then
      fail "G8 operator state unavailable after process restoration"
      result=1
    else
      eu_g8_cache_unchanged "$API_BODY" "$epoch" "$points" "process restoration" || result=1
    fi
  fi
  return "$result"
}

eu_gate_G8() {
  local g8_before g8_epoch g8_points active g8_truth g8_operator g8_seq1 g8_seq2
  if [ "$g7_loss_verified" -ne 1 ]; then
    fail "G8 cannot test cache isolation without the verified G7 CC disconnect"
    return
  fi
  if ! eu_get_operator; then
    fail "G8 operator state unavailable before the fault sequence"
    return
  fi
  g8_before="$API_BODY"
  g8_epoch=$(printf '%s' "$g8_before" | jq -r '.cache_epoch // empty')
  g8_points=$(eu_get_point_snapshot "$g8_before" station) || g8_points=""
  if [ -z "$g8_epoch" ] || [ -z "$g8_points" ] \
    || ! printf '%s' "$g8_before" | jq -e "$EU_JQ_DEFS four_points" >/dev/null 2>&1; then
    fail "G8 cache epoch, four-point shape, or point snapshot is empty/invalid"
    return 1
  fi
  eu_g8_regulate_without_cc "$g8_epoch" "$g8_points" || return
  for active in true false true; do
    if [ "$active" = true ]; then
      eu_action_step process clear_fault "G8 clear before trip" || return
      eu_action_step breaker-1 reset "G8 reclose before trip" || return
      eu_wait_for_field '.devices["breaker-1"].measurements.valid == true' 10 "G8 reclose solved" || return
      eu_action_step process fault_s1 "G8 sim fault active=$active" || true
    else
      eu_action_step process clear_fault "G8 sim fault active=$active" || true
    fi
    if [ "$active" = "true" ]; then
      eu_wait_for_field '.devices["breaker-1"].position == "open" and .devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G8 local protection and open solver" || true
    else
      eu_wait_for_field '.process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G8 solver after fault cleared" || true
    fi
    if ! eu_get_field; then
      fail "G8 physical state/solver response unavailable during fault active=$active"
      continue
    fi
    g8_truth="$API_BODY"
    if ! eu_get_operator; then
      fail "G8 operator-state route was not HTTP 200 JSON during fault active=$active"
      continue
    fi
    g8_operator="$API_BODY"
    eu_g8_cache_unchanged "$g8_operator" "$g8_epoch" "$g8_points" "fault active=$active" || true
    if [ "$active" = "true" ]; then
      eu_assert_jq "$g8_truth" '.devices["breaker-1"].position == "open" and .devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1' "G8 physical state and independent solver show open/tripped"
    fi
  done
  if ! eu_get_field; then
    fail "G8 solver state response failed"
    return
  fi
  g8_seq1=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
  sleep 2
  if ! eu_get_field; then
    fail "G8 second solver state response failed"
    return
  fi
  g8_seq2=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
  if [[ "$g8_seq1" =~ ^[0-9]+$ ]] && [[ "$g8_seq2" =~ ^[0-9]+$ ]] \
    && [ "$g8_seq2" -gt "$g8_seq1" ]; then
    pass "G8 solver solve_seq advances independently while operator cache remains stale"
  else
    fail "G8 solver solve_seq failed to advance ($g8_seq1 -> $g8_seq2)"
  fi
}

eu_gate_G9() {
  local g9_truth shape
  shape=$(eu_station_shape) || return
  if ! eu_apply_named improved; then
    fail "G9 cannot restore improved policy"
    return
  fi
  if [ "$g7_loss_verified" -ne 1 ]; then
    fail "G9 cannot verify recovery without the verified G7 CC disconnect"
    return
  fi
  # GI refresh and measurement validity are distinct: PF is undefined at zero
  # power after G8 leaves CB1 open, but its IV report still proves recovery.
  if eu_wait_for_operator "($shape) and .link.state == \"started\" and .link.gi.state == \"complete\"
    and all(.points[]; .value != null and .quality.nt == false
      and .quality.sb == false and .quality.bl == false
      and (.quality.ov == null or .quality.ov == false)
      and ((.quality.iv == false and .stale_reason == null)
        or (.quality.iv == true and .stale_reason == \"measurement_invalid\")))" \
    30 "G9 CC reconnect/GI and every inventory point refreshed"; then
    pass "G9 restored IEC104 observations after GI"
  fi
  if ! eu_get_field; then
    fail "G9 physical state unavailable after reconnect"
    return
  fi
  g9_truth="$API_BODY"
  if eu_get_operator; then
    # shellcheck disable=SC2016
    eu_assert_jq_truth "$API_BODY" "$g9_truth" '
      point("feeder-breaker-position").value == $truth.devices["breaker-1"].position
      and point("feeder-protection-trip").value == $truth.devices["breaker-1"].protection_tripped
      and point("f2-position").value == $truth.devices["f2-breaker"].position
      and point("f2-protection-trip").value == $truth.devices["f2-breaker"].protection_tripped
      and $truth.devices["breaker-1"].position == "open" and $truth.devices["breaker-1"].protection_tripped == true
      and ($truth.devices["oltc-1"].tap | type) == "number"
      and point("oltc-1-tap").value.value == $truth.devices["oltc-1"].tap
    ' "G9 restored operator state equals open/tripped physical state and OLTC tap"
  else
    fail "G9 operator state unavailable after reconnect"
  fi
}

eu_gate_G10() {
  eu_reset_all || return
  eu_apply_named improved || return
  eu_lab_validation_prepare eu-validation-evidence || return
  eu_validate_scenario PASS eu-validation-evidence
}
