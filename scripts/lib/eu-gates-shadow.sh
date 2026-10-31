# shellcheck shell=bash
# G29 is observational evidence, separate from the L4 prevention/continuity gates.

eu_shadow_get() {
  local path="$1" file="$2" response code
  response=$(docker exec "$FW" curl -sS --max-time 10 -w '\n%{http_code}' \
    -H "Authorization: Bearer $FW_TOKEN" "http://127.0.0.1:8080/api/v1$path") || {
    fail "shadow evidence GET $path transport failed"
    return 1
  }
  code="${response##*$'\n'}"
  printf '%s\n' "${response%$'\n'*}" >"$file" || return 1
  if [ "$code" != 200 ] || ! jq -e 'type == "object"' "$file" >/dev/null; then
    fail "shadow evidence GET $path returned invalid HTTP $code response"
    return 1
  fi
}

eu_shadow_check() {
  python3 "$SCRIPT_DIR/lib/eu-shadow-check.py" "$@" || {
    fail "G29 invalid $1 shadow evidence"
    return 1
  }
}

eu_shadow_health() {
  eu_shadow_get /health "$TMP_DIR/G29-health.json" || return
  eu_firewall_config >"$TMP_DIR/G29-applied.json" || return
  # Healthy inactive IEC enforcement is expected: these are monitor-only rules.
  jq -e '.status == "ok" and .enforcement.state == "enforcing"
    and .enforcement.reason == "" and .iec104Enforcement.healthy == true
    and .iec104Enforcement.reason == "" and .iec104Enforcement.active == false' \
    "$TMP_DIR/G29-health.json" >/dev/null \
    && jq -e '.dataplane.dpiEnabled == true
      and .dataplane.nfqueueGroup == 101
      and .dataplane.dpiIcsProtocols.iec104 != false' \
      "$TMP_DIR/G29-applied.json" >/dev/null || {
    fail "G29 DPI/decoder/queue or applied-engine health is not ready"
    return 1
  }
  # Verify actual installed predicates, not just the presence of candidate IDs.
  eu_normalize_firewall_rules "$PACKAGE/firewall/improved.json" >"$TMP_DIR/G29-expected-rules.json" \
    && eu_normalize_firewall_rules "$TMP_DIR/G29-applied.json" >"$TMP_DIR/G29-applied-rules.json" \
    && cmp -s "$TMP_DIR/G29-expected-rules.json" "$TMP_DIR/G29-applied-rules.json" || {
    fail "G29 applied rules differ from Improved"
    return 1
  }
}

eu_shadow_events() {
  local rule="$1" start="$2" end="$3" phase="$4" query cursor="" next page
  query=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.urlencode({
    "kind":"ics.decision","mode":"monitor","ruleId":sys.argv[1],
    "since":sys.argv[2],"limit":5000}))' "$rule" "$start") || return
  for ((page = 0; page < 10; page++)); do
    eu_shadow_get "/events?$query${cursor:+&cursor=$cursor}" \
      "$TMP_DIR/G29-$phase-$rule-events-$page.json" || return
    next=$(eu_shadow_check events "$TMP_DIR/G29-$phase-$rule-events-$page.json" \
      "$rule" "$start" "$end") || return
    [ -n "$next" ] || return 0
    cursor=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' \
      "$next") || return
  done
  fail "G29 monitor event pagination exceeded ten pages"
  return 1
}

eu_shadow_phase() {
  local phase="$1" previous="$2" point="$3" operation="$4" feedback="$5" start end
  start=$(eu_utc) || return
  # The operator route uses the same CC IEC104 session as the student route.
  # Device-scoped readiness permits restoration while feeder-pf is invalid.
  eu_command "$point" "$operation" completed || return
  eu_wait_for_field "$feedback" 15 "G29 $phase independent field result" || return
  # Let the completed request's asynchronous telemetry publish before querying.
  sleep 1
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/G29-$phase.json" || return
  eu_shadow_check delta "$previous" "$TMP_DIR/G29-$phase.json" "$phase" || return
  end=$(eu_utc) || return
  printf '%s\n%s\n' "$start" "$end" >"$TMP_DIR/G29-$phase-window.txt" || return
  if [ "$phase" = breaker ]; then
    eu_shadow_events observe-cc-breaker-command "$start" "$end" "$phase" || return
  else
    eu_shadow_events observe-cc-oltc-step "$start" "$end" "$phase" || return
    if [ "$phase" = raise ]; then
      eu_shadow_events observe-cc-oltc-raise-only "$start" "$end" "$phase" || return
    fi
  fi
  pass "G29 $phase monitor deltas and windowed events agree ($start .. $end); command still operates the field"
}

eu_shadow_commands() {
  local initial="$1"
  eu_shadow_phase breaker "$TMP_DIR/G29-baseline.json" feeder-breaker-command open \
    '.devices["breaker-1"].position == "open" and .process.buses.s1.live == false' || return
  eu_command feeder-breaker-command close completed || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .process.buses.s1.live == true' \
    15 "G29 restore CB1" || return
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/G29-before-lower.json" || return
  # Even the restore command may not cross a policy-install/restart boundary.
  eu_shadow_check delta "$TMP_DIR/G29-baseline.json" "$TMP_DIR/G29-before-lower.json" breaker || return
  eu_shadow_phase lower "$TMP_DIR/G29-before-lower.json" oltc-1-step lower \
    ".devices[\"oltc-1\"].tap == $((initial - 1)) and .devices[\"oltc-1\"].moving == false" || return
  eu_shadow_phase raise "$TMP_DIR/G29-lower.json" oltc-1-step raise \
    ".devices[\"oltc-1\"].tap == $initial and .devices[\"oltc-1\"].moving == false" || return
  # Close the proof window after event pagination too, not just before it.
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/G29-final.json" || return
  eu_shadow_check delta "$TMP_DIR/G29-baseline.json" "$TMP_DIR/G29-final.json" breaker
}

eu_gate_G29() {
  local initial result=0 prepared=0
  initial=$(jq -er '.initial_tap | numbers' "$PACKAGE/process/reference-cases.json") || return
  if eu_set_profile false && eu_reset_all && eu_apply_named improved \
    && eu_lab_reconnect_cc; then
    prepared=1
    if ! eu_reference_tap "$initial" || ! eu_shadow_health \
      || ! eu_shadow_get /firewall/shadow-counters "$TMP_DIR/G29-baseline.json" \
      || ! eu_shadow_check baseline "$TMP_DIR/G29-baseline.json" \
      || ! eu_shadow_commands "$initial"; then
      result=1
    fi
  else
    result=1
  fi
  # Restore the normalized reset pre-state even after a failed evidence assertion.
  if [ "$prepared" -eq 1 ]; then
    if eu_get_field; then
      if printf '%s' "$API_BODY" | jq -e '.devices["breaker-1"].position == "open"' >/dev/null; then
        eu_command feeder-breaker-command close completed || result=1
      fi
    else
      result=1
    fi
    eu_reference_tap "$initial" || result=1
    eu_wait_for_field ".devices[\"oltc-1\"].tap == $initial and .devices[\"oltc-1\"].moving == false" \
      15 "G29 restored reference tap" || result=1
    eu_command oltc-1-mode auto completed || result=1
    eu_wait_for_field '.devices["breaker-1"].position == "closed" and .devices["oltc-1"].auto == true' \
      15 "G29 restored breaker and mode" || result=1
  fi
  eu_set_profile true || result=1
  eu_apply_named weak || result=1
  return "$result"
}
