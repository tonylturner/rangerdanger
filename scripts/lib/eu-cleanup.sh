# shellcheck shell=bash
# Owned-service teardown, evidence retention and EXIT restoration.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_unpause_owned() {
  local container="$1" flag_name="$2" was_paused="$3" paused
  [ "$was_paused" -eq 1 ] || return 0
  paused=$(docker inspect -f '{{.State.Paused}}' "$container" 2>/dev/null) || paused=""
  if [ "$paused" = "false" ]; then
    case "$flag_name" in
      gps) gps_paused=0 ;;
      opendss) opendss_paused=0 ;;
      rtu) rtu_paused=0 ;;
      cc) cc_paused=0 ;;
    esac
    return 0
  fi
  if [ "$paused" != "true" ]; then
    fail "could not determine paused state for owned container $container"
    return 1
  fi
  if ! docker unpause "$container" >/dev/null 2>"$TMP_DIR/unpause.err"; then
    fail "could not unpause $container: $(cat "$TMP_DIR/unpause.err")"
    return 1
  fi
  paused=$(docker inspect -f '{{.State.Paused}}' "$container" 2>/dev/null) || paused=""
  if [ "$paused" != "false" ]; then
    fail "$container unpause did not read back as unpaused"
    return 1
  fi
  case "$flag_name" in
    gps) gps_paused=0 ;;
    opendss) opendss_paused=0 ;;
    rtu) rtu_paused=0 ;;
    cc) cc_paused=0 ;;
  esac
  pass "$container unpaused"
}
# eu_save_evidence: before cleanup deletes them, copy every API response and
# session row (the response log), the decoded captures, the Kali monitor files
# and the firewall capture to EU_GATE_EVIDENCE_DIR.
eu_save_evidence() {
  local dir="${EU_GATE_EVIDENCE_DIR:-$ROOT/build/deploy-test/eu-evidence/$(date -u +%Y%m%dT%H%M%SZ)}" file
  if ! mkdir -p "$dir" || ! cp -R "$TMP_DIR/." "$dir/"; then
    fail "could not keep gate evidence in $dir"
    return 1
  fi
  if [ -n "$MONITOR_LOG" ]; then
    for file in "$MONITOR_LOG" "$MONITOR_ERR" "$MONITOR_RC" "$MONITOR_PID"; do
      docker cp "$KALI:$file" "$dir/kali-${file##*/}" >/dev/null 2>&1 \
        || printf 'evidence: Kali %s was not available\n' "$file" >&2
    done
  fi
  if [ -n "$CAPTURE_PATH" ]; then
    docker cp "${CAPTURE_HOST:-$FW}:$CAPTURE_PATH" "$dir/firewall-${CAPTURE_PATH##*/}" >/dev/null 2>&1 \
      || printf 'evidence: firewall capture %s was not available\n' "$CAPTURE_PATH" >&2
  fi
  printf 'Gate evidence kept in %s\n' "$dir" >&2
}

# shellcheck disable=SC2329
eu_cleanup() {
  local exit_status="$1" weak_reset=0
  [ "$cleanup_running" -eq 0 ] || return
  cleanup_running=1
  trap - EXIT
  eu_stop_capture || true
  eu_stop_monitor || true
  eu_stop_plc_scan || true
  eu_unpause_owned "$GPS" gps "$gps_paused" || true
  eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || true
  eu_unpause_owned "$RTU" rtu "$rtu_paused" || true
  eu_unpause_owned "$CC" cc "$cc_paused" || true
  # shellcheck disable=SC2154
  if [ "$failures" -gt 0 ] || [ "$exit_status" -ne 0 ]; then
    eu_save_evidence || true
  fi
  if [ "$MONITOR_STOP_FAILED" -eq 1 ]; then
    printf 'cleanup kept the Kali monitor files %s* for diagnosis\n' "${MONITOR_LOG%.jsonl}" >&2
  elif [ -n "$MONITOR_LOG" ] && ! docker exec "$KALI" rm -f \
    "$MONITOR_LOG" "$MONITOR_ERR" "$MONITOR_RC" "$MONITOR_PID" \
    >/dev/null 2>"$TMP_DIR/monitor-rm.err"; then
    fail "cleanup could not remove Kali monitor logs: $(cat "$TMP_DIR/monitor-rm.err")"
  fi
  if [ -n "$CAPTURE_HOST" ]; then
    docker rm "$CAPTURE_HOST" >/dev/null || fail "cleanup owned capture container $CAPTURE_HOST"
    CAPTURE_PATH="" CAPTURE_HOST=""
  fi
  if [ -n "$CAPTURE_PATH" ] && ! docker exec "$FW" rm -f \
    "$CAPTURE_PATH" "$CAPTURE_PID_FILE" "$CAPTURE_LOG" \
    >/dev/null 2>"$TMP_DIR/capture-rm.err"; then
    fail "cleanup could not remove firewall capture files: $(cat "$TMP_DIR/capture-rm.err")"
  fi
  # shellcheck disable=SC2154 # ownership is set by policy/device helpers
  if [ "$state_touched" -eq 1 ]; then
    if ! eu_apply_named weak; then
      fail "cleanup could not restore weak policy"
      weak_reset=1
    fi
    if [ "${profile_disabled:-0}" -eq 1 ]; then
      eu_set_profile true || weak_reset=1
    fi
    eu_reset_all || weak_reset=1
    if [ "$weak_reset" -eq 0 ] && eu_get_field; then
      if printf '%s' "$API_BODY" | jq -e \
        '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].lockout == false and .process.fault_section == "none" and .devices["breaker-1"].protection_tripped == false' \
        >/dev/null 2>&1; then
        pass "cleanup device reset readback is the initial breaker truth"
      else
        fail "cleanup device reset did not restore initial truth: $API_BODY"
        weak_reset=1
      fi
    elif [ "$weak_reset" -eq 0 ]; then
      fail "cleanup could not read back device reset truth"
      weak_reset=1
    fi
    if [ "$weak_reset" -eq 0 ]; then
      pass "cleanup ended on weak policy + device reset"
    fi
  fi
  if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR" || fail "could not remove gate temporary directory $TMP_DIR"
  fi
  # shellcheck disable=SC2154
  if [ "$failures" -gt 0 ] || [ "$exit_status" -ne 0 ]; then
    # shellcheck disable=SC2154
    printf '\nEU IEC104 gate FAIL (%d PASS, %d FAIL)\n' "$passes" "$failures" >&2
    exit 1
  fi
  printf '\nEU IEC104 gate PASS (%d PASS, 0 FAIL)\n' "$passes"
  exit 0
}

# shellcheck disable=SC2329
eu_on_exit() {
  local result="$?"
  eu_cleanup "$result"
}
# A failed gate must not leave its owned injector/capture affecting later gates.
# shellcheck disable=SC2154 # ownership flags are initialized by the gate runner
eu_gate_resources_finish() {
  if [ "${profile_disabled:-0}" -eq 1 ]; then
    eu_set_profile true || fail "$1 profile restoration failed"
  fi
  if [ -n "$PLC_SCAN_HELPER" ]; then
    eu_stop_plc_scan || fail "$1 owned PLC tracer teardown failed"
  fi
  if [ -n "$CAPTURE_HOST" ]; then
    eu_window_finish "$1-aborted" || fail "$1 owned capture teardown failed"
  fi
  eu_unpause_owned "$GPS" gps "$gps_paused" || fail "$1 GPS teardown failed"
  eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || fail "$1 solver teardown failed"
  eu_unpause_owned "$RTU" rtu "$rtu_paused" || fail "$1 RTU teardown failed"
  eu_unpause_owned "$CC" cc "$cc_paused" || fail "$1 CC teardown failed"
}
