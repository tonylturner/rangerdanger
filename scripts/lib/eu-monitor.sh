# shellcheck shell=bash
# IEC104 client-session helpers for the EU smoke gate: Kali iec104cmd runs, the
# supervised Kali background monitor, and firewall conntrack session tuples.

eu_jsonl_count() {
  local file="$1"
  python3 - "$file" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, encoding="utf-8") as stream:
    lines = stream.readlines()
if not lines:
    raise SystemExit("empty JSON-lines response")
items = []
for number, line in enumerate(lines, 1):
    if not line.strip():
        raise SystemExit(f"blank JSON-lines response at line {number}")
    try:
        item = json.loads(line)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"unparsable JSON-lines response at line {number}: {exc}")
    if not isinstance(item, dict):
        raise SystemExit(f"JSON-lines response at line {number} is not an object")
    items.append(item)
print(len(items))
PY
}
eu_run_kali_gi() {
  local label="$1" status="$2" output="$TMP_DIR/$1.jsonl" stderr_file="$TMP_DIR/$1.stderr"
  docker exec "$KALI" iec104cmd "$RTU_IP" gi --json >"$output" 2>"$stderr_file"
  CLI_STATUS=$?
  if [ "$CLI_STATUS" -ne "$status" ]; then
    fail "$label iec104cmd exited $CLI_STATUS, expected $status; stderr: $(cat "$stderr_file")"
    return 1
  fi
  if [ "$status" = "0" ]; then
    if ! CLI_LINES=$(eu_jsonl_count "$output" 2>"$TMP_DIR/jsonl.err") \
      || [ "$CLI_LINES" != "1" ] \
      || ! jq -e '
        .event == "result" and .op == "gi" and .status == "ok"
        and (.at | type == "string" and length > 0)
      ' "$output" >/dev/null 2>&1; then
      fail "$label iec104cmd success lacks one parseable GI result: $(cat "$TMP_DIR/jsonl.err")"
      return 1
    fi
    pass "$label iec104cmd exited 0 with a confirmed GI result"
  elif [ "$status" = "2" ] && [ ! -s "$output" ] \
    && grep -Fq 'connect failed' "$stderr_file"; then
    # iec104cmd exits before emitting JSON when it cannot open a TCP session.
    pass "$label iec104cmd exited 2 with an explicit connection failure"
  else
    fail "$label iec104cmd rejection output is not the expected empty-output connect-failure path: $(cat "$stderr_file")"
    return 1
  fi
  return 0
}
eu_monitor_read() {
  local raw
  raw=$(docker exec "$KALI" cat "$MONITOR_LOG" 2>"$TMP_DIR/monitor-cat.err") || {
    printf 'monitor log read failed: %s\n' "$(cat "$TMP_DIR/monitor-cat.err")" >&2
    return 1
  }
  if [ -z "$raw" ]; then
    MONITOR_BODY=""
    MONITOR_LINES=0
    return 0
  fi
  printf '%s\n' "$raw" >"$TMP_DIR/monitor-read.jsonl"
  MONITOR_LINES=$(eu_jsonl_count "$TMP_DIR/monitor-read.jsonl" 2>"$TMP_DIR/jsonl.err") || {
    printf 'monitor emitted unparsable JSON lines: %s\n' "$(cat "$TMP_DIR/jsonl.err")" >&2
    return 1
  }
  if ! printf '%s\n' "$raw" | jq -s -e '
    all(.[]; if .event == "connected" then
      (.at | type == "string") and has("local") and .local == null
      and (.peer | type == "string")
    elif .event == "asdu" then
      (.at | type == "string") and has("type") and (.cot | type == "number")
      and (.ioa | type == "number") and has("value") and has("quality") and has("time")
    elif .event == "gi_complete" or .event == "held" then
      (.at | type == "string")
    elif .event == "link_down" then
      (.at | type == "string") and (.reason | type == "string")
    else false end)
  ' >/dev/null 2>&1; then
    printf 'monitor JSONL event does not satisfy the §6 --json schema\n' >&2
    return 1
  fi
  MONITOR_BODY="$raw"
  return 0
}
# The monitor runs under a supervisor shell in Kali that records the
# iec104cmd PID itself (not a wrapper's), waits for it and writes its exit
# status, so cleanup can signal and verify the owned child.
eu_monitor_start() {
  local suffix
  suffix="$$"
  MONITOR_LOG="/tmp/eu-iec104-smoke-$suffix.jsonl"
  MONITOR_ERR="/tmp/eu-iec104-smoke-$suffix.stderr"
  MONITOR_RC="/tmp/eu-iec104-smoke-$suffix.exit"
  MONITOR_PID="/tmp/eu-iec104-smoke-$suffix.pid"
  if ! docker exec "$KALI" sh -c \
    "rm -f '$MONITOR_LOG' '$MONITOR_ERR' '$MONITOR_RC' '$MONITOR_PID' \
      && : >'$MONITOR_LOG' && : >'$MONITOR_ERR'" \
    >/dev/null 2>"$TMP_DIR/monitor-files.err"; then
    fail "could not prepare background-monitor files: $(cat "$TMP_DIR/monitor-files.err")"
    return 1
  fi
  monitor_running=1
  # shellcheck disable=SC2016
  if ! docker exec -d "$KALI" sh -c '
    iec104cmd "$1" monitor --json --hold 180 --gi-every 2 >"$2" 2>"$3" &
    child=$!
    printf "%s\n" "$child" >"$4"
    wait "$child"
    printf "%s\n" "$?" >"$5"
  ' eu-monitor "$RTU_IP" "$MONITOR_LOG" "$MONITOR_ERR" "$MONITOR_PID" "$MONITOR_RC" \
    >/dev/null 2>"$TMP_DIR/monitor-start.err"; then
    fail "could not launch Kali background monitor: $(cat "$TMP_DIR/monitor-start.err")"
    return 1
  fi
  return 0
}

# eu_stop_monitor: stop the owned iec104cmd child. Kali has no kill
# executable, so the shell builtin signals inside `sh -c`, and only while the
# supervisor has no exit status yet (the PID is still its unreaped child). The
# stop counts only once the supervisor has recorded the child's exit status.
# shellcheck disable=SC2329
eu_stop_monitor() {
  [ "$monitor_running" -eq 1 ] || return 0
  local pid="" attempt rc
  for ((attempt = 0; attempt < 20; attempt++)); do
    pid=$(docker exec "$KALI" cat "$MONITOR_PID" 2>/dev/null) || pid=""
    [[ "$pid" =~ ^[0-9]+$ ]] && break
    sleep 0.1
  done
  # shellcheck disable=SC2016
  if [[ "$pid" =~ ^[0-9]+$ ]] && docker exec "$KALI" sh -c 'test -s "$2" || kill -TERM "$1" || { sleep 1; test -s "$2"; }' \
    eu-stop "$pid" "$MONITOR_RC" >"$TMP_DIR/monitor-kill.out" 2>&1; then
    for ((attempt = 0; attempt < 40; attempt++)); do
      if rc=$(docker exec "$KALI" cat "$MONITOR_RC" 2>/dev/null) && [[ "$rc" =~ ^[0-9]+$ ]]; then
        monitor_running=0
        pass "owned background monitor (iec104cmd PID $pid) stopped; supervisor recorded exit status $rc"
        return 0
      fi
      sleep 0.25
    done
  fi
  # shellcheck disable=SC2034
  MONITOR_STOP_FAILED=1
  # shellcheck disable=SC2016
  fail "could not stop owned background monitor (iec104cmd PID ${pid:-unrecorded}): $(cat "$TMP_DIR/monitor-kill.out" 2>/dev/null); process: $(docker exec "$KALI" sh -c 'head -n 3 "/proc/$1/status" 2>&1' eu-diag "${pid:-0}" 2>&1)"
  return 1
}
eu_monitor_wait_event() {
  local event="$1" seconds="$2" i
  for ((i = 0; i < seconds * 4; i++)); do
    if ! eu_monitor_read; then
      fail "monitor JSONL became unparsable while waiting for event $event"
      return 1
    fi
    if [ "$MONITOR_LINES" -gt 0 ] \
      && printf '%s\n' "$MONITOR_BODY" | jq -s -e --arg event "$event" \
        'any(.[]; .event == $event)' >/dev/null 2>&1; then
      return 0
    fi
    if docker exec "$KALI" sh -c "test -s '$MONITOR_RC'" >/dev/null 2>&1; then
      fail "monitor exited before event $event: $(docker exec "$KALI" cat "$MONITOR_RC" 2>/dev/null)"
      return 1
    fi
    sleep 0.25
  done
  fail "monitor did not emit event $event within ${seconds}s"
  return 1
}
eu_monitor_asdu_count() {
  if ! eu_monitor_read; then
    fail "monitor JSONL is unparsable while counting ASDUs"
    return 1
  fi
  if [ "$MONITOR_LINES" -eq 0 ]; then
    MONITOR_ASDUS=0
    return 0
  fi
  # shellcheck disable=SC2034
  MONITOR_ASDUS=$(printf '%s\n' "$MONITOR_BODY" | jq -s '[.[] | select(.event == "asdu")] | length') || {
    fail "monitor ASDU count response was unparsable"
    return 1
  }
}
eu_monitor_finish() {
  local expected="$1" timeout="$2" start_ms="${3:-}" now_ms elapsed i rc
  [ -n "$start_ms" ] || start_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || {
    fail "monitor exit wait cannot establish monotonic timeout"
    return 1
  }
  for ((i = 0; i < timeout * 4; i++)); do
    now_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]]; then
      fail "monitor exit wait cannot read monotonic clock"
      return 1
    fi
    elapsed=$((now_ms - start_ms))
    if [ "$elapsed" -gt "$((timeout * 1000))" ]; then
      break
    fi
    if docker exec "$KALI" sh -c "test -s '$MONITOR_RC'" >/dev/null 2>&1; then
      rc=$(docker exec "$KALI" cat "$MONITOR_RC" 2>"$TMP_DIR/monitor-rc.err") || {
        fail "monitor exit status file could not be read: $(cat "$TMP_DIR/monitor-rc.err")"
        return 1
      }
      if ! eu_monitor_read; then
        fail "monitor emitted unparsable JSON lines on exit"
        return 1
      fi
      if [ "$rc" != "$expected" ]; then
        fail "monitor exited $rc, expected $expected"
        return 1
      fi
      if [ "$MONITOR_LINES" -eq 0 ]; then
        fail "monitor exited $rc with an empty JSON-lines response"
        return 1
      fi
      # shellcheck disable=SC2034
      monitor_running=0
      pass "background monitor exited $expected with $MONITOR_LINES parseable JSON objects"
      return 0
    fi
    sleep 0.25
  done
  fail "background monitor did not exit within ${timeout}s"
  return 1
}

eu_monitor_no_asdu_after() {
  local cutoff="$1" after_kill
  if ! eu_monitor_read; then
    fail "monitor log was unparsable after conntrack kill"
    return 1
  fi
  after_kill=$(printf '%s\n' "$MONITOR_BODY" | python3 -c '
import calendar, datetime, json, re, sys
cutoff = int(sys.argv[1])
for line in sys.stdin:
    event = json.loads(line)
    if event.get("event") != "asdu":
        continue
    at = event.get("at")
    if not isinstance(at, str):
        raise SystemExit("ASDU event has no parseable timestamp")
    match = re.fullmatch(r"(.*T\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)", at)
    if not match:
        raise SystemExit("ASDU timestamp is not RFC3339Nano")
    parsed = datetime.datetime.fromisoformat(match.group(1) + match.group(3).replace("Z", "+00:00"))
    seconds = calendar.timegm(parsed.astimezone(datetime.timezone.utc).utctimetuple())
    fraction = (match.group(2) or "")[:9].ljust(9, "0")
    if seconds * 1_000_000_000 + int(fraction) > cutoff:
        raise SystemExit("ASDU received after exact conntrack kill")
print("none")
' "$cutoff" 2>"$TMP_DIR/post-kill-parse.err") || after_kill=""
  if [ "$after_kill" = "none" ]; then
    pass "G6 no ASDU was received after conntrack kill"
    return 0
  fi
  fail "G6 post-kill monitor data was invalid or contained later ASDUs: $(cat "$TMP_DIR/post-kill-parse.err")"
  return 1
}

# eu_read_sessions DPORT SRC [allow-empty]: the filtered conntrack list. An
# empty list fails unless the caller is proving that no session exists.
eu_read_sessions() {
  local dport="$1" src="$2" minimum=1
  [ "${3:-}" != "allow-empty" ] || minimum=0
  if ! eu_api_get "/api/firewall/sessions?dport=$dport&src=$src"; then
    fail "firewall session list for src $src dport $dport is unavailable or unparsable"
    return 1
  fi
  SESSION_BODY="$API_BODY"
  if ! printf '%s' "$SESSION_BODY" | jq -e --arg src "$src" --argjson dport "$dport" \
    --argjson minimum "$minimum" '
    type == "object"
    and (.sessions | type == "array")
    and (.count | type == "number" and . >= 0 and floor == .)
    and .count == (.sessions | length)
    and .count >= $minimum
    and all(.sessions[];
            (.proto | type) == "string"
            and (.src | type) == "string"
            and (.dst | type) == "string"
            and (.sport | type == "number" and floor == . and . >= 1 and . <= 65535)
            and (.dport | type == "number" and floor == . and . == $dport)
            and has("state") and has("timeout") and .src == $src)
  ' >/dev/null 2>&1; then
    fail "firewall session envelope is empty or violates the filtered integer-port contract (src $src dport $dport): $SESSION_BODY"
    return 1
  fi
  return 0
}
eu_session_tuple() {
  local body="$1" src="$2" dst="$3" dport="$4" matches count
  SESSION_TUPLE=""
  matches=$(printf '%s' "$body" | jq -c --arg src "$src" --arg dst "$dst" \
    --argjson dport "$dport" '
      [.sessions[] | select((.proto | ascii_downcase) == "tcp"
        and .src == $src and .dst == $dst and .dport == $dport)]
    ') || {
      printf 'session response cannot be parsed while selecting %s -> %s tcp/%s\n' \
        "$src" "$dst" "$dport" >&2
      return 1
    }
  count=$(printf '%s' "$matches" | jq 'length') || count=""
  if [ "$count" != "1" ]; then
    printf 'expected exactly one session tuple for %s -> %s tcp/%s, found %s\n' \
      "$src" "$dst" "$dport" "${count:-unparsable}" >&2
    return 1
  fi
  # shellcheck disable=SC2034
  SESSION_TUPLE="$matches"
  return 0
}
eu_kill_tuple() {
  local tuple="$1" src dst sport dport proto response
  proto=$(printf '%s' "$tuple" | jq -r '.[0].proto // empty') || return 1
  src=$(printf '%s' "$tuple" | jq -r '.[0].src // empty') || return 1
  dst=$(printf '%s' "$tuple" | jq -r '.[0].dst // empty') || return 1
  sport=$(printf '%s' "$tuple" | jq -r '.[0].sport // empty') || return 1
  dport=$(printf '%s' "$tuple" | jq -r '.[0].dport // empty') || return 1
  if [ -z "$proto" ] || [ -z "$src" ] || [ -z "$dst" ] \
    || ! [[ "$sport" =~ ^[0-9]+$ ]] || ! [[ "$dport" =~ ^[0-9]+$ ]]; then
    fail "session kill tuple is incomplete or has non-integer ports: $tuple"
    return 1
  fi
  if ! eu_api_post "/api/firewall/sessions/kill" \
    "{\"proto\":\"$proto\",\"src\":\"$src\",\"dst\":\"$dst\",\"sport\":$sport,\"dport\":$dport}"; then
    fail "session kill returned no parseable response for $src:$sport -> $dst:$dport"
    return 1
  fi
  response="$API_BODY"
  if [ "$HTTP_STATUS" != "200" ] \
    || ! printf '%s' "$response" | jq -e '.status == "deleted"' >/dev/null 2>&1; then
    fail "conntrack kill was not a confirmed 200/deleted for $src:$sport -> $dst:$dport: HTTP $HTTP_STATUS $response"
    return 1
  fi
  pass "exact conntrack tuple deleted: $src:$sport -> $dst:$dport"
  return 0
}

# eu_wait_session_history_drained SRC DST DPORT LABEL: before a monitor whose
# tuple must be the only match, wait for earlier gate clients' closing rows
# (the G4 one-shot GI leaves TIME_WAIT) to expire. A live row has no gate owner
# at this point and fails. Nothing is flushed or killed.
eu_wait_session_history_drained() {
  local src="$1" dst="$2" dport="$3" label="$4" rows live budget="" start_ms now_ms
  start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    eu_read_sessions "$dport" "$src" allow-empty || return 1
    rows=$(printf '%s' "$SESSION_BODY" | jq -c --arg src "$src" --arg dst "$dst" --argjson dport "$dport" '
      [.sessions[] | select((.proto | ascii_downcase) == "tcp"
        and .src == $src and .dst == $dst and .dport == $dport)]
      | map(. + {closing: (.state as $state
          | ["TIME_WAIT", "CLOSE", "CLOSE_WAIT", "FIN_WAIT", "LAST_ACK"] | any(. == $state))})
    ') || {
      fail "$label cannot select $src -> $dst tcp/$dport rows: $SESSION_BODY"
      return 1
    }
    live=$(printf '%s' "$rows" | jq -c 'map(select(.closing | not))')
    if [ "$live" != "[]" ]; then
      fail "$label found live $src -> $dst tcp/$dport conntrack rows that no gate step owns: $live"
      return 1
    fi
    if [ "$rows" = "[]" ]; then
      pass "$label no earlier $src -> $dst tcp/$dport conntrack rows remain"
      return 0
    fi
    if [ -z "$budget" ]; then
      # Closing rows expire after their own timeout; Linux's longest default
      # closing state (TIME_WAIT, FIN_WAIT) is 120 s.
      budget=$(printf '%s' "$rows" | jq -r '
        if all(.[]; .timeout | type == "number") then map(.timeout) | max + 5 else "invalid" end')
      if ! [[ "$budget" =~ ^[0-9]+$ ]] || [ "$budget" -gt 125 ]; then
        fail "$label closing rows have missing or over-long timeouts: $rows"
        return 1
      fi
      printf '%s waiting up to %ss for closing conntrack rows to expire: %s\n' "$label" "$budget" "$rows"
    fi
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]] || [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      fail "$label closing conntrack rows did not expire within ${budget}s: $rows"
      return 1
    fi
    sleep 1
  done
}

# eu_check_g13_audit AUDIT GENERATION BASELINE CURSOR REVISIONS: the device
# audit after CURSOR (generation compared as a number) is non-empty and complete,
# records exactly the REVISIONS (JSON array) as truth changes, and a first_solve
# for each of them and for nothing at or below BASELINE.
eu_check_g13_audit() {
  local audit="$1" generation="$2" baseline="$3" cursor="$4" revisions="$5" revision
  if printf '%s' "$audit" | jq -e --argjson generation "$generation" \
    '.generation == $generation' >/dev/null 2>&1; then
    pass "G13 audit generation equals device generation $generation"
  else
    fail "G13 audit generation differs from device generation $generation: $audit"
  fi
  if printf '%s' "$audit" | jq -e --argjson cursor "$cursor" '
    (.entries | type == "array" and length > 0)
    and all(.entries[]; (.seq | type == "number") and .seq > $cursor)
    and (.oldest_seq | type == "number" and . <= $cursor + 1)
  ' >/dev/null 2>&1; then
    pass "G13 audit after cursor $cursor is non-empty and lost no entries"
  else
    fail "G13 audit after cursor $cursor is empty, not cursor-bounded, or lost entries: $audit"
    return 1
  fi
  if printf '%s' "$audit" | jq -e --argjson revisions "$revisions" '
    [.entries[] | select(.operation != "first_solve"
        and (.before.revision | type == "number") and (.after.revision | type == "number")
        and .after.revision > .before.revision) | .after.revision] | unique == ($revisions | sort)
  ' >/dev/null 2>&1; then
    pass "G13 device audit records exactly the truth revisions $revisions"
  else
    fail "G13 device audit truth changes differ from the observed revisions $revisions"
  fi
  for revision in $(printf '%s' "$revisions" | jq -r '.[]'); do
    if printf '%s' "$audit" | jq -e --argjson revision "$revision" '
      any(.entries[]; .operation == "first_solve" and .detail.revision == $revision)
    ' >/dev/null 2>&1; then
      pass "G13 solver first_solve audit covers device revision $revision"
    else
      fail "G13 solver lacks first_solve audit for device revision $revision"
    fi
  done
  if printf '%s' "$audit" | jq -e --argjson baseline "$baseline" '
    all(.entries[] | select(.operation == "first_solve");
      (.detail.revision | type == "number") and .detail.revision > $baseline)
  ' >/dev/null 2>&1; then
    pass "G13 every first_solve after the cursor is for a post-baseline revision"
  else
    fail "G13 a first_solve after the cursor is unparsable or not after baseline revision $baseline"
  fi
}


# eu_iec104_command_filter CA IOA SE: CC-to-RTU frames carrying a C_DC_NA_1
# (TypeID 46) activation for the breaker command with S/E = SE. The # layer
# operator binds the fields to one ASDU, so a segment carrying several APDUs
# cannot combine fields of different ASDUs; up to k = 12 APDUs per segment.
eu_iec104_command_filter() {
  local ca="$1" ioa="$2" se="$3" layer filter=""
  for layer in 1 2 3 4 5 6 7 8 9 10 11 12; do
    filter="$filter${filter:+ || }(iec60870_asdu.typeid#$layer == 46"
    filter="$filter && iec60870_asdu.causetx#$layer == 6 && iec60870_asdu.addr#$layer == $ca"
    filter="$filter && iec60870_asdu.ioa#$layer == $ioa && iec60870_asdu.dco.se#$layer == $se)"
  done
  printf 'ip.src == %s && ip.dst == %s && tcp.dstport == 2404 && (%s)' "$CC_IP" "$RTU_IP" "$filter"
}
# eu_check_iec104_command PCAP CA IOA SE NAME: at least one frame matches the
# filter. Frame existence avoids parsing tshark's Boolean or aggregated fields.
eu_check_iec104_command() {
  local pcap="$1" ca="$2" ioa="$3" se="$4" name="$5" frames
  if ! frames=$(tshark -r "$pcap" -Y "$(eu_iec104_command_filter "$ca" "$ioa" "$se")" \
    -T fields -e frame.number 2>"$TMP_DIR/tshark-iec104.err"); then
    fail "G3 IEC104 $name decode failed: $(cat "$TMP_DIR/tshark-iec104.err")"
    return 1
  fi
  if [ -z "$frames" ]; then
    fail "G3 capture has no C_DC_NA_1 TypeID 46 $name (COT 6, CA $ca, IOA $ioa) from $CC_IP to $RTU_IP:2404"
    return 1
  fi
  pass "G3 capture decodes C_DC_NA_1 $name (COT 6, CA $ca, IOA $ioa) from $CC_IP to $RTU_IP:2404 in frame(s) $(printf '%s' "$frames" | tr '\n' ' ')"
}
eu_decode_capture() {
  if [ ! -s "$TMP_DIR/eu-iec104-gate.pcap" ]; then
    fail "G3 packet capture is empty"
    return 1
  fi
  pass "G3 packet capture copied and non-empty"
  if tshark -r "$TMP_DIR/eu-iec104-gate.pcap" -Y '_ws.malformed' -T fields -e frame.number \
    >"$TMP_DIR/malformed.txt" 2>"$TMP_DIR/tshark.err"; then
    if [ ! -s "$TMP_DIR/malformed.txt" ]; then
      pass "G3 capture contains no Wireshark-marked malformed frame"
    else
      fail "G3 malformed frames found: $(cat "$TMP_DIR/malformed.txt")"
    fi
  else
    fail "G3 tshark malformed-frame decode failed: $(cat "$TMP_DIR/tshark.err")"
  fi
  local ca ioa
  ca=$(jq -e '.ca | numbers' "$PACKAGE/protocols/points.json" 2>/dev/null) || ca=""
  ioa=$(jq -e '[.commands[] | select(.id == "feeder-breaker-command") | .ioa | numbers]
    | if length == 1 then .[0] else error("one command IOA expected") end' \
    "$PACKAGE/protocols/points.json" 2>/dev/null) || ioa=""
  if [ -z "$ca" ] || [ -z "$ioa" ]; then
    fail "G3 points.json lacks the common address or the feeder-breaker-command IOA"
  else
    eu_check_iec104_command "$TMP_DIR/eu-iec104-gate.pcap" "$ca" "$ioa" 1 select
    eu_check_iec104_command "$TMP_DIR/eu-iec104-gate.pcap" "$ca" "$ioa" 0 execute
  fi
  if tshark -r "$TMP_DIR/eu-iec104-gate.pcap" -Y 'tcp.port == 502 && modbus.func_code == 6' \
    -T fields -e ip.src -e ip.dst -e modbus.func_code >"$TMP_DIR/modbus-decode.txt" \
    2>"$TMP_DIR/tshark-modbus.err"; then
    if awk -F '\t' '
      $1 == "10.30.30.20" && $2 == "10.40.40.20" && $3 == "6" { found = 1 }
      END { exit(found ? 0 : 1) }
    ' "$TMP_DIR/modbus-decode.txt"; then
      pass "G3 capture decodes Modbus FC6 on the RTU/breaker hop"
    else
      fail "G3 capture does not prove Modbus FC6 on the RTU/breaker hop"
    fi
  else
    fail "G3 Modbus tshark decode failed: $(cat "$TMP_DIR/tshark-modbus.err")"
  fi
}

# Captures are attempt-local. Copies are retained with API/audit logs on failure.
eu_check_timed_status() {
  local pcap="$1" position="$2" invalid="$3" layer filter="" frames
  for layer in 1 2 3 4 5 6 7 8 9 10 11 12; do
    filter="$filter${filter:+ || }(iec60870_asdu.typeid#$layer == 31"
    filter="$filter && (iec60870_asdu.causetx#$layer == 3 || iec60870_asdu.causetx#$layer == 11)"
    filter="$filter && iec60870_asdu.addr#$layer == 1 && iec60870_asdu.ioa#$layer == 1001"
    filter="$filter && iec60870_asdu.diq.dpi#$layer == $position && iec60870_asdu.cp56time.iv#$layer == $invalid)"
  done
  if ! frames=$(tshark -r "$pcap" -Y "ip.src == $RTU_IP && ip.dst == $CC_IP && tcp.srcport == 2404 && ($filter)" \
    -T fields -e frame.number 2>"$TMP_DIR/timed-status.err"); then
    fail "G2 timed status decode failed: $(cat "$TMP_DIR/timed-status.err")"; return 1
  fi
  if [ -z "$frames" ]; then
    fail "G2 no timed breaker event position=$position CP56-invalid=$invalid in $pcap"; return 1
  fi
  pass "G2 actual timed breaker event position=$position CP56-invalid=$invalid in frame(s) $(printf '%s' "$frames" | tr '\n' ' ')"
}

eu_window_start() {
  local label="$1" peer="$FW" image platform
  case "$label" in G11|G17) peer="$RTU" ;; esac
  CAPTURE_HOST="rdinc4-i-window-$$"
  CAPTURE_PATH=/tmp/window.pcap CAPTURE_PID=1
  CAPTURE_PID_FILE="" CAPTURE_LOG=""
  # shellcheck disable=SC2034 # read by shared capture teardown
  capture_running=1
  image=$(docker inspect -f '{{.Image}}' "$FW") || return
  platform=$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image") || return
  # Reuse the already-running image, without pulling/tagging it. Sharing the
  # RTU namespace is necessary to observe its same-subnet PLC polls too.
  if ! docker run -d --platform "$platform" --name "$CAPTURE_HOST" --network "container:$peer" \
    --cap-add NET_RAW --entrypoint tcpdump "$image" --immediate-mode -i any -s 0 -U -w "$CAPTURE_PATH" \
    'tcp port 2404 or tcp port 502 or tcp port 8080 or tcp port 8081' >"$TMP_DIR/$label-capture.id"; then
    fail "$label cannot start owned capture container"; return 1
  fi
  sleep 1
}
eu_window_finish() {
  local label="$1"
  eu_stop_capture || return
  docker cp "$CAPTURE_HOST:$CAPTURE_PATH" "$TMP_DIR/$label.pcap" >/dev/null || {
    fail "$label cannot retrieve attempt capture"; return 1;
  }
  docker rm "$CAPTURE_HOST" >/dev/null || return
  CAPTURE_HOST=""
  # shellcheck disable=SC2034 # shared with capture teardown
  CAPTURE_PATH="" CAPTURE_PID_FILE="" CAPTURE_LOG="" CAPTURE_PID=""
}

# Poll requests, not CC/GI refreshes, measure the independent RTU scheduler.
eu_peer_poll_gaps() {
  local pcap="$1" since="$2" until="$3"
  tshark -r "$pcap" -Y 'ip.src == 10.30.30.20 && tcp.dstport == 502 && (modbus.func_code == 3 || modbus.func_code == 4)' \
    -T fields -e frame.time_epoch -e ip.dst -e tcp.seq_raw >"$TMP_DIR/polls.tsv" 2>"$TMP_DIR/polls.err" || {
    fail "G11 poll decode failed: $(cat "$TMP_DIR/polls.err")"; return 1;
  }
  if python3 - "$TMP_DIR/polls.tsv" "$PACKAGE/protocols/points.json" "$since" "$until" <<'PY'
import json, sys
path, points, start, end = sys.argv[1:]
start, end = float(start), float(end)
config = json.load(open(points))
for device in config['devices']:
    if device['id'] == 'feeder-breaker':
        continue
    ip = device['host']
    rows, seen = [], set()
    for line in open(path):
        stamp, dst, seq = line.strip().split('\t')
        stamp = float(stamp)
        if dst == ip and start <= stamp <= end and (dst, seq) not in seen:
            rows.append(stamp)
            seen.add((dst, seq))
    limit = (device['poll_ms'] + 250) / 1000
    gaps = [b-a for a,b in zip([start]+rows, rows+[end])]
    assert len(rows) >= 2 and max(gaps) <= limit, (device['id'], len(rows), max(gaps), limit)
    print(f"{device['id']}: {len(rows)} fresh polls; max gap {max(gaps):.3f}s <= {limit:.3f}s")
PY
  then pass "G11 every healthy peer poll gap <= poll_ms + 250ms during dropped breaker conduit"
  else fail "G11 healthy-peer protocol freshness failed (polls.tsv)"; return 1; fi
}

eu_fresh_points() {
  local predicate="$1" since="$2" budget="$3" label="$4"
  eu_wait_for_operator "def point(\$id): [.points[] | select(.id == \$id and good
    and (.received_at | epoch) >= (\"$since\" | epoch))]
    | if length == 1 then .[0] else error(\"no fresh good observation\") end;
    ($predicate)" "$budget" "$label" || return
  pass "$label: fresh protocol observations"
}

eu_gate_G15() {
  local section expected since
  eu_apply_named improved || return
  eu_reset_all || return
  eu_management rmu-1 POST /api/sim/actions '{"action":"earth_close"}' || return
  if [ "$HTTP_STATUS" = 409 ] && printf '%s' "$API_BODY" | jq -e '.reason | type == "string" and length > 0' >/dev/null; then
    pass "G15 earth_close refused on a closed/energised way: $API_BODY"
  else fail "G15 unsafe earth_close not refused: HTTP $HTTP_STATUS $API_BODY"; return 1; fi
  eu_command rmu-2-command close rejected live_live || return
  eu_action rmu-1 set_local || return
  eu_wait_for_operator 'point("rmu-1-remote").value == false and (point("rmu-1-remote") | good)' 10 "G15 local observed" || return
  eu_command rmu-1-command open rejected not_remote || return
  eu_action rmu-1 set_remote || return
  eu_command rmu-1-command open completed || return
  eu_wait_for_field '.devices["rmu-1"].measurements.valid and .process.buses.s2.live == false' 10 "G15 isolated S2 solve" || return
  eu_action rmu-1 earth_close || return
  eu_command rmu-1-command close rejected earth_closed || return
  eu_action rmu-1 earth_open || return
  eu_command rmu-1-command close completed || return
  for section in s1 s2; do
    eu_reset_all || return
    since=$(eu_utc)
    expected=false; [ "$section" != s2 ] || expected=true
    eu_action process "fault_$section" || return
    eu_wait_for_field ".devices[\"breaker-1\"].protection_tripped and .devices[\"breaker-1\"].lockout
      and .devices[\"rmu-1\"].fault_passage == $expected and .devices[\"rmu-1\"].measurements.valid
      and .process.buses.s2.live == false" 10 "G15 $section trip/FPI and solve" || return
    eu_fresh_points "point(\"feeder-protection-trip\").value and point(\"rmu-1-fault-passage\").value == $expected" \
      "$since" 10 "G15 $section protection/FPI via Modbus/IEC104" || return
    eu_command rmu-1-command open completed || return
    eu_wait_for_field '.devices["rmu-2"].measurements.valid and .devices["rmu-2"].measurements.section_live == false' 10 "G15 tie ready" || return
    since=$(eu_utc)
    eu_command rmu-2-command close completed || return
    if [ "$section" = s1 ]; then
      eu_wait_for_field '.process.buses.s2.live and .process.buses.s3.live and .process.f2_tripped == false' 10 "G15 S1 tie restores S2" || return
      eu_fresh_points 'point("rmu-2-section-kv").value > 18 and point("rmu-2-source-kv").value > 18' "$since" 10 "G15 restored voltage via protocols" || return
      eu_command feeder-breaker-command close rejected lockout || return
      eu_action process clear_fault || return
      eu_action breaker-1 reset_lockout || return
      eu_command rmu-1-command close completed || return
      eu_wait_for_field '.devices["breaker-1"].measurements.valid and .process.buses.s1.live' 10 "G15 reverse feed S1 through tie" || return
      eu_command feeder-breaker-command close rejected live_live || return
    else
      eu_wait_for_field '.process.f2_tripped and .process.buses.s2.live == false and .process.buses.s3.live == false' 10 "G15 S2 backfeed trips F2" || return
      eu_fresh_points 'point("rmu-1-fault-passage").value and point("rmu-2-fault-passage").value
        and point("rmu-2-source-kv").value < 0.1' "$since" 10 "G15 S2 FPI and S3 loss via protocols" || return
    fi
  done
  eu_reset_all
}

# PLC accepted requests must be new, peer-attributed Modbus writes, never just
# a tap that happened to have the desired value before the attempted load change.
eu_plc_requests() {
  local cursor="$1" count="$2" generation="$3"
  eu_management oltc-1 GET "/api/audit?after=$cursor" || return
  if printf '%s' "$API_BODY" | jq -e --argjson cursor "$cursor" --argjson count "$count" --argjson gen "$generation" '
    .generation == $gen and .oldest_seq <= $cursor + 1
    and all(.entries[] | select(.seq > $cursor and .operation == "plc_request"
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.30:"))); .result == "accepted")
    and ([.entries[] | select(.seq > $cursor and .operation == "plc_request" and .result == "accepted"
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.30:")))] | length) == $count' >/dev/null; then
    pass "G16 exactly $count new PLC Modbus request(s) after cursor $cursor"
  else fail "G16 PLC request evidence mismatch: $API_BODY"; return 1; fi
}
eu_gate_G16() {
  local cursor generation since source tap count budget
  eu_reset_all || return
  eu_command oltc-1-mode manual completed || return
  eu_command oltc-1-step raise completed || return
  eu_command oltc-1-step lower completed || return
  eu_action oltc-1 set_local || return
  eu_command oltc-1-mode auto rejected not_remote || return
  eu_action oltc-1 set_remote || return
  eu_action oltc-1 block || return
  eu_command oltc-1-step raise rejected blocked || return
  eu_action oltc-1 unblock || return
  eu_command oltc-1-mode auto completed || return
  for source in load 0.90 1.10; do
    eu_reset_all || return
    eu_wait_for_field '.devices["oltc-1"].armed and .devices["oltc-1"].tap == 0' 10 "G16 armed nominal" || return
    eu_management oltc-1 GET '/api/audit?after=0' || return
    cursor=$(printf '%s' "$API_BODY" | jq -r '.latest_seq // 0')
    generation=$(printf '%s' "$API_BODY" | jq -r '.generation')
    since=$(eu_utc)
    case "$source" in
      load) tap=1; count=1; budget=35; eu_action process set_load 3.0 || return ;;
      0.90) tap=8; count=8; budget=130; eu_action process set_source "$source" || return ;;
      1.10) tap=-7; count=7; budget=120; eu_action process set_source "$source" || return ;;
    esac
    eu_fresh_points "point(\"oltc-1-tap\").value == {value:$tap,transient:false}
      and (point(\"oltc-1-tap\") | good)" "$since" "$budget" "G16 $source reaches tap $tap" || return
    sleep 15
    eu_plc_requests "$cursor" "$count" "$generation" || return
    if [ "$source" = 0.90 ]; then
      eu_fresh_points 'point("oltc-1-at-upper").value and point("oltc-1-bus-kv").value < 19.8' "$since" 10 "G16 upper limit remains undervoltage" || return
      eu_command oltc-1-mode manual completed || return
      eu_command oltc-1-step raise rejected limit || return
      eu_command oltc-1-step lower completed || return
    else
      eu_fresh_points "point(\"oltc-1-tap\").value == {value:$tap,transient:false}
        and point(\"oltc-1-bus-kv\").value >= 19.8 and point(\"oltc-1-bus-kv\").value <= 20.2" \
        "$since" 10 "G16 $source settles in band without extra steps" || return
      if [ "$source" = 1.10 ]; then
        eu_command oltc-1-mode manual completed || return
        eu_command oltc-1-step lower completed || return
        eu_command oltc-1-step lower rejected limit || return
        eu_command oltc-1-step raise completed || return
      fi
    fi
  done
  eu_reset_all
}

eu_stop_plc_scan() {
  [ -n "$PLC_SCAN_HELPER" ] || return 0
  if ! docker stop -t 3 "$PLC_SCAN_HELPER" >/dev/null; then
    fail "G17 cannot stop owned PLC tracer $PLC_SCAN_HELPER"; return 1
  fi
  docker logs "$PLC_SCAN_HELPER" >"$TMP_DIR/G17-scan.log" 2>&1 || return
  docker rm "$PLC_SCAN_HELPER" >/dev/null || return
  PLC_SCAN_HELPER=""
  if grep -q '"event": "scan_frozen"' "$TMP_DIR/G17-scan.log" \
    && ! grep -q '"event": "scan_resumed"' "$TMP_DIR/G17-scan.log"; then
    fail "G17 tracer did not confirm scan resumed: $(cat "$TMP_DIR/G17-scan.log")"; return 1
  fi
}
eu_gate_G17() {
  local image platform attempt frozen_at until
  eu_reset_all || return
  eu_window_start G17 || return
  image=$(docker inspect -f '{{.Image}}' rangerdanger-openplc) || return
  platform=$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image") || return
  PLC_SCAN_HELPER="rdinc4-i-scan-$$"
  if ! docker run -d --platform "$platform" --name "$PLC_SCAN_HELPER" --network none --pid container:rangerdanger-openplc \
    --cap-add SYS_PTRACE --entrypoint python3 "$image" -c "$(cat "$SCRIPT_DIR/tests/eu-gate/freeze-plc-scan.py")" \
    >"$TMP_DIR/G17-tracer.id"; then
    fail "G17 cannot start scan-thread fault injector"; return 1
  fi
  for ((attempt=0; attempt<30; attempt++)); do
    docker exec "$PLC_SCAN_HELPER" test -s /tmp/scan-frozen >/dev/null 2>&1 && break
    sleep 0.1
  done
  if [ "$attempt" -eq 30 ]; then
    docker logs "$PLC_SCAN_HELPER" >"$TMP_DIR/G17-scan.log" 2>&1
    fail "G17 scan freeze unavailable: $(cat "$TMP_DIR/G17-scan.log")"; return 1
  fi
  frozen_at=$(python3 -c 'import time; print(time.time())')
  if eu_wait_for_operator '.link.state == "started"
    and ([.points[] | select(.id | startswith("plc-1-"))] | length == 5
      and all(.[]; .quality.iv and .stale_reason == "heartbeat"))
    and all(.points[] | select(.id | startswith("plc-1-") | not); good)' 5 "G17 responsive PLC heartbeat staleness"; then
    pass "G17 all five PLC points IV heartbeat while every healthy peer remains GOOD"
  fi
  until=$(python3 -c 'import time; print(time.time())')
  eu_window_finish G17 || return
  eu_stop_plc_scan || return
  if tshark -r "$TMP_DIR/G17.pcap" -Y 'ip.src == 10.30.30.30 && tcp.srcport == 502 && modbus.func_code == 3 && modbus.byte_cnt == 12' \
    -T fields -e frame.time_epoch -e modbus.regval_uint16 >"$TMP_DIR/G17-registers.tsv" 2>"$TMP_DIR/G17-decode.err" \
    && python3 - "$TMP_DIR/G17-registers.tsv" "$frozen_at" "$until" <<'PYHEARTBEAT'
import sys
rows = [line.strip().split('\t') for line in open(sys.argv[1])]
values = [v.split(',') for t,v in rows if float(sys.argv[2]) <= float(t) <= float(sys.argv[3])]
assert len(values) >= 3 and all(len(v) == 6 for v in values), values
assert len({v[-1] for v in values}) == 1, values
print(f"{len(values)} real FC3 responses with a constant heartbeat: {values[0][-1]}")
PYHEARTBEAT
  then pass "G17 protocol capture proves responsive PLC FC3 with frozen heartbeat (no replacement server)"
  else fail "G17 no responsive, constant-heartbeat PLC protocol evidence"; fi
  if eu_wait_for_operator 'all(.points[]; good)' 10 "G17 resumed scan heartbeat recovery"; then
    pass "G17 PLC heartbeat observations recover through Modbus/IEC104"
  fi
}

eu_gate_G18() {
  local since paused resumed until epoch
  eu_reset_all || return
  since=$(eu_utc)
  eu_api_get /api/historian/points || return
  eu_assert_jq "$API_BODY" 'length == 31' "G18 historian exposes 31 points"
  eu_get_operator || return
  epoch=$(printf '%s' "$API_BODY" | jq -r '.cache_epoch')
  eu_window_start G18 || return
  cc_paused=1
  docker pause "$CC" >/dev/null || { fail "G18 CC pause failed"; return 1; }
  # One push may already be in flight (contract budget 500ms). Drain it
  # before measuring the genuine ingest-silence window.
  sleep 1
  paused=$(eu_utc)
  sleep 5
  # End the confirmed-paused window before starting unpause: an ingest can
  # legitimately arrive before the Docker CLI has returned.
  resumed=$(eu_utc)
  eu_unpause_owned "$CC" cc "$cc_paused" || return
  sleep 3
  until=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  eu_api_get "/api/historian/history?point=feeder-current&since=$since&until=$until" || return
  if eu_historian_gap "$API_BODY" "$epoch" "$paused" "$resumed"; then
    pass "G18 real CC ingest resumes with schema-2 samples and an outage-spanning gap"
  else fail "G18 historian outage evidence failed ($paused to $resumed): $API_BODY"; fi
  # A valid snapshot on a read listener must not ingest, even from enterprise.
  eu_get_operator || return
  if docker exec "$KALI" curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -X POST \
    -H 'Content-Type: application/json' -d "$API_BODY" http://10.70.70.40:8080/api/ingest >"$TMP_DIR/read-ingest.status"; then
    case "$(cat "$TMP_DIR/read-ingest.status")" in
      404|405) pass "G18 read listener rejects real POST /api/ingest" ;;
      *) fail "G18 read listener accepted ingest: $(cat "$TMP_DIR/read-ingest.status")" ;;
    esac
  else fail "G18 enterprise read listener transport failed"; fi
  eu_window_finish G18 || return
  if tshark -r "$TMP_DIR/G18.pcap" -d tcp.port==8081,http \
    -Y 'ip.src == 10.60.60.20 && ip.dst == 10.70.70.40 && tcp.dstport == 8081 && http.request.method == "POST" && http.request.uri == "/api/ingest"' \
    -T fields -e frame.number >"$TMP_DIR/G18-ingest.frames" 2>"$TMP_DIR/G18-decode.err" && [ -s "$TMP_DIR/G18-ingest.frames" ]; then
    pass "G18 capture proves real CC POST /api/ingest over 8081 in this attempt"
  else fail "G18 no captured CC ingest operation (G18.pcap)"; fi
}

eu_gate_G19() {
  local container ip port rc path method body
  eu_get_operator || return
  body="$API_BODY"
  eu_apply_named improved || return
  eu_window_start G19 || return
  for container in "$KALI" "$RTU" rangerdanger-eng-ws; do
    for ip in 10.70.70.40 10.99.99.23 10.50.50.20; do
      # Only Kali probes solver surfaces; all three untrusted peers probe ingest.
      [ "$ip" = 10.70.70.40 ] || [ "$container" = "$KALI" ] || continue
      port=8080; path=/api/electrical; method=GET
      if [ "$ip" = 10.70.70.40 ]; then port=8081; path=/api/ingest; method=POST; fi
      if [ "$container" = "$RTU" ]; then
        docker exec "$container" wget -T 3 -O - --post-data="$body" "http://$ip:$port$path" \
          >"$TMP_DIR/G19-$container-$ip.out" 2>"$TMP_DIR/G19-$container-$ip.err"
        rc=$?
        if [ "$rc" = 1 ] && grep -Eq 'timed out|Connection refused' "$TMP_DIR/G19-$container-$ip.err"; then
          pass "G19 $container -> $ip:$port denied (wget $rc)"
        else fail "G19 $container -> $ip:$port not proven denied: wget $rc $(cat "$TMP_DIR/G19-$container-$ip.err")"; fi
      else
        docker exec "$container" curl -sS --connect-timeout 2 --max-time 3 -X "$method" \
          -H 'Content-Type: application/json' -d "$body" "http://$ip:$port$path" \
          >"$TMP_DIR/G19-$container-$ip.out" 2>"$TMP_DIR/G19-$container-$ip.err"
        rc=$?
        if [ "$rc" = 28 ] || [ "$rc" = 7 ]; then
          pass "G19 $container -> $ip:$port denied (curl $rc)"
        else fail "G19 $container -> $ip:$port not denied: curl $rc $(cat "$TMP_DIR/G19-$container-$ip.out")"; fi
      fi
    done
  done
  eu_window_finish G19 || return
  if tshark -r "$TMP_DIR/G19.pcap" -Y 'ip.src == 10.10.10.50 && tcp.flags.syn == 1 && tcp.flags.ack == 0' \
    -T fields -e ip.dst -e tcp.dstport >"$TMP_DIR/G19-syn.tsv" 2>"$TMP_DIR/G19-decode.err"; then
    for ip in 10.70.70.40 10.99.99.23 10.50.50.20; do
      port=8080; [ "$ip" != 10.70.70.40 ] || port=8081
      if grep -q "$(printf '%s\t%s' "$ip" "$port")" "$TMP_DIR/G19-syn.tsv"; then
        pass "G19 capture proves fresh Kali SYN to $ip:$port"
      else fail "G19 no fresh Kali SYN evidence to $ip:$port"; fi
    done
  else fail "G19 SYN capture decode failed"; fi
  if tshark -r "$TMP_DIR/G19.pcap" -d tcp.port==8081,http \
    -Y 'ip.src == 10.70.70.40 && ip.dst == 10.60.60.20 && tcp.srcport == 8081 && http.response.code == 200' \
    -T fields -e frame.number >"$TMP_DIR/G19-cc-ingest.frames" 2>"$TMP_DIR/G19-ingest.err" && [ -s "$TMP_DIR/G19-cc-ingest.frames" ]; then
    pass "G19 real CC ingest remains HTTP 200 over 8081 under improved"
  else fail "G19 improved has no successful CC ingest protocol operation"; fi
}

# Use parsed UTC timestamps: lexical comparisons are wrong at fractional-second
# boundaries ("00Z" sorts after "00.5Z").
eu_utc() { python3 -c 'import datetime; print(datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"))'; }
eu_command_result() {
  python3 - "$@" <<'PYRESULT'
import datetime, json, sys
body, point, op, expected, start, end, ioa, target = sys.argv[1:]
def at(value):
    assert isinstance(value, str) and value.endswith('Z'), value
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
try:
    b = json.loads(body)
    assert b['point'] == point and b['operation'] == op and b['result'] == expected
    assert isinstance(b['id'], str) and b['id']
    assert at(start) <= at(b['selected_at']) <= at(b['executed_at']) <= at(b['con_at']) <= at(end)
    if expected == 'completed':
        f = b['feedback']
        assert f['ioa'] == int(ioa) and f['value'] == json.loads(target)
        assert at(b['con_at']) <= at(f['at']) <= at(b['term_at']) <= at(end)
    else:
        assert expected == 'rejected' and b['feedback'] is None and b['term_at'] is None
except (AssertionError, ValueError, KeyError, TypeError) as error:
    print('invalid command evidence:', error, file=sys.stderr)
    sys.exit(1)
PYRESULT
}
eu_wait_valid_device() {
  local node="$1" start now
  start=$(eu_now_ms)
  while :; do
    eu_management "$node" GET /api/state || return
    if [ "$HTTP_STATUS" = 200 ] && printf '%s' "$API_BODY" | jq -e '.schema == 2 and .measurements.valid == true' >/dev/null; then return 0; fi
    now=$(eu_now_ms)
    if [ "$((now - start))" -ge 10000 ]; then fail "$node no valid pre-command solve: $API_BODY"; return 1; fi
    sleep 0.1
  done
}

# New increment-4 services retain the same non-forwarding/loopback boundaries.
eu_inc4_boundaries() {
  local node field process mgmt container forwarding
  for node in 1 2 3; do
    case "$node" in
      1) container=rangerdanger-rmu-1; field=21; process=31; mgmt=24 ;;
      2) container=rangerdanger-rmu-2; field=22; process=32; mgmt=25 ;;
      3) container=rangerdanger-oltc-1; field=23; process=33; mgmt=26 ;;
    esac
    check_listener "$container" "10.40.40.$field:502" "$container Modbus"
    check_listener "$container" "10.50.50.$process:8080" "$container truth" 2
    check_listener "$container" "10.99.99.$mgmt:8080" "$container sim" 2
    check_gateway "$container" 10.40.40.2
  done
  check_listener rangerdanger-historian 10.70.70.40:8081 "historian ingest"
  check_listener rangerdanger-historian 10.70.70.40:8080 "historian DMZ read" 2
  check_listener rangerdanger-historian 10.99.99.40:8080 "historian mgmt read" 2
  check_gateway rangerdanger-historian 10.70.70.2
  check_gateway rangerdanger-openplc 10.30.30.2
  for container in rangerdanger-rmu-1 rangerdanger-rmu-2 rangerdanger-oltc-1 rangerdanger-historian rangerdanger-openplc; do
    forwarding=$(docker exec "$container" cat /proc/sys/net/ipv4/ip_forward) || forwarding=""
    if [ "$forwarding" = 0 ]; then pass "$container ip_forward=0"
    else fail "$container ip_forward=$forwarding (expected 0)"; fi
  done
}

# A failed gate must not leave its owned injector/capture affecting later gates.
# shellcheck disable=SC2154 # ownership flags are initialized by the gate runner
eu_gate_resources_finish() {
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

eu_historian_gap() {
  printf '%s' "$1" | jq -e --arg epoch "$2" --arg pause "$3" --arg resume "$4" "$EU_JQ_DEFS"'
    .point == "feeder-current" and any(.segments[]; .cache_epoch == $epoch and (.samples | length > 0)
      and all(.samples[]; (.snapshot_seq | type == "number") and (.value | type == "number")
        and (.quality.ov | type == "boolean") and (.cc_received_at | type == "string")))
    and all(.segments[].samples[]; (.ingested_at | epoch) < ($pause | epoch) or (.ingested_at | epoch) >= ($resume | epoch))
    and any(.gaps[]; (.from | epoch) <= ($pause | epoch) and (.to | epoch) >= ($resume | epoch))' >/dev/null
}
