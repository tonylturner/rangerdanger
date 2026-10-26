# shellcheck shell=bash
# EU INPUT-plane isolation, separate from US firewall-smoke expectations.
eu_gate_G23() {
  local policy
  if python3 "$SCRIPT_DIR/lib/containd-ui-probe.py" "$API"; then
    pass "G23 embedded containd bundle and authenticated session"
  else
    fail "G23 embedded containd bundle or authenticated session"
    return 1
  fi
  for policy in weak improved; do
    eu_apply_named "$policy" || return
    if python3 "$SCRIPT_DIR/lib/eu-management-probes.py" "$PACKAGE/manifest.json" "$policy" \
      >"$TMP_DIR/G23-$policy.jsonl" 2>"$TMP_DIR/G23-$policy.err"; then
      cat "$TMP_DIR/G23-$policy.jsonl"
      pass "G23 $policy management-plane probes (all zone hosts and both desktops)"
    else
      cat "$TMP_DIR/G23-$policy.jsonl" "$TMP_DIR/G23-$policy.err" >&2
      fail "G23 $policy management-plane probes"
    fi
  done
  # These fresh backend operations use lan3 after improved closes every zone.
  eu_api_get /api/substation/network-events || return
  eu_assert_jq "$API_BODY" '.source == "containd"' "G23 backend events remain available"
  eu_api_get /api/firewall/flows || return
  eu_api_post /api/pcap/start '{"duration_sec":2,"name":"eu-management"}' || return
  eu_assert_jq "$API_BODY" 'has("error") | not' "G23 backend pcap start"
  [ "$HTTP_STATUS" = 200 ] || { fail "G23 pcap start HTTP $HTTP_STATUS"; return 1; }
  # The backend keeps capturing true through file publication. A stopped
  # capture without a ready file is terminal, including EU exec-start errors.
  eu_wait_for_state /api/pcap/status '.file_ready == true or .capturing == false' \
    20 "G23 backend pcap ready after improved" || return
  if ! printf '%s' "$WAIT_BODY" | jq -e '.file_ready == true' >/dev/null 2>&1; then
    fail "G23 backend pcap stopped without a ready file; last response: $WAIT_BODY"
    return 1
  fi
  pass "G23 backend pcap ready after improved"
  eu_reset_all || return
  eu_apply_named weak || return
  pass "G23 backend events, flows, device reset and apply survive improved"
}
