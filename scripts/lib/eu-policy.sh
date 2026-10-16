# shellcheck shell=bash
# Firewall policy apply/readback, nft checks, denial events and scenario validation.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

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
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
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
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
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
  local expected="$1" scenario="${2:-}" body
  if [ -z "$scenario" ]; then
    fail "scenario validator requires an explicit served scenario"
    return 1
  fi
  # Lab 2 performs several real endpoint reads during collection.
  if ! eu_api_get "/api/scenarios/$scenario/validate" 30; then
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
  pass "$scenario validator outcome is $expected with parseable checks"
}
