# shellcheck shell=bash
# Kali IEC104 monitor helpers for the EU smoke gate.

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
  if ! docker exec -d "$KALI" sh -c \
    "(iec104cmd '$RTU_IP' monitor --json --hold 180 --gi-every 2 \
       >'$MONITOR_LOG' 2>'$MONITOR_ERR'; rc=\$?; printf '%s\n' \"\$rc\" >'$MONITOR_RC') & \
     child=\$!; printf '%s\n' \"\$child\" >'$MONITOR_PID'" \
    >/dev/null 2>"$TMP_DIR/monitor-start.err"; then
    fail "could not launch Kali background monitor: $(cat "$TMP_DIR/monitor-start.err")"
    return 1
  fi
  return 0
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
