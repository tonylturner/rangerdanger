# shellcheck shell=bash
# G29 is observational evidence, separate from the L4 prevention/continuity gates.

eu_shadow_get() {
  local path="$1" file="$2" response code
  response=$(docker_bounded 15 exec "$FW" curl -sS --max-time 10 -w '\n%{http_code}' \
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

eu_shadow_wait_health() {
  python3 "$SCRIPT_DIR/lib/containd_readiness.py" --container "$FW" --token "$FW_TOKEN" --health
}

eu_shadow_health() {
  local label="${1:-G29}" expected="${2:-$PACKAGE/firewall/improved.json}"
  eu_shadow_wait_health || return
  eu_shadow_get /health "$TMP_DIR/$label-health.json" || return
  eu_firewall_config >"$TMP_DIR/$label-applied.json" || return
  # Healthy inactive IEC enforcement is expected: these are monitor-only rules.
  jq -e '.status == "ok" and .enforcement.state == "enforcing"
    and .enforcement.reason == "" and .iec104Enforcement.healthy == true
    and .iec104Enforcement.reason == "" and .iec104Enforcement.active == false' \
    "$TMP_DIR/$label-health.json" >/dev/null \
    && jq -e '.dataplane.dpiEnabled == true
      and .dataplane.nfqueueGroup == 101
      and .dataplane.dpiIcsProtocols.iec104 != false' \
      "$TMP_DIR/$label-applied.json" >/dev/null || {
    fail "$label DPI/decoder/queue or applied-engine health is not ready"
    return 1
  }
  # Verify actual installed predicates, not just the presence of candidate IDs.
  eu_normalize_firewall_rules "$expected" >"$TMP_DIR/$label-expected-rules.json" \
    && eu_normalize_firewall_rules "$TMP_DIR/$label-applied.json" >"$TMP_DIR/$label-applied-rules.json" \
    && cmp -s "$TMP_DIR/$label-expected-rules.json" "$TMP_DIR/$label-applied-rules.json" || {
    fail "$label applied rules differ from expected policy"
    return 1
  }
}

eu_shadow_events() {
  local rule="$1" start="$2" end="$3" phase="$4" prefix="${5:-G29}" query cursor="" next page
  query=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.urlencode({
    "kind":"ics.decision","mode":"monitor","ruleId":sys.argv[1],
    "since":sys.argv[2],"limit":5000}))' "$rule" "$start") || return
  for ((page = 0; page < 10; page++)); do
    eu_shadow_get "/events?$query${cursor:+&cursor=$cursor}" \
      "$TMP_DIR/$prefix-$phase-$rule-events-$page.json" || return
    next=$(eu_shadow_check events "$TMP_DIR/$prefix-$phase-$rule-events-$page.json" \
      "$rule" "$start" "$end") || return
    [ -n "$next" ] || return 0
    cursor=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' \
      "$next") || return
  done
  fail "G29 monitor event pagination exceeded ten pages"
  return 1
}

eu_shadow_phase() {
  local phase="$1" previous="$2" point="$3" operation="$4" feedback="$5"
  local prefix="${6:-G29}" start end
  start=$(eu_utc) || return
  # The operator route uses the same CC IEC104 session as the student route.
  # Device-scoped readiness permits restoration while feeder-pf is invalid.
  eu_command "$point" "$operation" completed || return
  eu_wait_for_field "$feedback" 15 "$prefix $phase independent field result" || return
  # Let the completed request's asynchronous telemetry publish before querying.
  sleep 1
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/$prefix-$phase.json" || return
  eu_shadow_check delta "$previous" "$TMP_DIR/$prefix-$phase.json" "$phase" || return
  end=$(eu_utc) || return
  printf '%s\n%s\n' "$start" "$end" >"$TMP_DIR/$prefix-$phase-window.txt" || return
  if [ "$phase" = breaker ]; then
    eu_shadow_events observe-cc-breaker-command "$start" "$end" "$phase" "$prefix" || return
  else
    eu_shadow_events observe-cc-oltc-step "$start" "$end" "$phase" "$prefix" || return
    if [ "$phase" = raise ]; then
      eu_shadow_events observe-cc-oltc-raise-only "$start" "$end" "$phase" "$prefix" || return
    fi
  fi
  pass "$prefix $phase monitor deltas and windowed events agree ($start .. $end); command still operates the field"
}

eu_shadow_commands() {
  local initial="$1" prefix="${2:-G29}"
  eu_shadow_phase breaker "$TMP_DIR/$prefix-baseline.json" feeder-breaker-command open \
    '.devices["breaker-1"].position == "open" and .process.buses.s1.live == false' "$prefix" || return
  eu_command feeder-breaker-command close completed || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .process.buses.s1.live == true' \
    15 "$prefix restore CB1" || return
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/$prefix-before-lower.json" || return
  # Even the restore command may not cross a policy-install/restart boundary.
  eu_shadow_check delta "$TMP_DIR/$prefix-baseline.json" "$TMP_DIR/$prefix-before-lower.json" breaker || return
  eu_shadow_phase lower "$TMP_DIR/$prefix-before-lower.json" oltc-1-step lower \
    ".devices[\"oltc-1\"].tap == $((initial - 1)) and .devices[\"oltc-1\"].moving == false" "$prefix" || return
  eu_shadow_phase raise "$TMP_DIR/$prefix-lower.json" oltc-1-step raise \
    ".devices[\"oltc-1\"].tap == $initial and .devices[\"oltc-1\"].moving == false" "$prefix" || return
  # Close the proof window after event pagination too, not just before it.
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/$prefix-final.json" || return
  eu_shadow_check delta "$TMP_DIR/$prefix-baseline.json" "$TMP_DIR/$prefix-final.json" breaker
}

eu_shadow_plan_case() {
  local initial="$1" golden="$ROOT/frontend/lib/testdata/plans/eu-iec104-dpi.json"
  local oracle="$ROOT/scripts/tests/fixtures" request expected
  request=$(jq -ce -L "$oracle" 'include "plan-gate"; plan_request' "$golden") || return
  printf '%s\n' "$request" >"$TMP_DIR/G29-plan-request.json" || return
  # shellcheck disable=SC2034 # consumed by eu-cleanup.sh
  state_touched=1
  eu_api_post /api/firewall/apply-plan "$request" 60 || return
  [ "$HTTP_STATUS" = 200 ] || {
    fail "G29 monitor-only plan apply returned HTTP $HTTP_STATUS: $API_BODY"
    return 1
  }
  printf '%s\n' "$API_BODY" >"$TMP_DIR/G29-plan-apply.json" || return
  jq -e -L "$oracle" --slurpfile golden "$golden" \
    'include "plan-gate"; plan_applied($golden[0])' \
    "$TMP_DIR/G29-plan-apply.json" >/dev/null || {
    fail "G29 monitor-only plan apply differs from its singleton golden"
    return 1
  }
  eu_require_backend_protection "$API_BODY" || return
  eu_api_get /api/firewall/active || return
  printf '%s\n' "$API_BODY" >"$TMP_DIR/G29-plan-active.json" || return
  jq -e --slurpfile golden "$golden" '
    .active_config == "custom" and .policy_source == "plan"
    and .plan.actionIds == ["iec104-dpi"]
    and .plan.policyHash == $golden[0].policyHash
  ' "$TMP_DIR/G29-plan-active.json" >/dev/null || {
    fail "G29 IEC104 monitor plan provenance is not active"
    return 1
  }

  expected="$TMP_DIR/G29-plan-expected.json"
  jq '.config' "$golden" >"$expected" || return
  eu_lab_reconnect_cc || return
  eu_shadow_health G29-plan "$expected" || return
  eu_shadow_get /firewall/shadow-counters "$TMP_DIR/G29-plan-baseline.json" || return
  eu_shadow_check baseline "$TMP_DIR/G29-plan-baseline.json" || return
  eu_shadow_commands "$initial" G29-plan || return
  eu_api_get /api/firewall/active || return
  printf '%s\n' "$API_BODY" >"$TMP_DIR/G29-plan-active-after.json" || return
  jq -e --slurpfile golden "$golden" '
    .active_config == "custom" and .policy_source == "plan"
    and .plan.actionIds == ["iec104-dpi"]
    and .plan.policyHash == $golden[0].policyHash
  ' "$TMP_DIR/G29-plan-active-after.json" >/dev/null || {
    fail "G29 monitor commands changed plan provenance"
    return 1
  }
  pass "G29 IEC104 plan emitted breaker and OLTC observations without blocking commands; policy_source remained plan"
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
      || ! eu_shadow_commands "$initial" \
      || ! eu_shadow_plan_case "$initial"; then
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
