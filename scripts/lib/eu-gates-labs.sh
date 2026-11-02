# shellcheck shell=bash
# Student-route walks for the five served EU validator slots and two planning slots.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers and gates.

eu_lab_exec() {
  local node="$1" command="$2" expected="${3:-0}" timeout="${4:-25}" body
  printf 'RUN %s: %s\n' "$node" "$command"
  body=$(jq -nc --arg command "$command" --argjson timeout "$timeout" \
    '{command:$command,timeout_sec:$timeout}') || return
  eu_api_post "/api/workshop/nodes/$node/exec" "$body" || return
  printf 'RESULT %s\n' "$API_BODY"
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e --argjson code "$expected" \
    '.exit_code == $code and (.stdout | type == "string") and (.stderr | type == "string")' >/dev/null; then
    fail "$node student command expected exit $expected: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
}

eu_lab_exec_denied() {
  local node="$1" command="$2" timeout="${3:-25}" body
  printf 'RUN %s: %s\n' "$node" "$command"
  body=$(jq -nc --arg command "$command" --argjson timeout "$timeout" \
    '{command:$command,timeout_sec:$timeout}') || return
  eu_api_post "/api/workshop/nodes/$node/exec" "$body" || return
  printf 'RESULT %s\n' "$API_BODY"
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e '
    (.stdout | type == "string") and (.stderr | type == "string")
    and (.exit_code | type == "number" and . != 0)' >/dev/null; then
    fail "$node student command expected denial (nonzero exit): HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
}

eu_lab_cc() {
  local point="$1" operation="$2" expected="${3:-completed}" result
  eu_wait_for_operator '.link.state == "started" and all(.points[]; good)' \
    15 "student command fresh feedback" || return
  eu_lab_exec cc-1 "wget -qO- --header 'Content-Type: application/json' --post-data '{\"point\":\"$point\",\"operation\":\"$operation\"}' http://10.99.99.21:8080/api/commands" || return
  result=$(printf '%s' "$API_BODY" | jq -er '.stdout') || return
  if ! printf '%s' "$result" | jq -e --arg point "$point" --arg op "$operation" --arg result "$expected" \
    '.point == $point and .operation == $op and .result == $result' >/dev/null; then
    fail "student CC $point $operation expected $expected: $result"
    return 1
  fi
  pass "student CC $point $operation $expected"
}

# Resolve every step by the ID authored in its served scenario. The API's
# executor accepts an array index, so derive it from the current response.
eu_lab_step() {
  local scenario="$1" step_id="$2" step type index title
  eu_api_get "/api/scenarios/$scenario" || return
  step=$(printf '%s' "$API_BODY" | jq -ce --arg id "$step_id" '
    .steps | fromjson | map(select(.id == $id))
    | if length == 1 then .[0] else error("step ID must resolve exactly once") end') || {
    fail "$scenario cannot resolve authored step $step_id"
    return 1
  }
  type=$(printf '%s' "$step" | jq -r '.action.type // "manual"') || return
  title=$(printf '%s' "$step" | jq -r '.title // .id') || return
  index=$(printf '%s' "$API_BODY" | jq -er --arg id "$step_id" '
    .steps | fromjson | to_entries | map(select(.value.id == $id))
    | if length == 1 then .[0].key else error("step ID must resolve exactly once") end') || return
  printf 'WALK %s %s (step index %s): %s\n' "$scenario" "$step_id" "$index" "$title"
  case "$type" in
    manual|decision) return 0 ;;
    *) eu_execute_step "$step_id" "$scenario step $step_id" "$scenario" ;;
  esac
}

eu_lab_wait_traffic_quiescent() {
  local label="$1" attempt
  for ((attempt = 0; attempt < 35; attempt++)); do
    eu_api_get /api/traffic/status || return
    if printf '%s' "$API_BODY" | jq -e '.generating == false' >/dev/null; then
      return 0
    fi
    sleep 1
  done
  fail "$label scenario traffic did not finish within 35s"
  return 1
}

eu_lab_wait_capture() {
  local label="$1"
  eu_lab_wait_traffic_quiescent "$label" || return
  eu_api_post /api/traffic/generate '{"duration_sec":30}' || return
  [ "$HTTP_STATUS" = 200 ] || {
    fail "$label scenario traffic did not start: HTTP $HTTP_STATUS $API_BODY"
    return 1
  }
  # Capture from fw-1 as the scenario directs. The bounded run leaves time for
  # the asynchronous traffic recipe while staying inside the exec/API budget.
  eu_lab_exec fw-1 "tcpdump -i any -nn 'not host 127.0.0.1' -G 12 -W 1 -w /data/captures/baseline.pcap" 0 25 || return
  eu_lab_wait_traffic_quiescent "$label capture" || return
  eu_lab_exec fw-1 'ls -lh /data/captures/baseline.pcap' || return
  pass "$label student traffic and firewall capture completed"
}

# Every curriculum validator observes a fresh reset pre-state. A second reset
# separates PASS evidence from the earlier weak-policy FAIL attempt.
eu_lab_gate() {
  local scenario="$1" walk="$2" policy="$3" prepare="$4" result=0
  if ! eu_reset_all \
    || ! eu_apply_named weak \
    || ! eu_validate_scenario FAIL "$scenario" \
    || ! "$walk" "$scenario"; then
    result=1
  else
    if ! eu_reset_all \
      || ! eu_apply_named "$policy" \
      || ! "$prepare" "$scenario" \
      || ! eu_validate_scenario PASS "$scenario"; then
      result=1
    fi
  fi
  if [ "$result" -ne 0 ] && eu_api_get "/api/scenarios/$scenario/validate" 30; then
    printf 'STOPPED %s validator: %s\n' "$scenario" "$API_BODY"
  fi
  eu_apply_named weak || result=1
  eu_reset_all || result=1
  return "$result"
}

eu_lab_baseline_capture_analysis() {
  local scenario="$1"
  eu_lab_step "$scenario" generate-scenario-traffic || return
  eu_lab_wait_capture "$scenario" || return
  eu_lab_step "$scenario" capture-traffic-at-the-firewall || return
  eu_lab_step "$scenario" analyze-cross-zone-flows || return
  eu_lab_exec eng-ws-1 'ls -lh /home/abc/pcaps/baseline.pcap' || return
  eu_lab_exec eng-ws-1 'tshark -r /home/abc/pcaps/baseline.pcap -q -z conv,ip' || return
  eu_lab_exec eng-ws-1 'tshark -r /home/abc/pcaps/baseline.pcap -q -z io,stat,0,"tcp.port==2404","tcp.port==502","tcp.port==8080","tcp.port==8081"' || return
  eu_lab_exec eng-ws-1 "tshark -r /home/abc/pcaps/baseline.pcap -Y 'ip.src==10.20.20.0/24 and ip.dst==10.30.30.0/24'" || return
  eu_lab_exec eng-ws-1 "tshark -r /home/abc/pcaps/baseline.pcap -Y 'ip.src==10.10.10.0/24 and (ip.dst==10.30.30.0/24 or ip.dst==10.40.40.0/24)'"
}

eu_lab_baseline_walk() {
  local scenario="$1"
  eu_lab_step "$scenario" walk-the-topology-and-inspect-the-crossings || return
  eu_lab_baseline_capture_analysis "$scenario" || return
  eu_lab_step "$scenario" critical-conduits-what-must-flow || return
  eu_lab_step "$scenario" findings-what-you-observed || return
  eu_lab_step "$scenario" probe-for-latent-exposure || return
  eu_lab_exec kali-1 'nmap -Pn -p 2404 10.30.30.20 --max-retries 1 -T4' || return
  eu_lab_exec kali-1 'iec104cmd 10.30.30.20 gi --json' || return
  eu_lab_exec kali-1 'nmap -Pn -p 502 10.40.40.20 --max-retries 1 -T4' || return
  eu_lab_exec kali-1 'mbpoll -m tcp -a 1 -t 3 -0 -r 0 -c 11 -1 10.40.40.20' || return
  eu_lab_step "$scenario" transition-decide-what-to-fix-first
}

eu_lab_baseline_prepare() {
  eu_lab_baseline_capture_analysis "$1"
}

eu_lab_wait_good_cc() {
  eu_lab_wait_cc_delivered 30 "student route current CC GI and every inventory observation delivered" true
}

# Current operator state carries the delivery evidence; the CC stamps GI state
# and journal entries separately, so their timestamps are not an equality key.
eu_lab_cc_delivery_predicate() {
  local shape="$1" required="$2"
  cat <<'JQ'
    . as $state
    | ($state.link.gi.started_at | epoch) as $gi
    | ($state.schema == 3
      and ($state.cache_epoch | type == "string" and length > 0)
      and $state.link.connection_seq > 0
      and $state.link.state == "started"
      and $state.link.gi.state == "complete"
      and ($state.link.since | epoch) <= $gi
      and $gi >= (($state.rendered_at | epoch) - 600)
      and all($state.points[];
        (.received_at | epoch) as $received
        | ($received >= $gi and $received <= ($state.rendered_at | epoch))
          and .value != null
          and ((.quality.iv == false and .quality.nt == false
                and .quality.sb == false and .quality.bl == false
                and (.quality.ov == null or .quality.ov == false)
                and .stale_reason == null)
            or (.quality.iv == true and .quality.nt == false
                and .quality.sb == false and .quality.bl == false
                and .quality.ov == false
                and .stale_reason == "measurement_invalid")))
    )
JQ
  printf '    and ($state | (%s)) and ($state | (%s))\n' "$shape" "$required"
}

eu_lab_wait_cc_delivered() {
  local budget="$1" label="$2" required="$3" shape predicate started_ms now_ms state
  shape=$(eu_station_shape) || return
  predicate=$(eu_lab_cc_delivery_predicate "$shape" "$required") || return
  started_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    eu_api_get /api/operator/state || {
      fail "$label could not read current CC state"
      return 1
    }
    state="$API_BODY"
    eu_assert_operator_shape "$state" "$label" || return 1
    now_ms=$(eu_now_ms) || now_ms=""
    if [[ "$now_ms" =~ ^[0-9]+$ ]] \
      && [ "$((now_ms - started_ms))" -le "$((budget * 1000))" ] \
      && printf '%s' "$state" | jq -e "$EU_JQ_DEFS $predicate" >/dev/null 2>&1; then
      API_BODY="$state"
      return 0
    fi
    if [[ "$now_ms" =~ ^[0-9]+$ ]] \
      && [ "$((now_ms - started_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.1
  done
  fail "$label did not reach delivered CC observations within ${budget}s; last state: $state"
  return 1
}

# Close the actual TCP connection, even when the new policy permits it.
# Retiring conntrack alone under an allow can leave the old session alive.
eu_lab_reconnect_cc() {
  local requested shape
  shape=$(eu_station_shape) || return
  eu_api_post /api/operator/reconnect '' || return
  if [ "$HTTP_STATUS" != 202 ]; then
    fail "CC reconnect refused: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
  requested=$(printf '%s' "$API_BODY" | jq -er '.requested_at | strings') || return
  eu_wait_for_operator "(.link.gi.started_at | epoch) as \$gi | ($shape) and .link.state == \"started\" and (.link.since | epoch) > (\"$requested\" | epoch)
    and .link.gi.state == \"complete\" and (.link.gi.started_at | epoch) >= (.link.since | epoch)
    and all(.points[]; good and (.received_at | epoch) >= \$gi)" \
    30 "fresh CC session, completed GI and all inventory points since GI"
}

# Establish an in-band baseline, then force two completed lower steps. One
# step can stay inside the band; reset tap zero is not the nominal equilibrium.
# The validator independently requires PLC-origin requests and solved motion.
# With the daily profile running the final tap need not equal the starting tap.
eu_lab_wait_regulated_cc() {
  local pre_tap="$1" since="$2"
  eu_wait_for_regulation "point(\"oltc-1-tap\").value.value > $((pre_tap - 2))
    and point(\"oltc-1-auto\").value == true and point(\"oltc-1-remote\").value == true
    and point(\"plc-1-running\").value == true and point(\"plc-1-input-valid\").value == true
    and (point(\"oltc-1-current\").value | type == \"number\")" \
    "$since" "student regulation loop: PLC restores the operating band"
}

eu_lab_positive_conduits() {
  local pre_tap since
  eu_lab_exec kali-1 'curl -fsS --max-time 3 http://10.70.70.40:8080/api/points' || return
  eu_lab_exec eng-ws-1 'curl -fsS --max-time 3 http://10.30.30.30:8080/login' || return
  since=$(eu_utc) || return
  eu_wait_for_regulation true "$since" "student regulation loop: settled baseline" || return
  eu_lab_cc oltc-1-mode manual || return
  eu_get_operator || return
  pre_tap=$(printf '%s' "$API_BODY" | jq -er '.points[] | select(.id == "oltc-1-tap") | .value.value | numbers') || {
    fail "student regulation loop cannot read the pre-loop tap: $API_BODY"
    return 1
  }
  eu_lab_cc oltc-1-step lower || return
  eu_lab_cc oltc-1-step lower || return
  since=$(eu_utc) || return
  eu_lab_cc oltc-1-mode auto || return
  eu_lab_wait_regulated_cc "$pre_tap" "$since"
}

eu_lab_implementation_walk() {
  local scenario="$1"
  eu_lab_step "$scenario" exercise-overview || return
  eu_lab_step "$scenario" verify-access-to-containd || return
  eu_lab_step "$scenario" phase-1-verify-current-access-under-the-weak-baseline || return
  eu_lab_step "$scenario" phase-2-remove-broad-trust-and-establish-default-deny || return
  eu_lab_step "$scenario" implement-rules || return
  eu_lab_step "$scenario" phase-4-enable-logging-and-visibility || return
  eu_lab_reconnect_cc || return
  eu_lab_step "$scenario" test-positive || return
  eu_lab_positive_conduits || return
  eu_lab_step "$scenario" test-negative || return
  eu_lab_step "$scenario" export-your-policy
}

eu_lab_implementation_prepare() {
  eu_lab_reconnect_cc || return
  eu_lab_step "$1" test-positive || return
  eu_lab_positive_conduits
}

# Incremental raw snapshots survive any early return. The reducer writes a FAIL
# record for incomplete walks too; cleanup still owns Weak/reset restoration.
eu_lab_g22_event() {
  local name="$1" body="${2:-}"
  G22_TIME=$(eu_utc) || return
  if [ -n "$body" ]; then
    printf '%s\n' "$body" >"$TMP_DIR/G22-$name.json" || return
  fi
  jq -nc --arg event "$name" --arg at "$G22_TIME" \
    '{event:$event,at:$at}' >>"$TMP_DIR/G22-events.jsonl"
}

eu_lab_g22_wait_cc() {
  local since="$1" position="${2:-open}"
  eu_wait_for_operator ".link.state == \"started\"
    and four_points and (point(\"feeder-breaker-position\") |
      good and .value == \"$position\" and (.received_at | epoch) >= (\"$since\" | epoch))" \
    15 "G22 fresh GOOD CC breaker $position feedback"
}

eu_lab_g22_write_evidence() {
  if ! python3 "$SCRIPT_DIR/lib/eu-g22-evidence.py" record "$TMP_DIR" "$1" "$2" \
    2>"$TMP_DIR/G22-evidence.err"; then
    fail "G22 correlated evidence failed: $(cat "$TMP_DIR/G22-evidence.err")"
    return 1
  fi
}

eu_lab_hardening_walk() {
  local scenario="$1" result=0
  python3 -c 'import json,sys,uuid; print(json.dumps({"attempt_id":str(uuid.uuid4()),
    "scenario":sys.argv[1]}))' "$scenario" >"$TMP_DIR/G22-context.json" || return
  : >"$TMP_DIR/G22-events.jsonl" || return
  eu_lab_g22_walk "$scenario" || result=1
  eu_lab_g22_write_evidence "$scenario" "$result" || result=1
  eu_save_evidence || result=1
  return "$result"
}

eu_lab_g22_walk() {
  local scenario="$1" revision baseline_revision start end restore_start command_status
  eu_lab_step "$scenario" verify-normal-operations || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed"
    and .devices["breaker-1"].protection_tripped == false
    and .process.buses.s1.live == true and .process.truth_stale == false
    and .process.devices["feeder-breaker"].delivered == true
    and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' \
    15 "G22 closed supplied baseline" || return
  eu_lab_g22_event baseline-field "$WAIT_BODY" || return
  baseline_revision=$(printf '%s' "$WAIT_BODY" | jq -er '.devices["breaker-1"].revision | numbers') || return
  eu_wait_for_operator '.link.state == "started" and four_points
    and (point("feeder-breaker-position") | good and .value == "closed")' \
    15 "G22 independent GOOD closed CC baseline" || return
  eu_lab_g22_event baseline-cc "$WAIT_BODY" || return
  eu_lab_step "$scenario" attack-iec104-breaker-open || return
  eu_window_start G22-weak || return
  eu_lab_g22_event start || return
  start="$G22_TIME"
  command_status=0
  eu_lab_exec kali-1 'iec104cmd 10.30.30.20 dc 3001 open -sbo --json' || command_status=$?
  eu_lab_g22_event weak-command "$API_BODY" || return
  [ "$command_status" -eq 0 ] || return "$command_status"
  eu_wait_for_field ".devices[\"breaker-1\"].position == \"open\"
    and .process.buses.s1.live == false
    and .process.truth_stale == false
    and .devices[\"breaker-1\"].revision > $baseline_revision
    and (.process.solved_at | epoch) >= (\"$start\" | epoch)
    and .process.devices[\"feeder-breaker\"].delivered == true
    and .process.devices[\"feeder-breaker\"].solved_revision == .devices[\"breaker-1\"].revision" \
    15 "student IEC104 attack opens the breaker and drops section supply" || return
  eu_lab_g22_event consequence-field "$WAIT_BODY" || return
  eu_lab_g22_wait_cc "$start" || return
  eu_lab_g22_event consequence-cc "$WAIT_BODY" || return
  eu_lab_g22_event end || return
  end="$G22_TIME"
  eu_window_finish G22-weak || return
  eu_g22_capture_chain "$TMP_DIR/G22-weak.pcap" "$start" "$end" "$TMP_DIR/G22-packets.json" || return
  eu_lab_g22_event decoded || return
  eu_lab_g22_event restore-start || return
  restore_start="$G22_TIME"
  eu_lab_step "$scenario" restore-the-substation || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false
    and .process.buses.s1.live == true and .process.truth_stale == false
    and .process.devices["feeder-breaker"].delivered == true
    and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' \
    15 "student breaker restore and process solve" || return
  eu_lab_g22_event restoration-field "$WAIT_BODY" || return
  eu_lab_g22_wait_cc "$restore_start" closed || return
  eu_lab_g22_event restoration-cc "$WAIT_BODY" || return
  eu_lab_step "$scenario" apply-the-hardened-policy || return
  eu_lab_g22_event improved-apply "$API_BODY" || return
  eu_lab_reconnect_cc || return
  eu_lab_g22_event improved-control-before "$WAIT_BODY" || return
  eu_lab_step "$scenario" re-test-the-attack-under-the-hardened-policy || return
  revision=$(eu_get_field && printf '%s' "$API_BODY" | jq -er '.devices["breaker-1"].revision') || return
  eu_lab_g22_event improved-start || return
  command_status=0
  eu_lab_exec_denied kali-1 'iec104cmd 10.30.30.20 dc 3001 open -sbo --json' || command_status=$?
  eu_lab_g22_event improved-command "$API_BODY" || return
  [ "$command_status" -eq 0 ] || return "$command_status"
  eu_assert_unchanged_breaker "$revision" "student hardened IEC104 retest" || return
  eu_lab_g22_event improved-held-field "$API_BODY" || return
  eu_lab_g22_event improved-end || return
  # Reconnect is a positive listener control, not a substitute for weak-command
  # feedback (which was observed without a forced GI above).
  eu_lab_reconnect_cc || return
  eu_lab_g22_event improved-control-after "$WAIT_BODY" || return
  eu_lab_step "$scenario" validate || return
  eu_lab_g22_event completed
}

eu_assert_unchanged_breaker() {
  local revision="$1" label="$2" current
  eu_get_field || return
  current=$(printf '%s' "$API_BODY" | jq -er '.devices["breaker-1"] |
    select(.position == "closed" and .protection_tripped == false) | .revision') || {
    fail "$label changed the breaker or returned no revision"
    return 1
  }
  if [ "$current" = "$revision" ]; then
    pass "$label left breaker revision $revision unchanged"
  else
    fail "$label changed breaker revision $revision -> $current"
    return 1
  fi
}

eu_lab_hardening_prepare() {
  local scenario="$1" revision
  eu_lab_reconnect_cc || return
  revision=$(eu_get_field && printf '%s' "$API_BODY" | jq -er '.devices["breaker-1"].revision') || return
  eu_lab_exec_denied kali-1 'iec104cmd 10.30.30.20 dc 3001 open -sbo --json' || return
  eu_assert_unchanged_breaker "$revision" "post-hardening IEC104 retest" || return
  eu_lab_reconnect_cc || return
  eu_lab_step "$scenario" validate || return
  eu_lab_hardening_wait_good_cc
}

# Wait after the served Validate step so the independent validator can snapshot
# post-GI delivered CC evidence before its active TCP probes.
eu_lab_hardening_wait_good_cc() {
  eu_lab_wait_cc_delivered 15 \
    "fresh station observations and valid PLC input before hardening validation" \
    'point("plc-1-input-valid").value == true'
}

eu_lab_nmap_vendor() {
  local expected="$1"
  eu_lab_exec kali-1 'nmap -Pn -p 3389,5900 10.20.20.10 --max-retries 1 -T4' || return
  if ! printf '%s' "$API_BODY" | jq -e --arg expected "$expected" '
    .stdout as $out
    | if $expected == "open" then
        ($out | contains("3389/tcp open")) and ($out | contains("5900/tcp open"))
      else
        ($out | contains("3389/tcp filtered")) and ($out | contains("5900/tcp filtered"))
      end
  ' >/dev/null; then
    fail "vendor path nmap expected both RDP/VNC ports $expected: $API_BODY"
    return 1
  fi
  pass "enterprise-to-vendor RDP/VNC ports are $expected"
}

# The FC6 write runs from the served vendor-jump foothold; read-only audit
# evidence proves the accepted request's source without substituting sim truth.
eu_lab_vendor() {
  local target="$1" value="$2" expected_exit="${3:-0}" before_seq
  if [ "$expected_exit" = 0 ]; then
    eu_management rmu-1 GET '/api/audit?after=0' || return
    before_seq=$(printf '%s' "$API_BODY" | jq -er '.latest_seq | numbers') || {
      fail "vendor FC6 cannot establish an RMU audit cursor"
      return 1
    }
  fi
  eu_lab_exec vendor-jump-1 \
    "mbpoll -m tcp -a 1 -t 4 -0 -r 0 -1 $target $value" "$expected_exit" || return
  if [ "$expected_exit" = 0 ]; then
    eu_management rmu-1 GET "/api/audit?after=$before_seq" || return
    if ! printf '%s' "$API_BODY" | jq -e --argjson cursor "$before_seq" '
      [.entries[] | select(.seq > $cursor and .operation == "open" and .result == "accepted"
        and .after.position == "open" and .context.protocol == "modbus"
        and (.context.peer | startswith("10.20.20.10:")))] | length == 1
    ' >/dev/null; then
      fail "vendor FC6 did not produce an accepted, source-attributed RMU1 OPEN audit entry"
      return 1
    fi
    pass "vendor FC6 RMU1 OPEN is attributed to vendor-jump 10.20.20.10"
  fi
}

eu_lab_vendor_walk() {
  local scenario="$1" before_seq
  eu_lab_step "$scenario" confirm-the-lateral-path-is-open || return
  eu_lab_nmap_vendor open || return
  eu_lab_step "$scenario" pivot-via-rdp-or-vnc || return
  eu_lab_step "$scenario" use-the-vendor-foothold-to-attack-field-devices || return
  eu_lab_vendor 10.40.40.21 1 || return
  eu_get_field || return
  if ! printf '%s' "$API_BODY" | jq -e '.devices["rmu-1"].position == "open"' >/dev/null; then
    fail "vendor-originated FC6 did not open RMU1"
    return 1
  fi
  eu_lab_step "$scenario" restore-service || return
  eu_wait_for_field '.devices["rmu-1"].position == "closed" and .devices["rmu-2"].position == "open"
    and .process.truth_stale == false' 15 "student vendor lab device restore" || return
  eu_lab_step "$scenario" apply-hardened-policy || return
  eu_lab_nmap_vendor filtered || return
  eu_management rmu-1 GET '/api/audit?after=0' || return
  before_seq=$(printf '%s' "$API_BODY" | jq -er '.latest_seq | numbers') || {
    fail "vendor FC6 retest cannot establish an RMU audit cursor"
    return 1
  }
  eu_lab_vendor 10.40.40.21 1 1 || return
  eu_management rmu-1 GET "/api/audit?after=$before_seq" || return
  if ! printf '%s' "$API_BODY" | jq -e --argjson cursor "$before_seq" '
    [.entries[] | select(.seq > $cursor and .result == "accepted"
      and (.context.peer | startswith("10.20.20.10:")))] | length == 0
  ' >/dev/null; then
    fail "vendor-originated retest produced an accepted vendor-attributed RMU audit entry"
    return 1
  fi
  eu_get_field || return
  if ! printf '%s' "$API_BODY" | jq -e \
    '.devices["rmu-1"].position == "closed" and .devices["rmu-2"].position == "open"' >/dev/null; then
    fail "vendor-originated retest changed the restored RMU positions"
    return 1
  fi
  pass "vendor-originated retest was denied with no accepted vendor write; RMU positions held"
  eu_lab_step "$scenario" re-attempt-the-pivot || return
  eu_lab_step "$scenario" validate
}

eu_lab_vendor_prepare() {
  eu_lab_step "$1" validate
}

eu_lab_validation_walk() {
  local scenario="$1"
  eu_apply_named improved || return
  eu_lab_step "$scenario" confirm-hardened-config-is-active || return
  eu_lab_step "$scenario" generate-the-validation-report || return
  eu_api_post /api/firewall/validation-report '{}' || return
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e \
    '.summary.result == "PASS" and .summary.skipped == 0' >/dev/null; then
    fail "student validation report did not prove the full matrix: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
  pass "student validation report is PASS with no skipped probes"
  eu_lab_step "$scenario" reflection-what-did-your-plan-close
}

eu_lab_validation_prepare() {
  eu_lab_wait_good_cc || return
  eu_api_post /api/firewall/validation-report '{}' || return
  [ "$HTTP_STATUS" = 200 ] && printf '%s' "$API_BODY" | jq -e '.summary.result == "PASS" and .summary.skipped == 0' >/dev/null
}

eu_lab_visibility_failed() {
  if ! printf '%s' "$API_BODY" | jq -e 'any(.checks[];
    .status == "fail" and (.name == "Fresh CC session admitted after policy commit"
      or .name == "CC link, GI and station delivery"))' >/dev/null; then
    fail "bad change must fail CC visibility, not merely an unrelated prerequisite: $API_BODY"
    return 1
  fi
}

# Only the gate deliberately commits the incomplete request. Learners may
# reject/correct it before commit; an outage is never a completion condition.
eu_lab_bad_change() {
  local scenario="$1" prepare="$2" policy="$TMP_DIR/lesson-without-cc.json" result=0
  if ! eu_reset_all || ! eu_apply_named improved || ! eu_lab_reconnect_cc \
    || ! eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$policy" \
    || ! eu_cc_commit_loss "$policy" lesson-missing-cc \
    || ! eu_validate_scenario FAIL "$scenario" || ! eu_lab_visibility_failed \
    || ! eu_apply_named improved || ! eu_lab_wait_good_cc \
    || ! "$prepare" "$scenario" || ! eu_validate_scenario PASS "$scenario"; then
    result=1
  fi
  eu_apply_named weak || result=1
  eu_reset_all || result=1
  return "$result"
}

eu_curriculum_slot() {
  local role="$1" scenario="$2" expected_decisions="$3" packages scenario_body description
  eu_api_get /api/packages || return
  packages="$API_BODY"
  if ! printf '%s' "$packages" | jq -e --arg scenario "$scenario" --arg role "$role" '
    [.[] | select(.id == "eu-iec104-substation" and .active == true)] as $active
    | ($active | length) == 1
      and $active[0].curriculum.roles[$role] == $scenario
      and ($active[0].curriculum.catalog | type == "array" and length > 0)
      and $active[0].curriculum.budgetHours == 500
      and ($active[0].curriculum.teams | length) == 4
  ' >/dev/null; then
    fail "$scenario is not bound to a served EU decision catalog role"
    return 1
  fi
  eu_api_get "/api/scenarios/$scenario" || return
  scenario_body="$API_BODY"
  description=$(printf '%s' "$scenario_body" | jq -er '
    [.steps | fromjson[] | .description // ""] | join("\n")') || return
  if [ "$expected_decisions" = planning ]; then
    if ! printf '%s' "$scenario_body" | jq -e '
      ([.steps | fromjson[] | select(.action.type == "decision")] | length) == 1
    ' >/dev/null; then
      fail "$scenario does not serve its one curriculum-backed decision step"
      return 1
    fi
  elif ! jq -ne --arg description "$description" --argjson ids "$expected_decisions" '
    all($ids[]; . as $id | $description | contains(":::decision id=" + $id))
  ' >/dev/null; then
    fail "$scenario does not serve its package-bound decision prompts"
    return 1
  fi
  pass "$scenario is served with its bound decision catalog"
}

eu_lab_validation_slot() {
  eu_curriculum_slot planning eu-remediation-planning planning
}

eu_gate_G20() {
  eu_lab_gate eu-baseline-assessment eu_lab_baseline_walk weak eu_lab_baseline_prepare
}
eu_gate_G21() {
  eu_lab_gate eu-firewall-implementation eu_lab_implementation_walk improved eu_lab_implementation_prepare || return
  eu_lab_bad_change eu-firewall-implementation eu_lab_implementation_prepare
}
eu_gate_G22() {
  eu_lab_gate eu-hardening-configurations eu_lab_hardening_walk improved eu_lab_hardening_prepare || return
  eu_lab_bad_change eu-hardening-configurations eu_lab_hardening_prepare
}
eu_gate_G24() {
  eu_lab_gate eu-vendor-rdp-compromise eu_lab_vendor_walk improved eu_lab_vendor_prepare
}
eu_gate_G25() {
  eu_lab_gate eu-validation-evidence eu_lab_validation_walk improved eu_lab_validation_prepare
}
eu_gate_G26() {
  eu_curriculum_slot requirements eu-segmentation-requirements \
    '["current-policy-allowable","hardened-changes-rough-count","enterprise-to-field","enterprise-to-station","vendor-to-station","non-rtu-to-field","unauth-control","ot-eng-availability","fw-admin-skills","vendor-change-window","risk-acceptance-authority"]'
}
eu_gate_G27() {
  eu_lab_validation_slot
}
