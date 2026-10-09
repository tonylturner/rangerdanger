# shellcheck shell=bash
#
# GET /api/range helpers for setup.sh and the host scripts. Source this
# file after setting API to the portal base URL (http://localhost:8088).
# The status body is one flat JSON object, so parsing needs only sed:
# no jq or python3 dependency.

# range_field <json> <key>: one top-level string field. Absent, null and
# "" all read as empty; escaped characters stay escaped.
range_field() {
    printf '%s' "$1" | sed -nE 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p'
}

# range_status: the current status body, or "unreachable".
range_status() {
    curl -fsS --max-time 10 "$API/api/range" 2>/dev/null || echo unreachable
}

# range_is_ready <package>: true when the range is that package and ready.
range_is_ready() {
    local status
    status=$(range_status)
    [ "$(range_field "$status" package)" = "$1" ] && [ "$(range_field "$status" phase)" = ready ]
}

# Additive EU IEC104 smoke-gate helpers. US gates keep their existing range API.
eu_api_get() {
  local path="$1"
  API_BODY=""
  HTTP_STATUS=$(curl -sS --max-time 2 -o "$TMP_DIR/api-get.json" \
    -w '%{http_code}' "$API$path" 2>"$TMP_DIR/curl.err") || {
    printf 'GET %s failed: %s\n' "$path" "$(cat "$TMP_DIR/curl.err")" >&2
    return 1
  }
  API_BODY=$(cat "$TMP_DIR/api-get.json")
  if [ "$HTTP_STATUS" != "200" ]; then
    printf 'GET %s returned HTTP %s, expected 200: %s\n' "$path" "$HTTP_STATUS" "$API_BODY" >&2
    return 1
  fi
  printf '%s' "$API_BODY" | jq -e 'type == "object" or type == "array"' >/dev/null 2>&1 || {
    printf 'GET %s returned empty or unparsable JSON: %s\n' "$path" "$API_BODY" >&2
    return 1
  }
  return 0
}

eu_api_post() {
  local path="$1" body="$2"
  API_BODY=""
  HTTP_STATUS=""
  HTTP_STATUS=$(curl -sS --max-time 30 -o "$TMP_DIR/api-response.json" \
    -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
    -d "$body" "$API$path" 2>"$TMP_DIR/curl.err") || {
      printf 'POST %s failed: %s\n' "$path" "$(cat "$TMP_DIR/curl.err")" >&2
      return 1
    }
  API_BODY=$(cat "$TMP_DIR/api-response.json")
  printf '%s' "$API_BODY" | jq -e 'type == "object" or type == "array"' >/dev/null 2>&1 || {
    printf 'POST %s returned empty or unparsable JSON (HTTP %s): %s\n' \
      "$path" "$HTTP_STATUS" "$API_BODY" >&2
    return 1
  }
  return 0
}

eu_assert_jq() {
  local body="$1" expression="$2" label="$3"
  if printf '%s' "$body" | jq -e "$expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy: $expression)"
  fi
}
eu_assert_jq_truth() {
  local body="$1" truth="$2" expression="$3" label="$4"
  if printf '%s' "$body" | jq -e --argjson truth "$truth" "$expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy the truth comparison)"
  fi
}
eu_assert_status_200() {
  local label="$1"
  if [ "$HTTP_STATUS" = "200" ]; then
    pass "$label"
  else
    fail "$label (HTTP ${HTTP_STATUS:-missing}, expected 200): $API_BODY"
  fi
}
eu_check_tools() {
  local tool missing=0
  for tool in curl jq python3 docker tshark; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      fail "required host tool missing: $tool"
      missing=1
    fi
  done
  if ! docker compose version >/dev/null 2>&1; then
    fail "required Docker Compose v2 is unavailable"
    missing=1
  fi
  if [ "$missing" -ne 0 ]; then
    return 1
  fi
  return 0
}

# Check a service's required listening address and count, using ss inside the
# service image. A missing ss utility or an unparsable table fails closed.
check_listener() {
  local container="$1" address="$2" label="$3" expected_count="${4:-1}" listeners port
  port="${address##*:}"
  listeners=$(docker exec "$container" ss -ltn 2>"$TMP_DIR/ss.err") || {
    fail "$label listener inspection failed (ss -ltn): $(cat "$TMP_DIR/ss.err")"
    return 1
  }
  if printf '%s\n' "$listeners" | awk -v endpoint="$address" -v port="$port" -v expected="$expected_count" '
    $4 == endpoint { found++ }
    NR > 1 && substr($4, length($4) - length(port)) == ":" port { count++ }
    END { exit(found == 1 && count == expected ? 0 : 1) }
  '; then
    pass "$label listens on $address with exactly $expected_count listener(s) on port $port"
  else
    fail "$label is not bound exactly as required at $address (expected $expected_count listener(s) on port $port); ss -ltn: $listeners"
  fi
}

# Read exactly one default gateway from Linux's /proc/net/route table.
default_gateway() {
  local container="$1" routes gateway
  routes=$(docker exec "$container" cat /proc/net/route 2>/dev/null) || return 1
  gateway=$(printf '%s\n' "$routes" | awk '
    $2 == "00000000" && $8 == "00000000" { count++; value = $3 }
    END { if (count != 1) exit 1; print value }
  ') || return 1
  case "$gateway" in *[!0123456789abcdefABCDEF]*|'') return 1 ;; esac
  [ "${#gateway}" -eq 8 ] || return 1
  printf '%d.%d.%d.%d' \
    "$((16#${gateway:6:2}))" "$((16#${gateway:4:2}))" \
    "$((16#${gateway:2:2}))" "$((16#${gateway:0:2}))"
}
check_gateway() {
  local container="$1" expected="$2" actual
  actual=$(default_gateway "$container") || actual=""
  if [ "$actual" = "$expected" ]; then
    pass "$container has exactly one default route via $expected"
  else
    fail "$container default route is $actual, expected exactly $expected"
  fi
}
eu_container_running() {
  local container="$1" status
  status=$(docker inspect -f '{{.State.Status}}' "$container" 2>/dev/null) || status=""
  [ "$status" = "running" ]
}
eu_make_firewall_token() {
  FW_TOKEN=$(CONTAIND_JWT_SECRET="$JWT_SECRET" python3 -c '
import base64, hashlib, hmac, json, os, time
enc = lambda b: base64.urlsafe_b64encode(b).rstrip(b"=").decode()
header = enc(b"{\"alg\":\"HS256\",\"typ\":\"JWT\"}")
payload = enc(json.dumps({"sub":"eu-iec104-smoke","role":"admin",
                          "exp":int(time.time()) + 3600},
                         separators=(",", ":")).encode())
message = header + "." + payload
signature = enc(hmac.new(os.environ["CONTAIND_JWT_SECRET"].encode(),
                         message.encode(), hashlib.sha256).digest())
print(message + "." + signature)
') || return 1
  [ -n "$FW_TOKEN" ]
}
eu_firewall_config() {
  local output status
  output=$(docker exec "$FW" curl -sS -w '\n%{http_code}' \
    -H "Authorization: Bearer $FW_TOKEN" \
    http://127.0.0.1:8080/api/v1/config 2>"$TMP_DIR/fw-curl.err") || {
      printf 'containd running config read failed: %s\n' "$(cat "$TMP_DIR/fw-curl.err")" >&2
      return 1
    }
  status="${output##*$'\n'}"
  output="${output%$'\n'*}"
  if [ "$status" != "200" ]; then
    printf 'containd running config returned HTTP %s, expected 200\n' "$status" >&2
    return 1
  fi
  printf '%s' "$output" | jq -e 'type == "object" and (.firewall.rules | type == "array")' \
    >/dev/null 2>&1 || {
      printf 'containd running config was empty or unparsable: %s\n' "$output" >&2
      return 1
    }
  printf '%s' "$output"
}
eu_policy_has_no_ephemeral_allow() {
  local policy="$1"
  jq -e --argjson lo "$EPHEMERAL_LOW" --argjson hi "$EPHEMERAL_HIGH" '
    def overlaps($port; $low; $high):
      if $port == null then true
      elif ($port | type) == "number" then ($port >= $low and $port <= $high)
      else
        ($port | tostring) as $text
        | if ($text | test("^[0-9]+$")) then
            (($text | tonumber) >= $low and ($text | tonumber) <= $high)
          elif ($text | test("^[0-9]+-[0-9]+$")) then
            ($text | split("-") | map(tonumber)) as $range
            | ($range[0] <= $high and $range[1] >= $low)
          else true
          end
      end;
    (.firewall.rules | type == "array")
    and ([.. | objects | select(has("ics"))] | length == 0)
    and ([.firewall.rules[] | select(.action == "ALLOW") as $rule
          | select(($rule.protocols | type) != "array"
                   or ($rule.protocols | length) == 0
                   or any($rule.protocols[];
                     ((.name | type) != "string")
                     or ((.name | ascii_downcase) as $protocol
                         | $protocol == "any"
                           or ((["icmp", "icmpv6"] | index($protocol)) == null
                               and overlaps(.port; $lo; $hi)))))]
        | length == 0)
  ' "$policy" >/dev/null 2>&1
}
eu_nft_has_no_ephemeral_allow() {
  local nft_dump="$1"
  printf '%s\n' "$nft_dump" | python3 -c '
import re, sys
low, high = map(int, sys.argv[1:3])
lines = [line.split("#", 1)[0].strip() for line in sys.stdin]
rules = [line for line in lines if "accept" in line]
if not rules:
    raise SystemExit("no nft accept rules found in effective forward chain")
for line in rules:
    if "ct state" in line and "established" in line and "related" in line:
        continue
    if "!=" in line:
        raise SystemExit("negated nft accept cannot prove absence of ephemeral destination-port access: " + line)
    match = re.search(r"\bdport\b", line)
    if not match:
        raise SystemExit("unbounded nft accept may include ephemeral destination ports: " + line)
    stops = []
    for token in ("counter", "accept"):
        index = line.find(token, match.end())
        if index >= 0:
            stops.append(index)
    expression = line[match.end():min(stops) if stops else len(line)]
    ports = re.findall(r"(?<![0-9])[0-9]+(?:-[0-9]+)?(?![0-9])", expression)
    if not ports:
        raise SystemExit("cannot parse nft destination-port accept: " + line)
    for port in ports:
        bounds = [int(value) for value in port.split("-")]
        start, end = (bounds[0], bounds[-1])
        if start <= high and end >= low:
            raise SystemExit("nft accept includes ephemeral destination port(s): " + line)
' "$EPHEMERAL_LOW" "$EPHEMERAL_HIGH"
}
eu_check_nft_chain() {
  local label="$1" nft_dump
  nft_dump=$(docker exec "$FW" nft list chain inet containd forward 2>"$TMP_DIR/nft.err") || {
    fail "$label effective nft forward chain unavailable: $(cat "$TMP_DIR/nft.err")"
    return 1
  }
  printf '%s\n' "--- effective nft forward chain ($label) ---"
  printf '%s\n' "$nft_dump"
  if eu_nft_has_no_ephemeral_allow "$nft_dump"; then
    pass "$label effective nft forward chain has no ephemeral destination-port allow"
  else
    fail "$label effective nft forward chain cannot prove absence of ephemeral allow"
    return 1
  fi
}
eu_check_policy_and_nft() {
  local label="$1" policy="$2"
  if ! eu_policy_has_no_ephemeral_allow "$policy"; then
    fail "$label policy has ICS keys, unbounded TCP allows, or an ephemeral-port allow"
    return 1
  fi
  pass "$label policy has no ICS keys or ephemeral destination-port allow"
  eu_check_nft_chain "$label"
}
eu_apply_named() {
  local name="$1" response active
  state_touched=1
  if ! eu_api_post "/api/firewall/apply" "{\"config\":\"$name\"}"; then
    fail "apply named $name failed"
    return 1
  fi
  response="$API_BODY"
  if [ "$HTTP_STATUS" != "200" ]; then
    fail "apply named $name returned HTTP $HTTP_STATUS: $response"
    return 1
  fi
  if ! printf '%s' "$response" | jq -e '(.warnings // []) == []' >/dev/null 2>&1; then
    fail "apply named $name reported policy import warnings: $response"
    return 1
  fi
  if ! eu_api_get "/api/firewall/active"; then
    fail "cannot read active policy after applying $name"
    return 1
  fi
  active="$API_BODY"
  if ! printf '%s' "$active" | jq -e --arg name "$name" '.active_config == $name' >/dev/null 2>&1; then
    fail "active policy readback is not $name: $active"
    return 1
  fi
  pass "$name applied with zero warnings and exact active-policy readback"
  return 0
}
eu_apply_custom() {
  local policy="$1" label="$2" response active effective expected_file actual_file
  state_touched=1
  if ! eu_api_post "/api/firewall/apply-custom" "$(cat "$policy")"; then
    fail "apply custom $label failed"
    return 1
  fi
  response="$API_BODY"
  if [ "$HTTP_STATUS" != "200" ]; then
    fail "apply custom $label returned HTTP $HTTP_STATUS: $response"
    return 1
  fi
  if ! printf '%s' "$response" | jq -e '(.warnings // []) == []' >/dev/null 2>&1; then
    fail "apply custom $label reported policy import warnings: $response"
    return 1
  fi
  if ! eu_api_get "/api/firewall/active"; then
    fail "cannot read active custom policy $label"
    return 1
  fi
  active="$API_BODY"
  if ! printf '%s' "$active" | jq -e '.active_config == "custom"' >/dev/null 2>&1; then
    fail "active policy readback is not custom after $label: $active"
    return 1
  fi
  effective=$(eu_firewall_config) || {
    fail "containd running-config readback failed after $label"
    return 1
  }
  expected_file="$TMP_DIR/expected-rules.json"
  actual_file="$TMP_DIR/actual-rules.json"
  jq -S '.firewall.rules' "$policy" >"$expected_file" || {
    fail "cannot parse expected rules for $label"
    return 1
  }
  printf '%s' "$effective" | jq -S '.firewall.rules' >"$actual_file" || {
    fail "containd readback rules are unparsable after $label"
    return 1
  }
  if ! cmp -s "$expected_file" "$actual_file"; then
    fail "containd effective firewall rules differ from custom $label before session kill"
    return 1
  fi
  pass "$label custom policy applied without warnings and effective rules read back exactly"
  return 0
}
eu_make_custom_policy() {
  local source_file="$1" src="$2" dst="$3" port="$4" output="$5"
  jq --arg src "$src" --arg dst "$dst" --arg port "$port" '
    def target($source; $destination; $destination_port):
      .action == "ALLOW"
      and any((.sources // [])[]; . == $source or . == ($source + "/32"))
      and any((.destinations // [])[]; . == $destination or . == ($destination + "/32"))
      and any(.protocols[]?;
        .name == "tcp" and ((.port | tostring) == $destination_port));
    [.firewall.rules[] | select(target($src; $dst; $port))] as $matches
    | if ($matches | length) != 1 then
        error("expected exactly one pinned allow, found \(($matches | length))")
      else
        .firewall.rules |= map(select(target($src; $dst; $port) | not))
      end
  ' "$source_file" >"$output" 2>"$TMP_DIR/jq.err" || {
    fail "cannot derive custom policy removing exactly $src -> $dst tcp/$port: $(cat "$TMP_DIR/jq.err")"
    return 1
  }
  jq -e '.firewall.rules | type == "array" and length > 0' "$output" >/dev/null 2>&1 || {
    fail "derived custom policy is empty or invalid: $output"
    return 1
  }
}
eu_read_sessions() {
  local dport="$1"
  if ! eu_api_get "/api/firewall/sessions?dport=$dport"; then
    fail "firewall session list for dport $dport is unavailable or unparsable"
    return 1
  fi
  SESSION_BODY="$API_BODY"
  if ! printf '%s' "$SESSION_BODY" | jq -e '
    type == "array"
    and all(.[]; (.proto | type) == "string"
            and (.src | type) == "string"
            and (.dst | type) == "string"
            and (.sport | type) == "number"
            and (.dport | type) == "number")
    and length > 0
  ' >/dev/null 2>&1; then
    fail "firewall session list is empty or violates the array/integer-port contract (dport $dport): $SESSION_BODY"
    return 1
  fi
  return 0
}
eu_session_tuple() {
  local body="$1" src="$2" dst="$3" sport="$4" dport="$5"
  local matches
  matches=$(printf '%s' "$body" | jq -c --arg src "$src" --arg dst "$dst" \
    --argjson sport "$sport" --argjson dport "$dport" '
      [.[] | select((.proto | ascii_downcase) == "tcp"
        and .src == $src and .dst == $dst
        and .sport == $sport and .dport == $dport)]
    ') || return 1
  printf '%s' "$matches"
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
  if ! CLI_LINES=$(eu_jsonl_count "$output" 2>"$TMP_DIR/jsonl.err"); then
    fail "$label iec104cmd output was empty or unparsable: $(cat "$TMP_DIR/jsonl.err")"
    return 1
  fi
  if [ "$CLI_STATUS" -ne "$status" ]; then
    fail "$label iec104cmd exited $CLI_STATUS, expected $status; stderr: $(cat "$stderr_file")"
    return 1
  fi
  pass "$label iec104cmd exit $status with $CLI_LINES parseable JSON object(s)"
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
      (.at | type == "string") and (.local | type == "string") and (.peer | type == "string")
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
      monitor_running=0
      pass "background monitor exited $expected with $MONITOR_LINES parseable JSON objects"
      return 0
    fi
    sleep 0.25
  done
  fail "background monitor did not exit within ${timeout}s"
  return 1
}

eu_wait_for_state() {
  local path="$1" expression="$2" budget="$3" label="$4" start_ms="${5:-}" now_ms
  [ -n "$start_ms" ] || start_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    if ! eu_api_get "$path"; then
      fail "$label returned no valid HTTP 200 JSON response"
      return 1
    fi
    WAIT_BODY="$API_BODY"
    now_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]]; then
      fail "$label cannot read monotonic clock"
      return 1
    fi
    if [ "$((now_ms - start_ms))" -le "$((budget * 1000))" ] \
      && printf '%s' "$WAIT_BODY" | jq -e "$expression" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.1
  done
  fail "$label did not reach expected state within ${budget}s"
  return 1
}

eu_wait_for_operator() {
  eu_wait_for_state "/api/operator/state" "$1" "$2" "$3" "${4:-}"
}
eu_wait_for_instructor() {
  eu_wait_for_state "/api/instructor/state" "$1" "$2" "$3"
}
eu_get_instructor() {
  eu_api_get "/api/instructor/state"
}
eu_get_operator() {
  eu_api_get "/api/operator/state"
}
eu_get_point_snapshot() {
  local body="$1"
  printf '%s' "$body" | jq -c '
    [.points[] | {id, value, received_at, source_time}] | sort_by(.id)
  '
}
eu_wait_command() {
  local operation="$1" expected="$2" response
  if ! eu_api_post "/api/operator/commands" \
    "{\"point\":\"feeder-breaker-command\",\"operation\":\"$operation\"}"; then
    fail "operator $operation command returned no parseable JSON response"
    return 1
  fi
  response="$API_BODY"
  if [ "$HTTP_STATUS" != "200" ]; then
    fail "operator $operation command returned HTTP $HTTP_STATUS: $response"
    return 1
  fi
  if ! printf '%s' "$response" | jq -e --arg expected "$expected" '
    .status == $expected
    and (if $expected == "completed" then
      .select.status == "confirmed" and .execute.status == "confirmed"
      and .execute.terminated_at != null and .feedback.status == "matched"
      and (.feedback.received_at | type == "string" and length > 0)
    elif $expected == "rejected" then
      .select.status == "negative" or .execute.status == "negative"
    else false end)
  ' >/dev/null 2>&1; then
    fail "operator $operation result did not prove expected $expected command semantics: $response"
    return 1
  fi
  pass "operator $operation result is $expected"
  # shellcheck disable=SC2034
  COMMAND_BODY="$response"
  return 0
}
eu_fresh_deny_event() {
  local source_ip="$1" started_epoch="$2" budget="$3" events count i status
  for ((i = 0; i < budget; i++)); do
    events=$(docker exec "$FW" curl -sS -w '\n%{http_code}' \
      -H "Authorization: Bearer $FW_TOKEN" \
      'http://127.0.0.1:8080/api/v1/events?limit=500' 2>"$TMP_DIR/events.err") || {
        fail "containd event response failed: $(cat "$TMP_DIR/events.err")"
        return 1
      }
    status="${events##*$'\n'}"
    events="${events%$'\n'*}"
    if [ "$status" != "200" ]; then
      fail "containd events returned HTTP $status, expected 200"
      return 1
    fi
    printf '%s' "$events" | jq -e '
      type == "array" or (type == "object" and (.events | type == "array"))
    ' >/dev/null 2>&1 || {
      fail "containd events response was empty or unparsable: $events"
      return 1
    }
    count=$(printf '%s' "$events" | python3 -c '
import calendar, datetime, json, re, sys
data = json.load(sys.stdin)
if isinstance(data, dict):
    data = data.get("events")
if not isinstance(data, list):
    raise SystemExit("event list is not an array")
started = int(sys.argv[1])
source = sys.argv[2]
def epoch_ns(value):
    if not isinstance(value, str):
        raise ValueError("event timestamp is missing or not a string")
    match = re.fullmatch(r"(.*T\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)", value)
    if not match:
        raise ValueError("event timestamp is not RFC3339Nano")
    parsed = datetime.datetime.fromisoformat(match.group(1) + match.group(3).replace("Z", "+00:00"))
    seconds = calendar.timegm(parsed.astimezone(datetime.timezone.utc).utctimetuple())
    fraction = (match.group(2) or "")[:9].ljust(9, "0")
    return seconds * 1_000_000_000 + int(fraction)
hits = []
for event in data:
    if not isinstance(event, dict):
        raise SystemExit("event is not an object")
    attrs = event.get("attributes") or {}
    if not isinstance(attrs, dict):
        raise SystemExit("event attributes are not an object")
    if event.get("kind") != "firewall.rule.hit":
        continue
    if str(attrs.get("action", "")).upper() != "DENY":
        continue
    if event.get("dstIp", attrs.get("dstIp")) != "10.30.30.20" \
            or str(event.get("dstPort", attrs.get("dstPort"))) != "2404":
        continue
    if event.get("srcIp", attrs.get("srcIp")) != source:
        continue
    if epoch_ns(event.get("timestamp")) >= started:
        hits.append(event)
print(len(hits))
' "$started_epoch" "$source_ip" 2>"$TMP_DIR/event-parse.err") || {
      fail "containd event list contained an unparsable response: $(cat "$TMP_DIR/event-parse.err")"
      return 1
    }
    if [ "$count" -gt 0 ]; then
      pass "fresh firewall.rule.hit DENY from $source_ip to $RTU_IP:2404 found"
      return 0
    fi
    sleep 1
  done
  fail "no fresh firewall.rule.hit DENY from $source_ip to $RTU_IP:2404 within ${budget}s"
  return 1
}

eu_validate_scenario() {
  local expected="$1" body
  if ! eu_api_get "/api/scenarios/$SCENARIO_ID/validate"; then
    fail "scenario validator returned no parseable HTTP 200 response"
    return 1
  fi
  body="$API_BODY"
  if ! printf '%s' "$body" | jq -e --arg expected "$expected" \
    '.outcome == $expected and (.checks | type == "array" and length > 0)' \
    >/dev/null 2>&1; then
    fail "scenario validator outcome did not equal $expected: $body"
    return 1
  fi
  if [ "$expected" = "PASS" ] && ! printf '%s' "$body" \
    | jq -e 'all(.checks[]; .status == "pass")' >/dev/null 2>&1; then
    fail "scenario validator outcome PASS contained a non-pass check: $body"
    return 1
  fi
  if [ "$expected" = "FAIL" ] && ! printf '%s' "$body" \
    | jq -e 'any(.checks[]; .status == "fail")' >/dev/null 2>&1; then
    fail "scenario validator outcome FAIL contained no failed evidence check: $body"
    return 1
  fi
  pass "scenario validator outcome is $expected with parseable checks"
}

eu_unpause_owned() {
  local container="$1" flag_name="$2" was_paused="$3" paused
  [ "$was_paused" -eq 1 ] || return 0
  paused=$(docker inspect -f '{{.State.Paused}}' "$container" 2>/dev/null) || paused=""
  if [ "$paused" = "false" ]; then
    case "$flag_name" in
      gps) gps_paused=0 ;;
      opendss) opendss_paused=0 ;;
      rtu) rtu_paused=0 ;;
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
  esac
  pass "$container unpaused"
}

eu_stop_capture() {
  [ "$capture_running" -eq 1 ] || return 0
  if [ -z "$CAPTURE_PID" ] && [ -n "$CAPTURE_PID_FILE" ]; then
    local attempt
    for ((attempt = 0; attempt < 20; attempt++)); do
      CAPTURE_PID=$(docker exec "$FW" cat "$CAPTURE_PID_FILE" 2>/dev/null) || CAPTURE_PID=""
      [[ "$CAPTURE_PID" =~ ^[0-9]+$ ]] && break
      sleep 0.1
    done
  fi
  if [ -z "$CAPTURE_PID" ] || ! docker exec "$FW" sh -c "kill -INT '$CAPTURE_PID'" \
    >/dev/null 2>"$TMP_DIR/capture-kill.err"; then
    fail "could not stop owned tcpdump capture: $(cat "$TMP_DIR/capture-kill.err")"
    return 1
  fi
  local i
  for ((i = 0; i < 40; i++)); do
    if ! docker exec "$FW" sh -c "kill -0 '$CAPTURE_PID'" >/dev/null 2>&1; then
      capture_running=0
      return 0
    fi
    sleep 0.25
  done
  fail "owned tcpdump process $CAPTURE_PID did not stop after SIGINT"
  return 1
}

# shellcheck disable=SC2329
eu_stop_monitor() {
  [ "$monitor_running" -eq 1 ] || return 0
  if docker exec "$KALI" sh -c "test -s '$MONITOR_RC'" >/dev/null 2>&1; then
    monitor_running=0
    return 0
  fi
  local pid
  local attempt
  for ((attempt = 0; attempt < 20; attempt++)); do
    pid=$(docker exec "$KALI" cat "$MONITOR_PID" 2>"$TMP_DIR/monitor-pid.err") || pid=""
    [[ "$pid" =~ ^[0-9]+$ ]] && break
    sleep 0.1
  done
  if ! [[ "$pid" =~ ^[0-9]+$ ]] || ! docker exec "$KALI" kill -TERM "$pid" \
    >/dev/null 2>"$TMP_DIR/monitor-kill.err"; then
    fail "could not terminate owned background monitor (PID $pid): $(cat "$TMP_DIR/monitor-kill.err")"
    return 1
  fi
  monitor_running=0
  pass "owned background monitor stopped during cleanup"
}

# shellcheck disable=SC2329
eu_cleanup() {
  local exit_status="$1" weak_reset=0
  [ "$cleanup_running" -eq 0 ] || return
  cleanup_running=1
  trap - EXIT
  eu_stop_capture || true
  eu_stop_monitor || true
  eu_unpause_owned "$GPS" gps "$gps_paused" || true
  eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || true
  eu_unpause_owned "$RTU" rtu "$rtu_paused" || true
  if [ -n "$MONITOR_LOG" ] && ! docker exec "$KALI" rm -f \
    "$MONITOR_LOG" "$MONITOR_ERR" "$MONITOR_RC" "$MONITOR_PID" \
    >/dev/null 2>"$TMP_DIR/monitor-rm.err"; then
    fail "cleanup could not remove Kali monitor logs: $(cat "$TMP_DIR/monitor-rm.err")"
  fi
  if [ -n "$CAPTURE_PATH" ] && ! docker exec "$FW" rm -f \
    "$CAPTURE_PATH" "$CAPTURE_PID_FILE" "$CAPTURE_LOG" \
    >/dev/null 2>"$TMP_DIR/capture-rm.err"; then
    fail "cleanup could not remove firewall capture files: $(cat "$TMP_DIR/capture-rm.err")"
  fi
  if [ "$state_touched" -eq 1 ]; then
    if eu_apply_named weak; then
      :
    else
      fail "cleanup could not restore weak policy"
      weak_reset=1
    fi
    if eu_api_post "/api/instructor/reset" '{}'; then
      if [ "$HTTP_STATUS" = "200" ] \
        && printf '%s' "$API_BODY" | jq -e 'type == "object"' >/dev/null 2>&1; then
        pass "cleanup instructor reset completed"
      else
        fail "cleanup instructor reset was not a parseable HTTP 200: HTTP $HTTP_STATUS $API_BODY"
        weak_reset=1
      fi
    else
      fail "cleanup instructor reset returned no parseable response"
      weak_reset=1
    fi
    if [ "$weak_reset" -eq 0 ] && eu_get_instructor; then
      if printf '%s' "$API_BODY" | jq -e \
        '.device.position == "closed" and .device.lockout == false and .device.fault_active == false and .device.protection_tripped == false' \
        >/dev/null 2>&1; then
        pass "cleanup instructor reset readback is the initial breaker truth"
      else
        fail "cleanup instructor reset did not restore initial truth: $API_BODY"
        weak_reset=1
      fi
    elif [ "$weak_reset" -eq 0 ]; then
      fail "cleanup could not read back instructor reset truth"
      weak_reset=1
    fi
    if [ "$weak_reset" -eq 0 ]; then
      pass "cleanup ended on weak policy + instructor reset"
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
  if tshark -r "$TMP_DIR/eu-iec104-gate.pcap" -Y 'tcp.port == 2404' -T fields \
    -e ip.src -e ip.dst -e iec60870_asdu.typeid -e iec60870_asdu.dco.se \
    >"$TMP_DIR/iec104-decode.txt" 2>"$TMP_DIR/tshark-iec104.err"; then
    if awk -F '\t' '
      $1 == "10.60.60.20" && $2 == "10.30.30.20" && $3 == "46" {
        if ($4 == "1") select = 1
        if ($4 == "0") execute = 1
      }
      END { exit(select && execute ? 0 : 1) }
    ' "$TMP_DIR/iec104-decode.txt"; then
      pass "G3 capture decodes C_DC_NA_1 select and execute on the CC/RTU hop"
    else
      fail "G3 capture does not prove C_DC_NA_1 TypeID 46 select and execute on the CC/RTU hop"
    fi
  else
    fail "G3 IEC104 tshark decode failed: $(cat "$TMP_DIR/tshark-iec104.err")"
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
