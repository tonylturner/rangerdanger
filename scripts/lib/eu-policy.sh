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
eu_wait_protection() {
  [ -n "${FW_TOKEN:-}" ] || eu_make_firewall_token || return 1
  printf '%s' "$1" | python3 "$SCRIPT_DIR/lib/containd_readiness.py" \
    --container "$FW" --token "$FW_TOKEN" || {
    fail "containd commit did not become protecting"
    return 1
  }
  # Readiness, not commit acknowledgement, starts loss-detection budgets.
  # shellcheck disable=SC2034 # consumed by the delivery-loss gates
  EU_READY_MS=$(eu_now_ms) || return
  # shellcheck disable=SC2034 # consumed by the monitor ASDU oracle
  EU_READY_EPOCH=$(eu_epoch_ns) || return
}
eu_revision_header() {
  awk 'tolower($0) ~ /^x-containd-revision:/ {
    sub(/^[^:]*:[[:space:]]*/, ""); sub(/\r$/, ""); print
  }' "$1"
}
eu_firewall_config() {
  local output status
  output=$(docker exec "$FW" curl -sS --max-time 15 -D /dev/stderr -w '\n%{http_code}' \
    -H "Authorization: Bearer $FW_TOKEN" \
    http://127.0.0.1:8080/api/v1/config 2>"$TMP_DIR/fw-config.headers") || {
      printf 'containd running config read failed: %s\n' "$(cat "$TMP_DIR/fw-config.headers")" >&2
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
    and (. as $policy | all(paths | select(.[-1] == "ics");
      . as $p | ($p | length) == 4 and $p[0:2] == ["firewall","rules"]
      and ($p[2] | type) == "number"
      and ($policy | getpath($p) | type == "object" and .protocol == "iec104" and .mode == "monitor")))
    and ([.firewall.rules[] | select(.action == "ALLOW" and (.ics // {}) == {}) as $rule
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
import ipaddress, re, sys
low, high = map(int, sys.argv[1:3])

def refuse(message):
    raise SystemExit(message)

# This proves absence of new ephemeral-service grants, not absence of reply
# packets addressed to client ephemeral ports. Established/related replies have
# always been exempt here. Track service ports through the ordered dispatcher;
# ct reply proto-src is the original-direction service destination after DNAT.
chains = {}
block = None
closed = False
lines = [line.strip() for line in sys.stdin if line.strip()]
if not lines or lines.pop(0) != "table inet containd_policy {":
    refuse("expected complete inet containd_policy table")
for line in lines:
    if closed:
        refuse("unexpected content after nft table")
    if line == "}":
        if block is None:
            closed = True
        else:
            block = None
        continue
    if block is None:
        match = re.fullmatch(r"(chain|counter|set) ([a-zA-Z0-9_]+) \{", line)
        if not match:
            refuse("unsupported nft table shape: " + line)
        kind, name = match.groups()
        block = (kind, name)
        if kind == "chain":
            if name in chains:
                refuse("duplicate nft chain: " + name)
            chains[name] = []
    elif block[0] == "chain":
        chains[block[1]].append(line)
if not closed or block is not None or "forward" not in chains:
    refuse("incomplete nft table or missing forward chain")

number = r"(?:0x[0-9a-fA-F]+|[0-9]+)"
quoted = r"\"[^\"\\]*\""
port_atom = r"[0-9]+(?:-[0-9]+)?"
port_value = rf"(?:{port_atom}|\{{\s*{port_atom}(?:\s*,\s*{port_atom})*\s*\}})"
state_value = r"(?:[a-z]+|\{[a-z, ]+\})"
address_value = r"(?:[0-9a-fA-F:./-]+|\{[0-9a-fA-F:./, -]+\})"
iface_value = rf"(?:{quoted}|\{{\s*{quoted}(?:\s*,\s*{quoted})*\s*\}})"
all_states = frozenset(("new", "established", "related", "invalid", "untracked"))
reply_states = frozenset(("established", "related"))

def port_set(value):
    ports = set()
    for atom in re.findall(port_atom, value):
        bounds = list(map(int, atom.split("-")))
        lo, hi = bounds[0], bounds[-1]
        if not 0 <= lo <= hi <= 65535:
            refuse("invalid nft port range: " + value)
        ports.update(range(max(low, lo), min(high, hi) + 1))
    return ports

def parse(line):
    rest, conditions, verdict, conditional = line, [], None, False
    while rest:
        def take(pattern):
            return re.match("(?:" + pattern + r")(?= |$)", rest)
        if (m := take(r"ct state (!= )?(" + state_value + ")")):
            states = frozenset(re.findall(r"[a-z]+", m[2]))
            if not states or not states <= all_states:
                refuse("unknown nft connection state: " + line)
            conditions.append(("state", all_states - states if m[1] else states))
            conditional = True
        elif (m := take(r"ct direction (original|reply)")):
            if m[1] == "reply":
                conditions.append(("state", reply_states))
            conditional = True
        elif (m := take(r"ct status assured")):
            conditional = True
        elif (m := take(r"(?:tcp dport|udp dport|ct reply proto-src) (" + port_value + ")")):
            conditions.append(("port", port_set(m[1])))
            conditional = True
        elif (m := take(r"(?:ct (?:original|reply) )?ip6? (?:saddr|daddr) (" + address_value + ")")):
            for address in re.findall(r"[0-9a-fA-F:./-]+", m[1]):
                try:
                    for endpoint in address.split("-"):
                        ipaddress.ip_network(endpoint, strict=False)
                except ValueError:
                    refuse("unknown nft address expression: " + line)
            conditional = True
        elif (m := take(r"(?:iifname|oifname) " + iface_value)):
            conditional = True
        elif (m := take(r"meta l4proto (?:tcp|udp|icmp|ipv6-icmp)")):
            conditional = True
        elif (m := take(r"(?:ct|meta) mark & " + number + r" (?:==|!=) " + number)):
            # Ignoring mark predicates over-approximates possible paths. A mark
            # alone never proves a bounded service; all such accepts still fail.
            conditional = True
        elif (m := take(r"(?:ct mark set ct mark|meta mark set meta mark)(?: [|&] " + number + ")+")):
            pass
        elif (m := take(r"counter(?: name " + quoted + r"| packets [0-9]+ bytes [0-9]+)?")):
            pass
        elif (m := take(r"log prefix " + quoted + r" group [0-9]+")):
            pass
        elif (m := take(r"queue to [0-9]+")):
            # Monitor queues can drop or continue. Treat continuation as possible.
            pass
        elif (m := take(r"(jump|goto) ([a-zA-Z0-9_]+)$")):
            verdict = (m[1], m[2])
        elif (m := take(r"(accept|drop|return|reject(?: with (?:tcp reset|icmpx admin-prohibited))?)$")):
            verdict = (m[1], None)
        else:
            refuse("unsupported nft rule shape: " + line)
        rest = rest[m.end():].lstrip()
    return conditions, verdict, conditional

parsed = {}
visiting, visited = set(), set()
accepts = 0

def walk(name, states, ports):
    global accepts
    if name not in chains:
        refuse("missing nft jump/goto target: " + name)
    if name in visiting:
        refuse("cyclic nft jump/goto: " + name)
    key = (name, states, ports)
    if key in visited:
        return
    visiting.add(name)
    if name not in parsed:
        rules = []
        for line in chains[name]:
            if line == "type filter hook forward priority filter; policy drop;" and name == "forward":
                continue
            rules.append((line, *parse(line)))
        parsed[name] = rules
    for line, conditions, verdict, conditional in parsed[name]:
        matched_states, matched_ports = states, ports
        for kind, values in conditions:
            if kind == "state":
                matched_states = matched_states & values
            else:
                matched_ports = matched_ports & values
        if verdict:
            action, target = verdict
            if action in ("jump", "goto"):
                # Validate targets even for a statically impossible state scope.
                walk(target, matched_states, matched_ports)
            elif action == "accept":
                accepts += 1
                if matched_states - reply_states and matched_ports:
                    refuse("nft accept includes ephemeral destination port(s): " + line)
            if not conditional and action != "jump":
                break
        # Unmatched conditional paths and jump returns continue conservatively.
        # Never infer safety from a preceding conditional deny or mutable mark.
    visiting.remove(name)
    visited.add(key)

if not chains["forward"] or chains["forward"][0] != "type filter hook forward priority filter; policy drop;":
    refuse("forward base chain must have policy drop")
walk("forward", all_states, frozenset(range(low, high + 1)))
if not accepts:
    refuse("no nft accept rules found reachable from forward")

' "$EPHEMERAL_LOW" "$EPHEMERAL_HIGH"
}
eu_check_nft_chain() {
  local label="$1" nft_dump
  # Read the whole policy table: forward dispatches to ordered/admission chains.
  # The pre-filter table is not the final policy boundary.
  nft_dump=$(docker exec "$FW" nft list table inet containd_policy 2>"$TMP_DIR/nft.err") || {
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
    fail "$label policy has invalid/non-monitor IEC104 ICS, unbounded TCP allows, or an ephemeral-port allow"
    return 1
  fi
  pass "$label policy has only explicit-monitor IEC104 ICS and no ephemeral destination-port allow"
  eu_check_nft_chain "$label"
}
eu_apply_named() {
  local name="$1" response active source
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
  eu_wait_protection "$response" || return 1
  if ! eu_api_get "/api/firewall/active"; then
    fail "cannot read active policy after applying $name"
    return 1
  fi
  active="$API_BODY"
  source="$name"
  [ "$name" != improved ] || source=hardened-reference
  if ! printf '%s' "$active" | jq -e --arg name "$name" --arg source "$source" \
    '.active_config == $name and .policy_source == $source and (has("plan") | not)' >/dev/null 2>&1; then
    fail "active policy readback is not $name/$source without plan provenance: $active"
    return 1
  fi
  pass "$name applied with zero warnings and exact active-policy readback"
  return 0
}
eu_normalize_firewall_rules() {
  # Only documented semantic no-ops are normalized. In particular negative:false
  # and test:false restrict IEC104, and must never be erased as echoed defaults.
  jq -S '.firewall.rules | map(
    if .ics == {} then del(.ics)
    elif (.ics | type) == "object" and .ics.protocol == "iec104"
      and .ics.mode == "monitor" and .ics.readOnly == false
    then del(.ics.readOnly) else . end)' "$@"
}
eu_commit_manual_policy() {
  local policy="$1" label="$2" response status warnings running
  local revision
  local candidate_file="$TMP_DIR/manual-candidate.json" headers_file="$TMP_DIR/manual-commit.headers"
  # shellcheck disable=SC2034 # ownership is read by eu-cleanup.sh
  state_touched=1
  # Match containd.Client.ImportConfig: preserve appliance-owned devices by
  # logical name, then enable enforcement so the commit updates effective nft rules.
  running=$(eu_firewall_config) || {
    fail "cannot read appliance bindings before staging manual policy $label"
    return 1
  }
  revision=$(eu_revision_header "$TMP_DIR/fw-config.headers")
  [ -n "$revision" ] || { fail "running config has no revision header"; return 1; }
  jq --argjson running "$running" '
      def string_or_null: type == "string" or type == "null";
      (if $running.interfaces == null then [] else $running.interfaces end) as $interfaces
      | if ($interfaces | type) != "array" then
          error("running interfaces must be an array")
        elif any($interfaces[];
          (type != "object" and type != "null")
          or (.name | string_or_null | not)
          or (.device | string_or_null | not)) then
          error("cannot decode running interface bindings")
        else . end
      | (reduce $interfaces[] as $iface
          ({}; .[$iface.name // ""] = ($iface.device // ""))) as $devices
      | .interfaces |= map(
          del(.device)
          | ($devices[.name] // "") as $device
          | if $device != "" then .device = $device else . end)
      | .dataplane |= (if type == "object" then . else {} end)
      | .dataplane.enforcement = true' "$policy" >"$candidate_file" || {
    fail "cannot merge appliance bindings and enable dataplane enforcement for manual policy $label"
    return 1
  }
  response=$(docker exec -i "$FW" curl -sS --max-time 15 -D /dev/stderr -w '\n%{http_code}' \
    -H "X-Containd-If-Revision: $revision" -X POST -H "Authorization: Bearer $FW_TOKEN" -H 'Content-Type: application/json' \
    --data-binary @- http://127.0.0.1:8080/api/v1/config/candidate \
    <"$candidate_file" 2>"$TMP_DIR/manual-candidate.err") || {
    fail "stage manual policy $label failed: $(cat "$TMP_DIR/manual-candidate.err")"
    return 1
  }
  status="${response##*$'\n'}"
  response="${response%$'\n'*}"
  if [ "$status" != 200 ]; then
    fail "stage manual policy $label returned HTTP $status: $response"
    return 1
  fi
  revision=$(eu_revision_header "$TMP_DIR/manual-candidate.err")
  [ -n "$revision" ] || { fail "candidate response has no revision header"; return 1; }
  # Headers go through the container's stderr, not a host path inside it.
  response=$(docker exec "$FW" curl -sS --max-time 15 -w '\n%{http_code}' -D /dev/stderr \
    -H "X-Containd-If-Revision: $revision" \
    -X POST -H "Authorization: Bearer $FW_TOKEN" -H 'Content-Type: application/json' \
    http://127.0.0.1:8080/api/v1/config/commit 2>"$headers_file") || {
    fail "commit manual policy $label failed: $(cat "$headers_file")"
    return 1
  }
  status="${response##*$'\n'}"
  response="${response%$'\n'*}"
  if [ "$status" != 200 ]; then
    fail "commit manual policy $label returned HTTP $status: $response"
    return 1
  fi
  warnings=$(awk 'tolower($0) ~ /^x-containd-warnings:/ {
    sub(/^[^:]*:[[:space:]]*/, ""); sub(/[[:space:]]*$/, "");
    if (length) print
  }' "$headers_file") || {
    fail "cannot read commit warnings for manual policy $label"
    return 1
  }
  if [ -n "$warnings" ]; then
    fail "commit manual policy $label reported policy import warnings: $warnings"
    return 1
  fi
  eu_wait_protection "$response" || return 1
  pass "$label committed and protecting without warnings"
}

# Observer attribution is checked after timing-sensitive delivery evidence;
# its independent grace/tick must not consume the policy-loss deadline.
eu_manual_policy_readback() {
  local policy="$1" label="$2" active effective expected_file actual_file i
  # policy_observer.go: observerGracePeriod=5s, observerInterval=5s.
  # Fifteen one-second waits cover grace + tick with five seconds of margin.
  for ((i = 0; i <= 15; i++)); do
    if ! eu_api_get "/api/firewall/active"; then
      fail "cannot read active manual policy $label"
      return 1
    fi
    active="$API_BODY"
    if printf '%s' "$active" | jq -e \
      '.active_config == "custom" and .policy_source == "manual-custom" and (has("plan") | not)' \
      >/dev/null 2>&1; then
      break
    fi
    [ "$i" -eq 15 ] || sleep 1
  done
  if [ "$i" -gt 15 ]; then
    fail "active policy did not converge to custom/manual-custom after $label: $active"
    return 1
  fi
  effective=$(eu_firewall_config) || {
    fail "containd running-config readback failed after $label"
    return 1
  }
  expected_file="$TMP_DIR/expected-rules.json"
  actual_file="$TMP_DIR/actual-rules.json"
  eu_normalize_firewall_rules "$policy" >"$expected_file" || {
    fail "cannot parse expected rules for $label"
    return 1
  }
  printf '%s' "$effective" | eu_normalize_firewall_rules >"$actual_file" || {
    fail "containd readback rules are unparsable after $label"
    return 1
  }
  if ! cmp -s "$expected_file" "$actual_file"; then
    fail "containd effective firewall rules differ from custom $label before accepting delivery evidence"
    return 1
  fi
  pass "$label manually committed without warnings, observed as manual-custom and effective rules read back exactly"
  return 0
}
eu_make_custom_policy() {
  local source_file="$1" src="$2" dst="$3" port="$4" output="$5"
  jq --arg src "$src" --arg dst "$dst" --arg port "$port" '
    def target($source; $destination; $destination_port):
      .action == "ALLOW"
      and (.ics // {}) == {}
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
