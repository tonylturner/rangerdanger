# shellcheck shell=bash
# EU IEC104 gate helpers; the US range library remains unchanged.

# jq definitions shared by every operator-state predicate (eu_wait_for_state,
# eu_assert_jq*): the four monitored points and their per-point quality.
# shellcheck disable=SC2016
EU_JQ_DEFS='
def breaker_points: [.points[] | select(.ioa == 1001 or .ioa == 1002 or .ioa == 2001 or .ioa == 2002)];
def four_points: ([breaker_points[].id] | sort)
  == ["feeder-breaker-position","feeder-current","feeder-protection-trip","feeder-voltage"];
def good: .quality.iv == false and .quality.nt == false and (.stale_reason == null or .stale_reason == "");
def all_good: four_points and all(breaker_points[]; good);
def all_stale($reason): four_points
  and all(breaker_points[]; .quality.iv == true and .quality.nt == true and .stale_reason == $reason);
def point($id): ([.points[] | select(.id == $id)][0] // {});
def epoch: capture("^(?<base>.*:[0-9]{2})(?:\\.(?<frac>[0-9]+))?Z$")
  | ((.base + "Z" | fromdateiso8601) + ("0." + (.frac // "0") | tonumber));
def near($a; $b; $tolerance):
  ($a | type) == "number" and ($b | type) == "number" and (($a - $b) | fabs) <= $tolerance;
'

eu_now_ms() {
  python3 -c 'import time; print(int(time.monotonic() * 1000))'
}

# Every API response is appended to the run's response log, which is kept as
# evidence when the gate fails (eu_save_evidence).
eu_log_response() {
  printf '%s %s %s HTTP %s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" \
    "${HTTP_STATUS:-none}" "$API_BODY" >>"$TMP_DIR/api-responses.log"
}

eu_api_get() {
  local path="$1"
  API_BODY=""
  HTTP_STATUS=$(curl -sS --max-time 2 -o "$TMP_DIR/api-get.json" \
    -w '%{http_code}' "$API$path" 2>"$TMP_DIR/curl.err") || {
    printf 'GET %s failed: %s\n' "$path" "$(cat "$TMP_DIR/curl.err")" >&2
    return 1
  }
  API_BODY=$(cat "$TMP_DIR/api-get.json")
  eu_log_response GET "$path"
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
  eu_log_response POST "$path"
  printf '%s' "$API_BODY" | jq -e 'type == "object" or type == "array"' >/dev/null 2>&1 || {
    printf 'POST %s returned empty or unparsable JSON (HTTP %s): %s\n' \
      "$path" "$HTTP_STATUS" "$API_BODY" >&2
    return 1
  }
  return 0
}

eu_assert_jq() {
  local body="$1" expression="$2" label="$3"
  if printf '%s' "$body" | jq -e "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy: $expression)"
  fi
}
eu_assert_jq_truth() {
  local body="$1" truth="$2" expression="$3" label="$4"
  if printf '%s' "$body" | jq -e --argjson truth "$truth" "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label (response did not satisfy the truth comparison)"
  fi
}
eu_assert_device_state_response() {
  local label="$1"
  if [ "$HTTP_STATUS" = "200" ] \
    && printf '%s' "$API_BODY" | jq -e '
      type == "object"
      and .schema == 2 and .kind == "breaker" and .device_id == "feeder-breaker"
      and (.generation | type == "number" and . >= 0)
      and (.revision | type == "number" and . >= 0)
      and (.position | type == "string" and (. == "open" or . == "closed"))
      and (.remote_enabled | type == "boolean")
      and (.lockout | type == "boolean")
      and (.protection_tripped | type == "boolean")
      and (.event_seq | type == "number" and . >= 0)
      and (.changed_at | type == "string" and length > 0)
      and (.measurements | type == "object")
      and (has("measurements") and
        (.measurements | has("current_a") and has("voltage_kv_ll") and has("valid")
          and has("solver_epoch") and has("solve_seq") and has("solved_at") and has("received_at")
          and has("source_kv_ll") and has("section_live") and has("source_live") and has("fault_current")))
      and (.measurements.current_a == null or (.measurements.current_a | type == "number"))
      and (.measurements.voltage_kv_ll == null or (.measurements.voltage_kv_ll | type == "number"))
      and (.measurements.source_kv_ll == null or (.measurements.source_kv_ll | type == "number"))
      and all(.measurements.section_live, .measurements.source_live, .measurements.fault_current;
        . == null or type == "boolean")
      and (.measurements.valid | type == "boolean")
      and (.measurements.solver_epoch == null or (.measurements.solver_epoch | type == "string"))
      and (.measurements.solve_seq == null or (.measurements.solve_seq | type == "number"))
      and (.measurements.solved_at == null or (.measurements.solved_at | type == "string"))
      and (.measurements.received_at == null or (.measurements.received_at | type == "string"))
  ' >/dev/null 2>&1; then
    pass "$label returned HTTP 200 with the device /api/state shape"
    return 0
  else
    fail "$label was not HTTP 200 with the device /api/state shape: HTTP ${HTTP_STATUS:-missing} $API_BODY"
    return 1
  fi
}
# Resolve authored step IDs from the served curriculum; execution stays index-based.
# Never retry an execution: a second POST is a second physical operation.
eu_execute_step() {
  local step="$1" label="$2" index scenario="iec104-telecontrol-boundary"
  if ! eu_api_get "/api/scenarios/$scenario"; then
    fail "$label cannot read the active scenario"
    return 1
  fi
  index=$(printf '%s' "$API_BODY" | jq -er --arg step "$step" '
    .steps | fromjson | to_entries | map(select(.value.id == $step))
    | if length == 1 and .[0].value.action != null then .[0].key
      else error("step must name exactly one executable action") end') || {
    fail "$label cannot resolve executable step $step"
    return 1
  }
  if ! eu_api_post "/api/scenarios/$scenario/steps/$index/execute" '{}'; then
    fail "$label returned no parseable execution result"
    return 1
  fi
  if [ "$HTTP_STATUS" != "200" ] || ! printf '%s' "$API_BODY" | jq -e '
    .success == true and .action_type != "manual"
    and (.results | type == "array" and length > 0 and all(.[]; .success == true))
  ' >/dev/null 2>&1; then
    fail "$label step execution failed: HTTP $HTTP_STATUS $API_BODY"
    return 1
  fi
  pass "$label executed through scenario step $step"
}

# Preserve the physical readback assertions after a successful student action.
eu_device_step() {
  eu_execute_step "$1" "$2" || return 1
  if ! eu_get_field; then
    fail "$2 field readback unavailable"
    return 1
  fi
  API_BODY=$(printf '%s' "$API_BODY" | jq -c '.devices["breaker-1"]') || return 1
  eu_assert_device_state_response "$2"
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
# Address *:PORT means one wildcard listener (ss prints *, 0.0.0.0 or [::]).
check_listener() {
  local container="$1" address="$2" label="$3" expected_count="${4:-1}" listeners port
  port="${address##*:}"
  listeners=$(docker exec "$container" ss -ltn 2>"$TMP_DIR/ss.err") || {
    fail "$label listener inspection failed (ss -ltn): $(cat "$TMP_DIR/ss.err")"
    return 1
  }
  if printf '%s\n' "$listeners" | awk -v endpoint="$address" -v port="$port" -v expected="$expected_count" '
    $4 == endpoint ||
      (endpoint == "*:" port && ($4 == "0.0.0.0:" port || $4 == "[::]:" port)) { found++ }
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
eu_wait_for_state() {
  local path="$1" expression="$2" budget="$3" label="$4" start_ms="${5:-}" now_ms
  [ -n "$start_ms" ] || start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    if ! eu_api_get "$path"; then
      fail "$label returned no valid HTTP 200 JSON response"
      return 1
    fi
    WAIT_BODY="$API_BODY"
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]]; then
      fail "$label cannot read monotonic clock"
      return 1
    fi
    if [ "$((now_ms - start_ms))" -le "$((budget * 1000))" ] \
      && printf '%s' "$WAIT_BODY" | jq -e "$EU_JQ_DEFS $expression" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.1
  done
  fail "$label did not reach expected state within ${budget}s; last response: $WAIT_BODY"
  return 1
}

eu_assert_operator_shape() {
  local body="$1" label="$2"
  if printf '%s' "$body" | jq -e --slurpfile expected "$PACKAGE/protocols/points.json" '
    type == "object" and .schema == 2
    and (.cache_epoch | type == "string" and length > 0)
    and (.snapshot_seq | type == "number" and . >= 0)
    and (.rendered_at | type == "string" and endswith("Z"))
    and (.link | type == "object" and has("state") and (has("local") | not))
    and (.points | length) == 31
    and ([.points[] | {id,ioa,asdu}] | sort_by(.ioa))
      == ([$expected[0].points[] | {id,ioa,asdu}] | sort_by(.ioa))
    and all(.points[]; (.cot | type == "number") and has("source_time") and has("received_at")
      and (.quality | all(.iv,.nt,.sb,.bl; type == "boolean"))
      and (if .asdu == "M_ME_NC_1" or .asdu == "M_ST_TB_1"
        then (.quality.ov | type == "boolean") else .quality.ov == null end))
    and all(.points[] | select(.asdu == "M_ME_NC_1"); .source_time == null)
    and all(.points[] | select(.asdu == "M_ST_TB_1"); .value == null or
      (.value | (.value | type == "number") and (.transient | type == "boolean")))
  ' >/dev/null 2>&1; then
    return 0
  fi
  fail "$label operator state has an invalid link/point shape, a local endpoint is present, or a float source_time is not null"
  return 1
}

eu_wait_for_operator() {
  if ! eu_wait_for_state "/api/operator/state" "$1" "$2" "$3" "${4:-}"; then
    return 1
  fi
  eu_assert_operator_shape "$WAIT_BODY" "$3"
}
eu_wait_for_field() {
  eu_wait_for_state "/api/field/state" "$1" "$2" "$3"
}
eu_get_field() {
  eu_api_get "/api/field/state"
}
eu_get_operator() {
  if ! eu_api_get "/api/operator/state"; then
    return 1
  fi
  eu_assert_operator_shape "$API_BODY" "GET /api/operator/state"
}
eu_get_point_snapshot() {
  local body="$1"
  printf '%s' "$body" | jq -ce "$EU_JQ_DEFS"'
    if type == "object" and (.points | type == "array")
    and all(.points[]; has("id") and has("value") and has("received_at") and has("source_time"))
    and ([.points[] | select(.id == "feeder-current" or .id == "feeder-voltage")
          | .id] | sort == ["feeder-current", "feeder-voltage"])
    and all(.points[] | select(.id == "feeder-current" or .id == "feeder-voltage");
      .source_time == null)
    then [breaker_points[] | {id, value, received_at, source_time}] | sort_by(.id)
    else error("float point source_time must be null and both float points must exist")
    end
  '
}

# eu_baseline_mismatches OPERATOR FIELD: one line per disagreement between
# the operator view, device truth and solver; no output means a GOOD baseline.
# GI (COT 20) reports are untimed (contract §3.2), so a status point's time tag
# must fit its COT; whether a timed tag is synchronised is G2's check.
eu_baseline_mismatches() {
  printf '%s' "$1" | jq -r --argjson truth "$2" "$EU_JQ_DEFS"'
    def time_fits_cot:
      if .cot == 20 then .source_time == null and .source_time_invalid == null
      else (.source_time | type == "string" and length > 0)
        and (.source_time_invalid | type == "boolean")
      end;
    def pair($what; $operator; $device): "\($what): operator \($operator | tojson), truth \($device | tojson)";
    $truth.devices["breaker-1"] as $device | $truth.process as $solver
    | (if .link.state != "started" then "link state \(.link.state | tojson), expected started" else empty end),
      (if .link.gi.state != "complete" then "GI state \(.link.gi.state | tojson), expected complete" else empty end),
      (if four_points then empty else "operator point ids \([.points[].id] | sort | tojson)" end),
      (.points[] | select(good | not)
        | "\(.id) not GOOD: iv=\(.quality.iv) nt=\(.quality.nt) stale_reason=\(.stale_reason | tojson)"),
      (.points[] | select(.id == "feeder-breaker-position" or .id == "feeder-protection-trip")
        | select(time_fits_cot | not)
        | "\(.id) time tag does not fit COT \(.cot | tojson): source_time=\(.source_time | tojson) source_time_invalid=\(.source_time_invalid | tojson)"),
      (point("feeder-breaker-position").value as $value
        | if $value == $device.position then empty else pair("position"; $value; $device.position) end),
      (point("feeder-protection-trip").value as $value
        | if $value == $device.protection_tripped then empty
          else pair("protection trip"; $value; $device.protection_tripped) end),
      (point("feeder-current").value as $value
        | if near($value; $device.measurements.current_a; 0.5) then empty
          else pair("current A (tolerance 0.5)"; $value; $device.measurements.current_a) end),
      (point("feeder-voltage").value as $value
        | if near($value; $device.measurements.voltage_kv_ll; 0.05) then empty
          else pair("voltage kV (tolerance 0.05)"; $value; $device.measurements.voltage_kv_ll) end),
      (if $solver.devices["feeder-breaker"].delivered == true
        and $solver.devices["feeder-breaker"].solved_revision == $device.revision then empty
        else "solver has not delivered breaker revision \($device.revision)" end),
      (if $solver.truth_stale == false then empty else "solver truth_stale" end)
  '
}

# eu_wait_for_baseline LABEL BUDGET: poll operator and field state until
# eu_baseline_mismatches is empty; on timeout print every mismatch and both
# last snapshots (BASELINE_OPERATOR, BASELINE_TRUTH).
eu_wait_for_baseline() {
  local label="$1" budget="$2" start_ms now_ms mismatches
  start_ms=$(eu_now_ms) || {
    fail "$label cannot establish monotonic timeout"
    return 1
  }
  while :; do
    BASELINE_OPERATOR="" BASELINE_TRUTH=""
    if ! eu_api_get "/api/operator/state" 2>"$TMP_DIR/baseline.err"; then
      mismatches="operator state unavailable: $(cat "$TMP_DIR/baseline.err")"
    else
      BASELINE_OPERATOR="$API_BODY"
      if ! eu_api_get "/api/field/state" 2>"$TMP_DIR/baseline.err"; then
        mismatches="field state unavailable: $(cat "$TMP_DIR/baseline.err")"
      else
        BASELINE_TRUTH="$API_BODY"
        mismatches=$(eu_baseline_mismatches "$BASELINE_OPERATOR" "$BASELINE_TRUTH" 2>&1) \
          || mismatches="cannot compare operator and field state: $mismatches"
      fi
    fi
    if [ -z "$mismatches" ]; then
      eu_assert_operator_shape "$BASELINE_OPERATOR" "$label"
      return
    fi
    now_ms=$(eu_now_ms) || now_ms=""
    if ! [[ "$now_ms" =~ ^[0-9]+$ ]] || [ "$((now_ms - start_ms))" -ge "$((budget * 1000))" ]; then
      break
    fi
    sleep 0.25
  done
  fail "$label did not converge within ${budget}s"
  printf '%s\n' "$mismatches" | sed 's/^/  mismatch: /' >&2
  printf '  operator snapshot: %s\n  field snapshot: %s\n' "${BASELINE_OPERATOR:-none}" "${BASELINE_TRUTH:-none}" >&2
  return 1
}
eu_wait_command() {
  eu_command feeder-breaker-command "$1" "$2"
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

eu_stop_capture() {
  [ "$capture_running" -eq 1 ] || return 0
  if [ -z "$CAPTURE_PID" ] && [ -n "$CAPTURE_PID_FILE" ]; then
    local attempt
    for ((attempt = 0; attempt < 20; attempt++)); do
      CAPTURE_PID=$(docker exec "${CAPTURE_HOST:-$FW}" cat "$CAPTURE_PID_FILE" 2>/dev/null) || CAPTURE_PID=""
      [[ "$CAPTURE_PID" =~ ^[0-9]+$ ]] && break
      sleep 0.1
    done
  fi
  if [ -z "$CAPTURE_PID" ] || ! docker exec "${CAPTURE_HOST:-$FW}" sh -c "kill -INT '$CAPTURE_PID'" \
    >/dev/null 2>"$TMP_DIR/capture-kill.err"; then
    fail "could not stop owned tcpdump capture: $(cat "$TMP_DIR/capture-kill.err")"
    return 1
  fi
  local i
  for ((i = 0; i < 40; i++)); do
    if ! docker exec "${CAPTURE_HOST:-$FW}" sh -c "kill -0 '$CAPTURE_PID'" >/dev/null 2>&1; then
      capture_running=0
      return 0
    fi
    sleep 0.25
  done
  fail "owned tcpdump process $CAPTURE_PID did not stop after SIGINT"
  return 1
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
  if [ "$state_touched" -eq 1 ]; then
    if ! eu_apply_named weak; then
      fail "cleanup could not restore weak policy"
      weak_reset=1
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


# Explicit management operations used only by this destructive acceptance gate.
# Nothing on the CC side is populated from these responses.
eu_management() {
  local node="$1" method="$2" path="$3" body="${4:-}" ip output
  case "$node" in
    breaker-1) ip=10.99.99.22 ;; rmu-1) ip=10.99.99.24 ;;
    rmu-2) ip=10.99.99.25 ;; oltc-1) ip=10.99.99.26 ;;
    process) ip=10.99.99.23 ;; *) fail "unknown management node $node"; return 1 ;;
  esac
  output=$(docker exec "$FW" curl -sS --max-time 5 -w '\n%{http_code}' \
    -X "$method" -H 'Content-Type: application/json' -d "$body" "http://$ip:8080$path") || {
    fail "$node $method $path transport failed"; return 1;
  }
  HTTP_STATUS="${output##*$'\n'}" API_BODY="${output%$'\n'*}"
  eu_log_response "$method" "$node$path"
  printf '%s' "$API_BODY" | jq -e 'type == "object"' >/dev/null || {
    fail "$node $path non-JSON HTTP $HTTP_STATUS: $API_BODY"; return 1;
  }
}
eu_action() {
  local node="$1" action="$2" value="${3:-}" path=/api/sim/actions
  [ "$node" != process ] || path=/api/process/actions
  state_touched=1
  eu_management "$node" POST "$path" "{\"action\":\"$action\"${value:+,\"value\":$value}}" || return
  if [ "$HTTP_STATUS" != 200 ]; then
    fail "$node $action HTTP $HTTP_STATUS: $API_BODY"; return 1
  fi
  pass "$node $action acknowledged"
}
eu_reset_all() {
  local started
  started=$(eu_utc)
  state_touched=1
  eu_api_post /api/workshop/reset '{}' || return
  if [ "$HTTP_STATUS" != 200 ] || ! printf '%s' "$API_BODY" | jq -e \
    '.actions | length >= 7 and all(.[]; .success == true)' >/dev/null; then
    fail "EU reset failed: HTTP $HTTP_STATUS $API_BODY"; return 1
  fi
  eu_wait_for_operator ".link.state == \"started\" and all(.points[];
    good and (.received_at | epoch) >= (\"$started\" | epoch))" 30 "reset fresh CC recovery"
}

# The CC result and the device audit must independently prove this attempt.
# A cached final value, or a prior accepted write, cannot satisfy this predicate.
eu_command() {
  local point="$1" operation="$2" expected="$3" reason="${4:-}" node audit_op cursor generation response started ended feedback_id feedback_ioa target
  case "$point" in
    feeder-breaker-command) node=breaker-1; audit_op="$operation" ;;
    rmu-1-command) node=rmu-1; audit_op="$operation" ;;
    rmu-2-command) node=rmu-2; audit_op="$operation" ;;
    oltc-1-step) node=oltc-1; audit_op=manual_step ;;
    oltc-1-mode) node=oltc-1; audit_op=mode ;;
    *) fail "unknown command $point"; return 1 ;;
  esac
  # Intentional invalid-measurement rejection must not wait the fault away.
  if [ "$reason" != no_measurement ]; then eu_wait_valid_device "$node" || return; fi
  eu_get_operator || return
  feedback_id=$(jq -er --arg point "$point" '.commands[] | select(.id == $point) | .feedback' "$PACKAGE/protocols/points.json") || return
  feedback_ioa=$(printf '%s' "$API_BODY" | jq -er --arg id "$feedback_id" '.points[] | select(.id == $id) | .ioa') || return
  target=$(printf '%s' "$API_BODY" | jq -c --arg id "$feedback_id" --arg op "$operation" '
    if $op == "raise" or $op == "lower" then
      (.points[] | select(.id == $id) | .value.value) + (if $op == "raise" then 1 else -1 end)
      | {value:.,transient:false}
    elif $op == "auto" then true elif $op == "manual" then false
    elif $op == "open" then "open" else "closed" end') || return
  eu_management "$node" GET '/api/audit?after=0' || return
  cursor=$(printf '%s' "$API_BODY" | jq -er '.latest_seq // 0') || return
  generation=$(printf '%s' "$API_BODY" | jq -er '.generation') || return
  started=$(eu_utc)
  eu_api_post /api/operator/commands "{\"point\":\"$point\",\"operation\":\"$operation\"}" || return
  # shellcheck disable=SC2034 # caller checks the original command response
  response="$API_BODY" COMMAND_BODY="$API_BODY"
  ended=$(eu_utc)
  if [ "$HTTP_STATUS" != 200 ] || ! eu_command_result "$response" "$point" "$operation" "$expected" "$started" "$ended" "$feedback_ioa" "$target"; then
    fail "$point $operation expected $expected in attempt window: HTTP $HTTP_STATUS $response"; return 1
  fi
  eu_management "$node" GET "/api/audit?after=$cursor" || return
  if ! printf '%s' "$API_BODY" | jq -e --argjson gen "$generation" --argjson cursor "$cursor" \
    --arg op "$audit_op" --arg expected "$expected" --arg reason "$reason" --arg start "$started" --arg end "$ended" "$EU_JQ_DEFS"'
    .generation == $gen and .oldest_seq <= $cursor + 1
    and ([.entries[] | select(.seq > $cursor and .operation == $op
      and (.at | epoch) >= ($start | epoch) and (.at | epoch) <= ($end | epoch)
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.20:")))] | length == 1
      and all(.[]; .result == (if $expected == "completed" then "accepted" else "rejected" end)
        and ($reason == "" or .detail == $reason)))' >/dev/null; then
    fail "$point $operation lacks one fresh RTU Modbus audit ($reason): $API_BODY"; return 1
  fi
  pass "$point $operation $expected: fresh IEC104 confirmations/feedback and RTU Modbus audit${reason:+ ($reason)}"
}
