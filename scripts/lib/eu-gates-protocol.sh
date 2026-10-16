# shellcheck shell=bash
# IEC104 timed quality and command/capture gates (G2-G3).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_gate_G2() {
  gps_paused=1
  if docker pause "$GPS" >/dev/null 2>"$TMP_DIR/pause-gps.err"; then
    pass "G2 GPS container paused"
  else
    fail "G2 could not pause GPS: $(cat "$TMP_DIR/pause-gps.err")"
    return 1
  fi
  sleep 6
  # The one-second GI can replace a timed event before the API is read. Prove
  # the actual event on the wire, not whichever cache update won that race.
  eu_window_start G2-invalid || return
  eu_wait_command open completed || return
  eu_window_finish G2-invalid || return
  eu_check_timed_status "$TMP_DIR/G2-invalid.pcap" 1 1 || return
  eu_unpause_owned "$GPS" gps "$gps_paused" || return
  eu_wait_for_field '.process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G2 solver after GPS resumes" || return
  sleep 2
  eu_window_start G2-synced || return
  eu_wait_command close completed || return
  eu_window_finish G2-synced || return
  eu_check_timed_status "$TMP_DIR/G2-synced.pcap" 2 0
}

eu_gate_G3() {
  local before_noop_revision after_noop_revision
  CAPTURE_PATH="/tmp/eu-iec104-smoke-$$.pcap"
  CAPTURE_PID_FILE="/tmp/eu-iec104-smoke-$$.tcpdump.pid"
  CAPTURE_LOG="/tmp/eu-iec104-smoke-$$.tcpdump.log"
  # shellcheck disable=SC2034
  capture_running=1
  if ! docker exec "$FW" sh -c "rm -f '$CAPTURE_PATH' '$CAPTURE_PID_FILE' '$CAPTURE_LOG'; \
    tcpdump --immediate-mode -i any -s 0 -U -w '$CAPTURE_PATH' 'tcp port 2404 or tcp port 502' \
      >'$CAPTURE_LOG' 2>&1 & echo \$! > '$CAPTURE_PID_FILE'"; then
    fail "G3 could not start tcpdump for both protocol hops"
  else
    CAPTURE_PID=$(docker exec "$FW" cat "$CAPTURE_PID_FILE" 2>/dev/null) || CAPTURE_PID=""
    if ! [[ "$CAPTURE_PID" =~ ^[0-9]+$ ]]; then
      fail "G3 tcpdump PID is missing or unparsable"
    else
      pass "G3 capture started for IEC104 and Modbus TCP hops"
    fi
  fi

  if eu_wait_command open completed; then
    eu_wait_for_field '.devices["breaker-1"].position == "open" and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G3 open truth/solver" || true
    eu_wait_for_operator 'point("feeder-breaker-position").value == "open"' 10 "G3 open operator feedback" || true
  fi
  if eu_wait_command close completed; then
    eu_wait_for_field '.devices["breaker-1"].position == "closed" and .process.buses.s1.live == true and .devices["breaker-1"].measurements.current_a > 0 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G3 close truth/solver" || true
    eu_wait_for_operator 'point("feeder-breaker-position").value == "closed"' 10 "G3 close operator feedback" || true
  fi
  if eu_wait_command open completed; then
    eu_wait_for_operator 'point("feeder-breaker-position").value == "open"' 10 "G3 second open feedback" || true
  fi
  if eu_get_field; then
    before_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.devices["breaker-1"].revision // empty')
  else
    before_noop_revision=""
    fail "G3 pre-no-op truth read unavailable"
  fi
  if eu_wait_command open completed; then
    eu_assert_jq "$COMMAND_BODY" '.result == "completed" and .feedback.ioa == 1001 and .feedback.value == "open" and .feedback.at != null' "G3 no-op open completed with matched COT 11 feedback"
  fi
  if eu_get_field; then
    after_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.devices["breaker-1"].revision // empty')
    if [ -n "$before_noop_revision" ] && [ "$after_noop_revision" = "$before_noop_revision" ]; then
      pass "G3 no-op command did not change device truth revision"
    else
      fail "G3 no-op changed or could not verify device revision ($before_noop_revision -> $after_noop_revision)"
    fi
  else
    fail "G3 post-no-op truth read unavailable"
  fi
  eu_wait_command close completed || true
  eu_action_step process fault_s1 "G3 sim local fault" || true
  eu_wait_for_field '.devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true' 10 "G3 local lockout" || true
  eu_wait_command close rejected || true
  eu_action_step process clear_fault "G3 device reset clear process fault" || true
  eu_action_step breaker-1 reset "G3 device reset" || true
  eu_stop_capture || true
  if [ -n "$CAPTURE_PID" ]; then
    if docker cp "$FW:$CAPTURE_PATH" "$TMP_DIR/eu-iec104-gate.pcap" >/dev/null 2>"$TMP_DIR/docker-cp.err"; then
      eu_decode_capture
    else
      fail "G3 could not retrieve firewall capture: $(cat "$TMP_DIR/docker-cp.err")"
    fi
  else
    fail "G3 capture PID is empty; cannot retrieve protocol evidence"
  fi

  # The decoded copy stays in TMP_DIR (evidence on failure).
  if docker exec "$FW" rm -f "$CAPTURE_PATH" "$CAPTURE_PID_FILE" "$CAPTURE_LOG" >/dev/null 2>&1; then
    CAPTURE_PATH=""
    CAPTURE_PID_FILE=""
    CAPTURE_LOG=""
  else
    fail "G3 temporary firewall capture files could not be removed"
  fi
}
