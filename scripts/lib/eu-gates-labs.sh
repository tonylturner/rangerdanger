# shellcheck shell=bash
# Student-route curriculum walks. No sim action or field truth supplies CC data.
# Each gate resets its own baseline and restores it even after an aborted walk.

eu_lab_exec() {
  local node="$1" command="$2" expected="${3:-0}" body
  printf 'RUN %s: %s\n' "$node" "$command"
  body=$(jq -nc --arg command "$command" '{command:$command,timeout_sec:25}') || return
  eu_api_post "/api/workshop/nodes/$node/exec" "$body" || return
  printf 'RESULT %s\n' "$API_BODY"
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e --argjson code "$expected" \
    '.exit_code == $code and (.stdout | type == "string") and (.stderr | type == "string")' >/dev/null; then
    fail "$node student command expected exit $expected: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
}

eu_lab_cc() {
  local point="$1" operation="$2" expected="${3:-completed}" result
  eu_wait_for_operator '.link.state == "started" and all(.points[]; good)' 15 "student command fresh feedback" || return
  eu_lab_exec cc-1 "wget -qO- --header 'Content-Type: application/json' --post-data '{\"point\":\"$point\",\"operation\":\"$operation\"}' http://10.99.99.21:8080/api/commands" || return
  result=$(printf '%s' "$API_BODY" | jq -er '.stdout') || return
  if ! printf '%s' "$result" | jq -e --arg point "$point" --arg op "$operation" --arg result "$expected" \
    '.point == $point and .operation == $op and .result == $result' >/dev/null; then
    fail "student CC $point $operation expected $expected: $result"
    return 1
  fi
  pass "student CC $point $operation $expected"
}

# Log every visited step, including observational/planning steps. Execute each
# authored mutation once, except when the caller explicitly defers an action.
eu_lab_step() {
  local index="$1" mode="${2:-execute}" step id type
  eu_api_get "/api/scenarios/$SCENARIO_ID" || return
  step=$(printf '%s' "$API_BODY" | jq -ec --argjson index "$index" \
    '.steps | fromjson | if length == 10 then .[$index] else error("expected ten steps") end') || return
  id=$(printf '%s' "$step" | jq -er '.id') || return
  printf 'WALK %s step %d %s: %s\n' "$SCENARIO_ID" "$((index + 1))" "$id" "$(printf '%s' "$step" | jq -r '.title')"
  type=$(printf '%s' "$step" | jq -r '.action.type // "manual"') || return
  case "$type:$mode" in
    manual:*|decision:*|*:defer) return 0 ;;
    *) eu_execute_step "$id" "$SCENARIO_ID step $((index + 1))" "$SCENARIO_ID" ;;
  esac
}

eu_lab_tap() {
  local tap="$1" budget="$2"
  eu_wait_for_operator "point(\"oltc-1-tap\").value == {value:$tap,transient:false}
    and all(.points[]; good)" "$budget" "student observed settled tap $tap" || return
  pass "student observed settled tap $tap with GOOD full-station quality"
}

eu_lab_vendor() {
  local ip="$1" value="$2" expected="${3:-0}"
  eu_lab_exec vendor-jump-1 "mbpoll -m tcp -a 1 -0 -r 0 -t 4 -1 -o 3 $ip $value" "$expected"
}

eu_lab_baseline_exports() {
  eu_lab_exec cc-1 'wget -qO- http://10.99.99.21:8080/api/state' || return
  eu_lab_exec cc-1 "wget -qO- 'http://10.99.99.21:8080/api/journal?after=0'"
}

eu_lab_probe_matrix() {
  local node
  for node in kali-1 vendor-jump-1 eng-ws-1; do
    eu_lab_exec "$node" 'nmap -Pn -sT -p 502 10.40.40.20-23' || return
    eu_lab_exec "$node" 'nmap -Pn -sT -p 2404 10.30.30.20' || return
    eu_lab_exec "$node" 'nmap -Pn -sT -p 8080 10.30.30.30' || return
    eu_lab_exec "$node" 'nmap -Pn -sT -p 8080,8081 10.70.70.40' || return
  done
  eu_lab_exec kali-1 'mbpoll -m tcp -a 1 -0 -r 0 -c 3 -t 3 -1 10.40.40.21' || return
  eu_lab_exec kali-1 'iec104cmd 10.30.30.20 gi --json'
}

eu_lab2_walk() {
  local policy since until
  eu_lab_step 0 || return
  eu_lab_baseline_exports || return
  eu_lab_step 1 || return
  eu_api_post /api/pcap/start '{"duration_sec":15,"name":"eu-lab2"}' || return
  [ "$HTTP_STATUS" = 200 ] || return 1
  sleep 16
  eu_api_get /api/pcap/status || return
  if ! printf '%s' "$API_BODY" | jq -e '.file_ready == true' >/dev/null; then
    fail "lab2 portal capture not ready: $API_BODY"; return 1
  fi
  curl -fsS --max-time 15 "$API/api/pcap/download" -o "$TMP_DIR/lab2.pcap" || return
  [ -s "$TMP_DIR/lab2.pcap" ] || return 1
  pass "lab2 portal fifteen-second PCAP downloaded"
  eu_lab_step 2 || return
  eu_lab_probe_matrix || return
  eu_lab_step 3 || return
  eu_lab_step 4 || return
  printf 'PLAN core 60h plus endpoint authority review 20h; 40h per role; same-subnet redesign deferred\n'
  eu_lab_step 5 || return
  # The curriculum explicitly requires stopping here if the supplied policy
  # lacks logged HTTP denies. Do not turn a default-drop timeout into evidence.
  policy=$(eu_firewall_config) || return
  if ! printf '%s' "$policy" | jq -e '
    [.. | objects | select(.action? == "DENY" and .log? == true)
      | .protocols[]? | select(.name == "tcp") | .port | tostring] as $ports
    | ($ports | index("8080")) != null and ($ports | index("8081")) != null' >/dev/null; then
    fail "lab 2 step 6 hold: supplied policy lacks explicit logged HTTP denies; policy files are outside this lane"
    return 1
  fi
  # Roll back, prove the restored baseline, then reapply the reviewed policy.
  eu_apply_named weak || return
  eu_lab_probe_matrix || return
  eu_lab_step 5 || return
  since=$(eu_utc)
  eu_lab_step 6 || return
  eu_lab_baseline_exports || return
  eu_lab_step 7 || return
  eu_lab_tap 1 40 || return
  eu_lab_exec kali-1 'curl -fsS http://10.70.70.40:8080/api/points' || return
  until=$(eu_utc)
  eu_lab_exec kali-1 "curl -fsS 'http://10.70.70.40:8080/api/history?point=oltc-1-tap&since=$since&until=$until'" || return
  eu_lab_step 8 || return
  # Port probes (rather than Modbus reads) generate the negative matrix.
  local node
  for node in kali-1 vendor-jump-1 eng-ws-1; do
    eu_lab_exec "$node" 'nmap -Pn -sT --max-retries 0 --host-timeout 20s -p 502 10.40.40.20-23' || return
    eu_lab_exec "$node" 'nmap -Pn -sT --max-retries 0 --host-timeout 20s -p 2404 10.30.30.20' || return
    eu_lab_exec "$node" 'nmap -Pn -sT --max-retries 0 --host-timeout 20s -p 8080 10.30.30.30' || return
    eu_lab_exec "$node" 'nmap -Pn -sT --max-retries 0 --host-timeout 20s -p 8081 10.70.70.40' || return
  done
  eu_lab_exec eng-ws-1 'curl -fsS http://10.30.30.30:8080/' || return
  eu_lab_step 9
}

eu_lab3_walk() {
  eu_lab_step 0 || return
  eu_lab_baseline_exports || return
  eu_lab_step 1 || return
  printf 'PREDICT RMU1 open loses S2 only; live-live tie close refused; tie open after transfer loses S2 only\n'
  eu_lab_step 2 || return
  eu_lab_cc rmu-1-command open || return
  eu_wait_for_operator 'point("rmu-1-section-kv").value < 0.1 and point("rmu-2-source-kv").value > 18' 10 "RMU1 open S2 dead S3 live" || return
  eu_lab_cc rmu-2-command close || return
  eu_wait_for_operator 'point("rmu-2-section-kv").value > 18' 10 "S2 restored from F2" || return
  eu_lab_cc rmu-2-command open || return
  eu_lab_cc rmu-1-command close || return
  eu_lab_step 3 || return
  eu_lab_cc rmu-1-command open rejected || return
  eu_lab_step 4 || return
  eu_lab_cc rmu-2-command close rejected || return
  eu_lab_step 5 || return
  eu_wait_for_operator 'point("feeder-protection-trip").value == true
    and point("rmu-1-fault-passage").value == false
    and point("rmu-1-source-kv").value < 0.1
    and all(.points[]; good)' 15 "S1 trip and clear downstream FPI" || return
  eu_lab_step 6 || return
  eu_lab_cc rmu-1-command open || return
  eu_lab_cc rmu-2-command close || return
  eu_lab_step 7 || return
  eu_lab_vendor 10.40.40.22 1 || return
  eu_lab_cc rmu-2-command close || return
  eu_lab_step 8 || return
  eu_lab_vendor 10.40.40.22 1 1 || return
  eu_lab_cc rmu-2-command open || return
  eu_lab_cc rmu-2-command close || return
  eu_lab_step 9
}

eu_lab4_clock_modes() {
  # The authored hold is needed for GPS polling, not a substitute for a check.
  sleep 15
  eu_lab_cc oltc-1-mode manual || return
  sleep 2
  eu_lab_cc oltc-1-mode auto || return
  sleep 2
}

eu_lab4_walk() {
  local since until
  since=$(eu_utc)
  eu_lab_step 0 || return
  eu_lab_exec plc-1 'cat /var/lib/openplc-eu-seed.sha256' || return
  eu_lab_baseline_exports || return
  eu_lab_step 1 || return
  eu_lab_step 2 || return
  eu_lab_tap 1 40 || return
  sleep 12
  eu_lab_tap 1 5 || return
  eu_lab_step 3 || return
  eu_lab_tap 8 130 || return
  sleep 12
  eu_lab_tap 8 5 || return
  eu_lab_step 4 || return
  eu_lab_tap -7 240 || return
  eu_lab_step 5 || return
  eu_lab_tap 0 130 || return
  eu_lab_cc oltc-1-mode manual || return
  for _ in 1 2 3 4 5 6 7 8; do eu_lab_cc oltc-1-step lower || return; done
  eu_lab_cc oltc-1-step lower rejected || return
  eu_lab_cc oltc-1-step raise || return
  eu_lab_cc oltc-1-mode auto || return
  eu_lab_tap 0 130 || return
  eu_lab_cc oltc-1-mode manual || return
  eu_lab_step 6 || return
  eu_lab_vendor 10.40.40.23 1 || return
  eu_lab_tap -1 10 || return
  eu_lab_cc oltc-1-step raise || return
  eu_lab_step 7 defer || return
  eu_apply_named improved || return
  eu_lab_vendor 10.40.40.23 1 1 || return
  eu_lab_cc oltc-1-step lower || return
  eu_lab_cc oltc-1-step raise || return
  eu_lab_cc oltc-1-mode auto || return
  eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$TMP_DIR/lab4-no-cc.json" || return
  eu_apply_custom "$TMP_DIR/lab4-no-cc.json" "lab4 CC outage" || return
  eu_execute_step source-pin-and-cc-outage "lab4 outage session cut" "$SCENARIO_ID" || return
  eu_wait_for_operator 'all(.points[]; .quality.iv and .quality.nt and .stale_reason == "link_lost")' 15 "lab4 CC outage stale" || return
  sleep 5
  eu_apply_named improved || return
  eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and all(.points[]; good)' 20 "lab4 recovery GI" || return
  until=$(eu_utc)
  # The authored portal trend uses this read proxy; improved does not grant
  # the CC teaching host the enterprise historian-read conduit.
  eu_api_get "/api/historian/history?point=oltc-1-bus-kv&since=$since&until=$until" || return
  eu_lab_step 8 || return
  eu_lab4_clock_modes || return
  eu_lab_step 9 || return
  eu_lab4_clock_modes
}

eu_lab_gate() {
  local SCENARIO_ID="$1" walk="$2" result=0
  eu_apply_named weak && eu_reset_all && eu_validate_scenario FAIL && "$walk" && eu_validate_scenario PASS || result=1
  # Keep the validator result in the API log before clearing physical evidence.
  if [ "$result" -ne 0 ]; then
    if eu_api_get "/api/scenarios/$SCENARIO_ID/validate" 30; then
      printf 'STOPPED %s validator: %s\n' "$SCENARIO_ID" "$API_BODY"
    fi
  fi
  eu_apply_named weak || result=1
  eu_reset_all || result=1
  return "$result"
}

eu_gate_G20() { eu_lab_gate eu-station-conduits eu_lab2_walk; }
eu_gate_G21() { eu_lab_gate eu-rmu-switching eu_lab3_walk; }
eu_gate_G22() { eu_lab_gate eu-oltc-plc-historian eu_lab4_walk; }
