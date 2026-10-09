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
