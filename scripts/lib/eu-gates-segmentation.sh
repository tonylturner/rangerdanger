# shellcheck shell=bash
# Firewall denial, exact-session cuts and CC cache isolation/recovery (G4-G10).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

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
  event_started=$(python3 -c 'import time; print(time.time_ns())') || event_started=""
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
  if ! eu_monitor_read || [ "$MONITOR_LINES" -eq 0 ] ||
    ! printf '%s\n' "$MONITOR_BODY" | jq -s -e --arg peer "$RTU_IP:2404" '
      [.[] | select(.event == "connected")] as $connected
      | ($connected | length) == 1
        and $connected[0].local == null and $connected[0].peer == $peer
    ' >/dev/null 2>&1; then
    fail "G6 monitor must emit exactly one connected event with local=null and peer=$RTU_IP:2404"
    return 1
  fi
  if ! eu_read_sessions 2404 10.10.10.50 ||
    ! eu_session_tuple "$SESSION_BODY" 10.10.10.50 "$RTU_IP" 2404; then
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
  kill_epoch=$(python3 -c 'import time; print(time.time_ns())') || kill_epoch=""
  if ! [[ "$kill_started_ms" =~ ^[0-9]+$ ]] || [ -z "$kill_epoch" ]; then
    fail "G6 could not timestamp conntrack kill"
    return 1
  fi
  eu_kill_tuple "$kali_tuple" && eu_monitor_finish 3 30 "$kill_started_ms" &&
    eu_monitor_no_asdu_after "$kill_epoch"
}

eu_gate_G7() {
  local before_g7 g7_policy g7_kill_started_ms
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
  eu_apply_custom "$g7_policy" "G7 improved minus CC allow" || return
  eu_check_nft_chain G7-custom || true
  if ! eu_read_sessions 2404 "$CC_IP" ||
    ! eu_session_tuple "$SESSION_BODY" "$CC_IP" "$RTU_IP" 2404; then
    fail "G7 session lookup did not yield exactly one CC-to-RTU tuple"
    return
  fi
  pass "G7 exact established CC tuple exists before kill"
  g7_kill_started_ms=$(eu_now_ms) || g7_kill_started_ms=""
  if ! [[ "$g7_kill_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G7 cannot establish monotonic timeout before conntrack kill"
  elif eu_kill_tuple "$SESSION_TUPLE" && eu_wait_for_operator \
    '.link.state != "started" and all(.points[]; .quality.iv and .quality.nt and .stale_reason == "link_lost")' \
    30 "G7 CC link loss with four link_lost points" "$g7_kill_started_ms"; then
    g7_loss_verified=1
    pass "G7 CC link is not started and all points are IV+NT link_lost"
  fi
}

eu_gate_G8() {
  local g8_before g8_epoch g8_points active g8_truth g8_operator current_points current_epoch g8_seq1 g8_seq2
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
  g8_points=$(eu_get_point_snapshot "$g8_before") || g8_points=""
  if [ -z "$g8_epoch" ] || [ -z "$g8_points" ] ||
    ! printf '%s' "$g8_before" | jq -e "$EU_JQ_DEFS four_points" >/dev/null 2>&1; then
    fail "G8 cache epoch, four-point shape, or point snapshot is empty/invalid"
  fi
  for active in true false true; do
    if [ "$active" = true ]; then
      eu_action_step process clear_fault "G8 clear before trip" || return
      eu_action_step breaker reset "G8 reclose before trip" || return
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
    current_points=$(eu_get_point_snapshot "$g8_operator") || current_points=""
    current_epoch=$(printf '%s' "$g8_operator" | jq -r '.cache_epoch // empty')
    if [ -n "$g8_points" ] && [ -n "$current_points" ] &&
      [ "$current_points" = "$g8_points" ] && [ "$current_epoch" = "$g8_epoch" ]; then
      pass "G8 operator values/received_at/source_time and cache_epoch unchanged with fault active=$active"
    else
      fail "G8 operator cache changed while CC path denied with fault active=$active"
    fi
    if printf '%s' "$g8_operator" | jq -e '.link.state != "started"' >/dev/null 2>&1; then
      pass "G8 operator-state route remains HTTP 200 while CC link is down"
    else
      fail "G8 operator route did not report the expected disconnected CC while fault active=$active"
    fi
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
  if [[ "$g8_seq1" =~ ^[0-9]+$ ]] && [[ "$g8_seq2" =~ ^[0-9]+$ ]] &&
    [ "$g8_seq2" -gt "$g8_seq1" ]; then
    pass "G8 solver solve_seq advances independently while operator cache remains stale"
  else
    fail "G8 solver solve_seq failed to advance ($g8_seq1 -> $g8_seq2)"
  fi
}

eu_gate_G9() {
  local g9_truth
  if ! eu_apply_named improved; then
    fail "G9 cannot restore improved policy"
    return
  fi
  if [ "$g7_loss_verified" -ne 1 ]; then
    fail "G9 cannot verify recovery without the verified G7 CC disconnect"
    return
  fi
  if eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and all_good' \
    30 "G9 CC reconnect/GI and four GOOD points"; then
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
      and $truth.devices["breaker-1"].position == "open" and $truth.devices["breaker-1"].protection_tripped == true
    ' "G9 restored operator state equals open/tripped physical state"
  else
    fail "G9 operator state unavailable after reconnect"
  fi
}

eu_gate_G10() {
  eu_reset_all || return
  eu_apply_named improved || return
  eu_validate_scenario PASS eu-validation-evidence
}
