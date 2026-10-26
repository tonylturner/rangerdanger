# shellcheck shell=bash
# Independent Modbus, solver and RTU outage/recovery gates (G11-G14).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_gate_G11() {
  local g11_policy g11_tuple g11_loss_started_ms stale_ms g11_before_reset g11_stale g11_stale_points \
    g11_stale_epoch g11_truth g11_after_reset g11_after_reset_points reset_truth poll_since poll_until
  g11_loss_verified=0
  # Earlier gates may have left a restrictive policy or tripped device behind.
  eu_apply_named improved || return
  eu_reset_all || return
  eu_wait_for_baseline "G11 GOOD baseline before Modbus loss" 30 || return
  g11_policy="$TMP_DIR/improved-without-rtu-modbus.json"
  eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$RTU_IP" "$BREAKER_IP" 502 "$g11_policy" || return
  if eu_policy_has_no_ephemeral_allow "$g11_policy"; then
    pass "G11 custom policy has no ICS keys or ephemeral destination-port allow"
  else
    fail "G11 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
  fi
  eu_commit_manual_policy "$g11_policy" "G11 improved minus RTU-to-breaker allow" || return
  eu_check_nft_chain G11-custom || true
  eu_get_operator || fail "G11 cannot snapshot operator points before Modbus loss"
  if ! eu_read_sessions 502 "$RTU_IP" \
    || ! eu_session_tuple "$SESSION_BODY" "$RTU_IP" "$BREAKER_IP" 502; then
    fail "G11 session lookup did not yield exactly one RTU-to-breaker tuple"
    return
  fi
  g11_tuple="$SESSION_TUPLE"
  eu_window_start G11 || return
  poll_since=$(eu_epoch_seconds) || return
  pass "G11 exact RTU-to-breaker Modbus session exists before kill"
  g11_loss_started_ms=$(eu_now_ms) || g11_loss_started_ms=""
  if ! [[ "$g11_loss_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G11 cannot establish monotonic timeout before conntrack kill"
  elif eu_kill_tuple "$g11_tuple"; then
    stale_ms=$(jq -r '.devices[] | select(.id == "feeder-breaker") | .stale_after_ms' \
      "$PACKAGE/protocols/points.json" 2>/dev/null) || stale_ms=""
    if ! [[ "$stale_ms" =~ ^[0-9]+$ ]] || [ "$stale_ms" -ne 3000 ]; then
      fail "G11 points.json stale_after_ms missing/invalid or differs from 3000: $stale_ms"
    elif eu_wait_for_operator '.link.state == "started" and all_stale("device_unreachable") and f2_good' \
      5 "G11 RTU device_unreachable within stale_after_ms + 2s" "$g11_loss_started_ms"; then
      g11_loss_verified=1
      pass "G11 four points IV+NT device_unreachable with IEC104 link started"
    fi
  fi
  poll_until=$(eu_epoch_seconds) || return
  eu_window_finish G11 || return
  eu_peer_poll_gaps "$TMP_DIR/G11.pcap" "$poll_since" "$poll_until" || true
  g11_stale_points=""
  g11_stale_epoch=""
  if eu_get_operator; then
    g11_stale="$API_BODY"
    g11_stale_points=$(eu_get_point_snapshot "$g11_stale" | jq -c 'map({id,value})') || g11_stale_points=""
    g11_stale_epoch=$(printf '%s' "$g11_stale" | jq -r '.cache_epoch // empty')
    if [ -z "$g11_stale_epoch" ] || [ -z "$g11_stale_points" ] \
      || ! printf '%s' "$g11_stale" | jq -e "$EU_JQ_DEFS four_points" >/dev/null 2>&1; then
      fail "G11 stale operator cache lacks its epoch or four expected points"
    fi
  else
    fail "G11 operator state unavailable after stale transition"
  fi
  g11_before_reset=""
  if eu_get_field; then
    g11_before_reset="$API_BODY"
  else
    fail "G11 physical state unavailable before reset"
  fi
  eu_action_step process clear_fault "G11 device reset changes truth clear process fault" || true
  eu_action_step breaker-1 reset "G11 device reset changes truth" || true
  if eu_get_field; then
    g11_truth="$API_BODY"
    if eu_get_operator; then
      g11_after_reset="$API_BODY"
      g11_after_reset_points=$(eu_get_point_snapshot "$g11_after_reset" | jq -c 'map({id,value})') || g11_after_reset_points=""
      if [ -n "$g11_stale_points" ] && [ -n "$g11_after_reset_points" ] \
        && [ "$g11_after_reset_points" = "$g11_stale_points" ] \
        && [ "$(printf '%s' "$g11_after_reset" | jq -r '.cache_epoch // empty')" = "$g11_stale_epoch" ]; then
        if printf '%s' "$g11_after_reset" | jq -e "$EU_JQ_DEFS all_stale(\"device_unreachable\")" >/dev/null; then
          pass "G11 reset truth cannot change stale cached values/epoch (received_at may advance on real stale GI)"
        else fail "G11 reset unexpectedly made dropped breaker points good"; fi
      else
        fail "G11 operator cache changed during out-of-band device reset"
      fi
      # shellcheck disable=SC2016
      eu_assert_jq_truth "$g11_truth" "${g11_before_reset:-null}" \
        '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false and .devices["breaker-1"].revision != $truth.devices["breaker-1"].revision' \
        "G11 device reset changed breaker truth"
    else
      fail "G11 operator state unavailable after device reset"
    fi
  else
    fail "G11 physical state unavailable after reset"
  fi
  eu_apply_named improved || return
  if [ "$g11_loss_verified" -ne 1 ]; then
    fail "G11 Modbus recovery not tested without the verified device_unreachable transition"
  elif eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and station_good and f2_good' \
    30 "G11 Modbus recovery after improved restore"; then
    g11_loss_verified=2
    pass "G11 four observations GOOD after restored Modbus conduit"
  fi
  if ! eu_get_field; then
    fail "G11 field state read after restore failed"
    return
  fi
  reset_truth="$API_BODY"
  if eu_get_operator; then
    # shellcheck disable=SC2016
    eu_assert_jq_truth "$API_BODY" "$reset_truth" '
      point("feeder-breaker-position").value == $truth.devices["breaker-1"].position
      and point("feeder-protection-trip").value == $truth.devices["breaker-1"].protection_tripped
    ' "G11 recovered operator values equal reset truth"
  else
    fail "G11 operator read after restore failed"
  fi
}

eu_gate_G12() {
  local g12_pause_started_ms g12_measurement_loss_seen=0
  # This outage needs a healthy chain, not a successful G11 outage/recovery.
  eu_apply_named improved || return
  eu_reset_all || return
  eu_wait_for_baseline "G12 GOOD baseline before measurement loss" 30 || return
  g12_pause_started_ms=$(eu_now_ms) || g12_pause_started_ms=""
  if ! [[ "$g12_pause_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G12 cannot establish monotonic timeout before pausing OpenDSS"
    return
  fi
  opendss_paused=1
  if ! docker pause "$OPENDSS" >/dev/null 2>"$TMP_DIR/pause-opendss.err"; then
    fail "G12 could not pause OpenDSS: $(cat "$TMP_DIR/pause-opendss.err")"
    eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || true
    return
  fi
  pass "G12 OpenDSS container paused"
  if eu_wait_for_operator '
    all(point("feeder-current"), point("feeder-voltage");
      .quality.iv == true and .quality.nt == false and .stale_reason == "measurement_invalid")
    and all(point("feeder-breaker-position"), point("feeder-protection-trip"); good)
  ' 5 "G12 measurement-invalid quality within five seconds" "$g12_pause_started_ms"; then
    g12_measurement_loss_seen=1
    pass "G12 current/voltage IV measurement_invalid; status points remain GOOD"
  fi
  eu_command rmu-1-command open rejected no_measurement || true
  eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || return
  if [ "$g12_measurement_loss_seen" -ne 1 ]; then
    fail "G12 measurement recovery not tested without the measurement_invalid transition"
  elif eu_wait_for_operator 'all(.points[]; good)' 10 "G12 measurement recovery"; then
    pass "G12 all four points GOOD after OpenDSS resumes"
  fi
}

eu_gate_G13() {
  local connection_seq generation baseline cursor revisions revision audit loss_budget node step field evidence target expected final
  local nodes='["breaker-1","f2-breaker","rmu-1","rmu-2","oltc-1"]' solved
  # Name every physical device explicitly: a missing device cannot pass all().
  # shellcheck disable=SC2016 # jq variables, not shell expansions
  solved=' .process.truth_stale == false and (. as $field | all('
  solved+="$nodes"
  # shellcheck disable=SC2016
  solved+='[]; . as $node | $field.devices[$node] as $device
    | $device.device_id == (if $node == "breaker-1" then "feeder-breaker" else $node end)
    and ($device.generation | type == "number") and ($device.revision | type == "number")
    and $device.moving != true and $device.measurements.valid == true
    and $field.process.devices[$device.device_id].delivered == true
    and $field.process.devices[$device.device_id].solved_revision == $device.revision))'
  loss_budget=$(jq -er '.timers | .t3_s + .t1_s + 10' "$PACKAGE/protocols/points.json") || return
  eu_apply_named improved || return
  eu_reset_all || return
  if ! eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and station_good and f2_good' \
    30 "G13 CC baseline before the RTU outage"; then
    return
  fi
  connection_seq=$(printf '%s' "$WAIT_BODY" | jq -er '.link.connection_seq | numbers') || {
    fail "G13 CC connection_seq is missing or not a number: $WAIT_BODY"
    return
  }
  eu_wait_for_field "$solved and .devices[\"breaker-1\"].position == \"closed\"
    and .devices[\"breaker-1\"].protection_tripped == false" 10 "G13 solver agrees with all five devices" || return
  field="$WAIT_BODY"
  # Independent generations, revisions and cursors; sequence numbers are local
  # to a device's audit ring and must never be shared across devices.
  evidence='{}'
  for node in breaker-1 f2-breaker rmu-1 rmu-2 oltc-1; do
    generation=$(printf '%s' "$field" | jq -er --arg node "$node" '.devices[$node].generation | numbers') || return
    baseline=$(printf '%s' "$field" | jq -er --arg node "$node" '.devices[$node].revision | numbers') || return
    if ! eu_api_get "/api/field/audit?device=$node&after=0"; then
      fail "G13 $node audit cursor unavailable or unparsable"
      return
    fi
    # A device may advance between the solved snapshot and this audit read.
    # Keep those transitions: start at the last entry for the baseline itself,
    # not at a newer latest_seq that would silently discard them.
    cursor=$(printf '%s' "$API_BODY" | jq -er --argjson generation "$generation" --argjson baseline "$baseline" '
      select(.generation == $generation)
      | [.entries[] | select(.after.revision == $baseline)] | last.seq | numbers') || {
      fail "G13 $node audit has no cursor for solved baseline revision $baseline: $API_BODY"
      return
    }
    evidence=$(printf '%s' "$evidence" | jq -c --arg node "$node" --argjson generation "$generation" \
      --argjson baseline "$baseline" --argjson cursor "$cursor" \
      '.[$node] = {generation:$generation, baseline:$baseline, cursor:$cursor, revisions:[]}') || return
  done
  rtu_paused=1
  if ! docker pause "$RTU" >/dev/null 2>"$TMP_DIR/pause-rtu.err"; then
    fail "G13 could not pause RTU: $(cat "$TMP_DIR/pause-rtu.err")"
    eu_unpause_owned "$RTU" rtu "$rtu_paused" || true
    return
  fi
  pass "G13 RTU container paused"
  # Retain the physical fault/trip/reset sequence. Then exercise peers that
  # need not change during an S1 fault. OLTC block/unblock avoids resetting
  # its PLC arming handshake and restores the pre-outage control state.
  for step in fault clear rmu-1 rmu-2 f2-breaker block unblock; do
    target="$step"
    expected="$solved"
    case "$step" in
      fault)
        target=breaker-1
        eu_action_step process fault_s1 "G13 fault while RTU paused" || break
        eu_wait_for_field '.devices["breaker-1"].protection_tripped == true' \
          10 "G13 physical protection trips during the fault" || break ;;
      clear)
        target=breaker-1
        eu_action_step process clear_fault "G13 clear process fault" || break
        eu_action_step breaker-1 reset "G13 breaker reset while RTU paused" || break
        expected+=' and .devices["breaker-1"].position == "closed"
          and .devices["breaker-1"].protection_tripped == false' ;;
      block|unblock)
        target=oltc-1
        eu_action oltc-1 "$step" || break ;;
      *) eu_action "$step" reset || break ;;
    esac
    # Do not supersede any observed revision before the solver has delivered
    # it. Collect changes from the whole station, including local protection.
    eu_wait_for_field "$expected" 10 "G13 all devices solved after $step while RTU paused" || break
    field="$WAIT_BODY"
    for node in breaker-1 f2-breaker rmu-1 rmu-2 oltc-1; do
      generation=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].generation')
      baseline=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].revisions[-1] // .[$node].baseline')
      cursor=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].cursor')
      revision=$(printf '%s' "$field" | jq -er --arg node "$node" --argjson generation "$generation" \
        --argjson baseline "$baseline" '.devices[$node] | select(.generation == $generation)
          | .revision | numbers | select(. >= $baseline)') || {
        fail "G13 $node generation changed or revision regressed after $step"
        break 2
      }
      if [ "$revision" = "$baseline" ]; then
        if [ "$node" = "$target" ]; then
          fail "G13 $node $step did not advance its revision"
          break 2
        fi
        continue
      fi
      evidence=$(printf '%s' "$evidence" | jq -c --arg node "$node" --argjson revision "$revision" \
        '.[$node].revisions += [$revision]') || break 2
      eu_wait_for_state "/api/field/audit?device=$node&after=$cursor" \
        ".generation == $generation and any(.entries[]; .operation == \"first_solve\" and .detail.revision == $revision)" \
        10 "G13 $node solver first_solve of revision $revision while RTU paused" || break 2
    done
  done
  # Recovery must be a new connection after the CC drops the paused RTU.
  if eu_wait_for_operator '.link.state != "started"' "$loss_budget" "G13 CC link loss while RTU paused"; then
    pass "G13 CC lost the IEC104 link to the paused RTU"
  fi
  eu_unpause_owned "$RTU" rtu "$rtu_paused" || return
  if eu_wait_for_operator ".link.state == \"started\" and .link.connection_seq > $connection_seq
    and .link.gi.state == \"complete\" and station_good and f2_good" 30 "G13 CC recovery by a new connection and GI"; then
    pass "G13 CC recovered all station observations through a new IEC104 connection and completed GI"
  fi
  # The PLC/actuators keep running during RTU loss and recovery. Freeze an
  # independent solved field cut, then require every revision through it,
  # including transitions between observations or after the last outage step.
  eu_wait_for_field "$solved" 10 "G13 final solver agrees with all five devices" || return
  field="$WAIT_BODY"
  for node in breaker-1 f2-breaker rmu-1 rmu-2 oltc-1; do
    generation=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].generation')
    baseline=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].baseline')
    cursor=$(printf '%s' "$evidence" | jq -r --arg node "$node" '.[$node].cursor')
    revisions=$(printf '%s' "$evidence" | jq -c --arg node "$node" '.[$node].revisions')
    final=$(printf '%s' "$field" | jq -er --arg node "$node" --argjson generation "$generation" \
      --argjson last "$(printf '%s' "$revisions" | jq -c 'max // 0')" '
      .devices[$node] | select(.generation == $generation)
      | .revision | numbers | select(. >= $last)') || {
      fail "G13 $node final generation changed or revision regressed: $field"
      continue
    }
    if ! eu_api_get "/api/field/audit?device=$node&after=$cursor"; then
      fail "G13 $node post-outage audit response is unavailable or unparsable"
      continue
    fi
    audit="$API_BODY"
    if [ "$revisions" = "[]" ]; then fail "G13 $node recorded no device revision during the outage"; fi
    eu_check_g13_audit "$audit" "$generation" "$baseline" "$cursor" "$final" "$revisions" "$node" || true
  done
}

eu_gate_G14() {
  eu_reset_all || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 15 "G14 reset truth and process recovery" || true
  if eu_wait_for_operator '
    .link.state == "started" and .link.gi.state == "complete" and station_good and f2_good
    and point("feeder-breaker-position").value == "closed"
  ' 30 "G14 CC observes reset through IEC104"; then
    pass "G14 reset state recovered through RTU/IEC104/CC chain"
  fi
  if eu_apply_named weak; then
    pass "G14 applied weak policy"
  else
    fail "G14 could not leave range on weak policy"
  fi
}
