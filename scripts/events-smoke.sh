#!/usr/bin/env bash
# Events smoke test — exercises the v0.1.25 firewall.rule.hit event
# pipeline + ICS DPI detection/logging that the traffic-only smoke
# (firewall-smoke.sh) doesn't cover.
#
# Four checks:
#
#   1. L4 events flow under improved policy
#      Apply improved → kali Modbus probe → expect at least one
#      firewall.rule.hit DENY event for deny-enterprise-to-field via
#      /api/substation/network-events (= backend's filtered view, the
#      same surface the lab UI's Live DPI Events strip reads). Catches
#      regressions in: nflog consumer wiring, X-Containd-Warnings
#      header parsing, backend Event struct schema compat,
#      isSubstationRelevant filter.
#
#   2. ICS template apply works via the canonical (hyphenated) name
#      POST /api/v1/templates/ics/apply with template=modbus-read-only
#      should land 2 rules in the policy. Catches: the hyphen/underscore
#      name normalization (the apply switch was written with
#      underscores but the list endpoint returns hyphens — anyone
#      copy-pasting from the list would 400 without the fix).
#
#   3. ICS DPI function-code detection/logging (the L4 source pin
#      enforces; DPI detects and logs). This check needs NFQUEUE and is
#      skipped if the host kernel cannot bind it, e.g. some macOS Docker
#      Desktop kernels. RTAC sends Modbus FC8 → expect matching DPI
#      evidence within a few seconds. Catches: NFQueueGroup wiring,
#      compiler chain ordering and DPI evidence publication.
#
#   4. DNP3 ICS DPI function-code detection/logging, under the same
#      NFQUEUE prerequisite, for an RTAC Direct Operate probe.
#
# Assumes rangerdanger stack is up + backend healthy (run
# scripts/smoke-test.sh --keep first).

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
JWT_SECRET="${CONTAIND_JWT_SECRET:-rangerdanger-dev}"
PROBE_WAIT="${PROBE_WAIT:-4}"          # initial wait before first event check
EVENT_POLL_BUDGET="${EVENT_POLL_BUDGET:-45}"  # max additional seconds to wait
                                              # for an event to materialise in
                                              # containd's store. Bumped 20 -> 45
                                              # after gate 1 flaked on a loaded
                                              # GH runner: the nflog DENY event
                                              # surfaced late (a propagation
                                              # race, not the consumer broken).
                                              # Still a hard fail at the budget
                                              # edge so a genuine break shows.

fail=0
passed=0
skipped=0
required_skipped=0
total=0

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; passed=$((passed+1)); total=$((total+1)); }
err()  { printf '  ✗ %s\n' "$1"; fail=1; total=$((total+1)); }
skip() {
  local requirement="${3:-optional}"
  printf '  ⊘ %s (skipped: %s; %s)\n' "$1" "$2" "$requirement"
  skipped=$((skipped+1))
  total=$((total+1))
  if [ "$requirement" = "required" ]; then
    required_skipped=$((required_skipped+1))
    fail=1
  fi
}

note "preflight"
if ! curl -fsS "$API/api/health" >/dev/null 2>&1; then
  err "backend $API not healthy — bring stack up first"
  exit 1
fi
ok "backend healthy"

# These checks assert the US range. Refuse another package or a range
# mid-switch: GET /api/range must report us-dnp3-substation ready.
# shellcheck source=lib/range.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/range.sh"
# Printed, not ok(): ok() counts toward the gate's passed/total.
if range_is_ready us-dnp3-substation; then
  printf '  ✓ %s\n' "range us-dnp3-substation ready"
else
  err "range is not us-dnp3-substation ready — GET $API/api/range: $(range_status)"
  exit 1
fi

# Generate an admin JWT so we can hit firewall API endpoints directly
# (the X-Containd-Warnings inspection step needs auth).
TOKEN=$(python3 -c "
import json, hmac, hashlib, base64, time
secret = b'$JWT_SECRET'
header = base64.urlsafe_b64encode(json.dumps({'alg':'HS256','typ':'JWT'}, separators=(',',':')).encode()).rstrip(b'=').decode()
payload = base64.urlsafe_b64encode(json.dumps({'sub':'smoke','role':'admin','exp':int(time.time())+600}, separators=(',',':')).encode()).rstrip(b'=').decode()
msg = f'{header}.{payload}'
sig = base64.urlsafe_b64encode(hmac.new(secret, msg.encode(), hashlib.sha256).digest()).rstrip(b'=').decode()
print(f'{msg}.{sig}')
")
if [ -z "$TOKEN" ]; then
  err "failed to generate JWT for direct containd queries"
  exit 1
fi

# Dataplane canary — same design as firewall-smoke.sh and
# lab-commands-smoke.sh. /api/firewall/apply returns once containd has
# committed the config, but nft rules and the nflog/NFQUEUE consumers
# reconcile asynchronously after that, so a fixed sleep after apply is
# a race. Poll the canary until the verdict matches the policy we just
# applied instead. The canary proves the drop rules are live; whether
# the nflog consumer came back after the commit is gate 1's job.
canary_tcp() {
  if docker exec rangerdanger-kali timeout 1 bash -c \
      'exec 3<>/dev/tcp/10.30.30.20/502' >/dev/null 2>&1; then
    echo allow
  else
    echo deny
  fi
}

wait_for_dataplane() {
  local expected="$1"
  local budget="${2:-15}"
  for _ in $(seq 1 $((budget * 2))); do
    [ "$(canary_tcp)" = "$expected" ] && return 0
    sleep 0.5
  done
  return 1
}

# apply_policy weak|improved — apply via the lab API, then wait for the
# dataplane to reconcile. Records ok/err; returns non-zero on failure.
apply_policy() {
  local name="$1" resp expected_canary
  resp=$(curl -fsS -m 30 -X POST "$API/api/firewall/apply" \
    -H 'Content-Type: application/json' \
    -d "{\"config\":\"$name\"}" 2>&1) \
    || { err "apply $name failed: $resp"; return 1; }
  case "$name" in
    weak)     expected_canary=allow ;;
    improved) expected_canary=deny ;;
    *) err "apply_policy: unknown policy $name"; return 1 ;;
  esac
  if wait_for_dataplane "$expected_canary" 15; then
    ok "apply $name: $resp (dataplane reconciled)"
  else
    err "dataplane never reconciled to $name (canary kali->rtac:502 still wrong after 15s)"
    return 1
  fi
}

# ─────────────────────────────────────────────────────────────────────
# Gate 1: L4 firewall.rule.hit events flow under improved policy
# ─────────────────────────────────────────────────────────────────────
note "gate 1: L4 firewall.rule.hit event flow"

apply_policy improved || exit 1

# Use the range kernel clock, just like eu_epoch_ns in lib/eu-range.sh.
# That library also initializes EU-only point metadata, so do not source it
# in this US gate. Never substitute the host clock (Docker Desktop can drift).
probe_boundary=$(docker exec rangerdanger-firewall date -u +%s%N) || {
  err "cannot capture range-clock event boundary"; exit 1;
}
if ! [[ "$probe_boundary" =~ ^[0-9]{19}$ ]]; then
  err "invalid range-clock event boundary: $probe_boundary"; exit 1
fi

# Kali probe — port 502 to a field device should hit deny-enterprise-to-field
# (an L4 log+drop rule, not an ICS-eligible one). nc -w 3 bounds it,
# because the SYN drops and nc would otherwise wait forever.
docker exec rangerdanger-kali sh -c "nc -nv -w 3 10.40.40.20 502 < /dev/null" >/dev/null 2>&1 || true
sleep "$PROBE_WAIT"

# Query the engine directly. Both listings prioritize retained DENYs, not
# globally newest events; no age expiry means an old probe can still appear.
# The timestamp boundary below prevents that historical evidence passing.
engine_events() {
  docker exec rangerdanger-firewall sh -c \
    "curl -s 'http://127.0.0.1:8081/internal/events?limit=500'" 2>/dev/null
}

count_dpi_requests() {
  local proto="$1" function_code="$2" src_ip="$3" dst_ip="$4" dst_port="$5" boundary="$6"
  engine_events | python3 -c '
import calendar, datetime, json, re, sys
def epoch_ns(value):
    match = re.fullmatch(r"(.*T\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)", value or "")
    if not match:
        raise ValueError("event timestamp is not RFC3339Nano")
    parsed = datetime.datetime.fromisoformat(match[1] + match[3].replace("Z", "+00:00"))
    seconds = calendar.timegm(parsed.astimezone(datetime.timezone.utc).utctimetuple())
    return seconds * 1_000_000_000 + int((match[2] or "").ljust(9, "0"))
proto, function_code, src_ip, dst_ip, dst_port = sys.argv[1:6]
boundary = int(sys.argv[6])
events = json.load(sys.stdin) or []
matches = [event for event in events
           if event.get("proto") == proto
           and event.get("kind") == "request"
           and event.get("attributes", {}).get("ics") is True
           and event.get("attributes", {}).get("function_code") == int(function_code)
           and event.get("srcIp") == src_ip
           and event.get("dstIp") == dst_ip
           and event.get("srcPort", 0) > 0
           and event.get("dstPort") == int(dst_port)
           and event.get("transport") == "tcp"
           and epoch_ns(event.get("timestamp")) > boundary]
print(len(matches))
' "$proto" "$function_code" "$src_ip" "$dst_ip" "$dst_port" "$boundary"
}

wait_for_dpi_request() {
  local proto="$1" function_code="$2" src_ip="$3" dst_ip="$4" dst_port="$5" boundary="$6"
  local elapsed=0 count=0
  while [ "$elapsed" -le "$EVENT_POLL_BUDGET" ]; do
    count=$(count_dpi_requests "$proto" "$function_code" "$src_ip" "$dst_ip" "$dst_port" "$boundary") || return 2
    if [ "$count" -ge 1 ]; then
      printf '%s\n' "$count"
      return 0
    fi
    [ "$elapsed" -eq "$EVENT_POLL_BUDGET" ] && break
    sleep 1
    elapsed=$((elapsed + 1))
  done
  printf '0\n'
  return 1
}

count_kali_denies() {
  engine_events | python3 -c '
import calendar, datetime, json, re, sys
def epoch_ns(value):
    match = re.fullmatch(r"(.*T\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)", value or "")
    if not match:
        raise ValueError("event timestamp is not RFC3339Nano")
    parsed = datetime.datetime.fromisoformat(match[1] + match[3].replace("Z", "+00:00"))
    seconds = calendar.timegm(parsed.astimezone(datetime.timezone.utc).utctimetuple())
    return seconds * 1_000_000_000 + int((match[2] or "").ljust(9, "0"))
boundary = int(sys.argv[1])
events = json.load(sys.stdin) or []
denies = [e for e in events
          if e.get("kind") == "firewall.rule.hit"
          and e.get("attributes",{}).get("action") == "DENY"
          and e.get("attributes",{}).get("ruleId") == "deny-enterprise-to-field"
          and e.get("srcIp") == "10.10.10.50"
          and e.get("dstIp") == "10.40.40.20"
          and e.get("dstPort") == 502
          and e.get("transport") == "tcp"
          and epoch_ns(e.get("timestamp")) > boundary]
print(len(denies))
' "$probe_boundary"
}

# Poll for the DENY event. The nflog consumer + engine event store + REST
# endpoint occasionally take longer than PROBE_WAIT (4s) to surface the
# event under CI runner load. Polling within EVENT_POLL_BUDGET keeps the
# gate deterministic against that propagation delay without papering
# over a real regression: if the consumer is broken the event never
# appears and we still fail at the budget edge.
deny_count=0
elapsed=0
# `-le` so the loop checks at elapsed values 0..EVENT_POLL_BUDGET
# inclusive. The previous `-lt` exited after the final sleep without
# rechecking, so an event that surfaced during the last second of the
# advertised budget was still missed (raised in review on #72).
while [ "$elapsed" -le "$EVENT_POLL_BUDGET" ]; do
  deny_count=$(count_kali_denies) || {
    err "cannot read or parse engine events"; exit 1;
  }
  if [ "$deny_count" -ge 1 ]; then
    break
  fi
  if [ "$elapsed" -eq "$EVENT_POLL_BUDGET" ]; then
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

if [ "$deny_count" -lt 1 ]; then
  # Distinguish the known containd failure mode from anything else.
  # containd re-registers nflog group 100 on every config commit; the
  # old consumer's unbind/close is asynchronous, so a commit that lands
  # while the previous consumer still holds the group gets EPERM from
  # the kernel, containd emits service.nflog.unavailable and never
  # retries. Drops still happen (the canary says deny) but no
  # firewall.rule.hit events are produced until the next commit wins
  # the race. Seen on loaded CI runners (Smoke run 36103111036) and
  # reproduced locally 2026-09-25 (build/specs/events-flake/). This is
  # a containd bug (pkg/dp/engine/engine.go Reconfigure/Start), not a
  # lab regression, but the gate stays red because the lab's Live
  # Events strip is genuinely blind in this state.
  nflog_err=$(engine_events | python3 -c '
import json, sys
for e in json.load(sys.stdin) or []:
    if e.get("kind") == "service.nflog.unavailable":
        print(e.get("attributes", {}).get("error", "")); break
' 2>/dev/null)
  if [ -n "$nflog_err" ]; then
    err "no kali->field:502 DENY event after ${EVENT_POLL_BUDGET}s: containd reported service.nflog.unavailable (${nflog_err}) — nflog re-bind lost the race with the previous consumer on config commit"
  else
    err "no firewall.rule.hit DENY for kali(10.10.10.50)->field(10.40.40.x):502 in engine events after ${EVENT_POLL_BUDGET}s — nflog consumer regressed"
  fi
else
  ok "$deny_count kali->field:502 DENY event(s) in engine event store (after ${elapsed}s of polling)"
fi

# Also verify the backend's substation surface returns SOME events at all
# (catches a broken backend Event JSON schema mapping; the kali DENY
# specifically may not be in that retained-evidence page).
sub_total=$(curl -fsS "$API/api/substation/network-events" 2>/dev/null \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len((d or {}).get("events") or []))' \
  2>/dev/null || echo 0)
if [ "$sub_total" -lt 1 ]; then
  err "/api/substation/network-events returned 0 events — backend Event schema may be misaligned with containd"
else
  ok "/api/substation/network-events delivering events ($sub_total in window)"
fi

# ─────────────────────────────────────────────────────────────────────
# Gate 2: ICS template apply with hyphenated (canonical) name
# ─────────────────────────────────────────────────────────────────────
note "gate 2: ICS template name normalization (hyphen ↔ underscore)"

# Use preview=true so we exercise the name-normalization logic without
# perturbing the running policy. The apply switch was written with
# underscores ("modbus_read_only") but GET /api/v1/templates returns
# hyphenated names ("modbus-read-only"); before the normalize fix
# anyone copy-pasting from the list endpoint would 400. Preview
# returns the generated rules without writing them, so this gate is
# safe to run mid-test without conflicting with gate 1's improved
# policy state. Run from inside the firewall (loopback bypasses the
# mgmt-iface access check so host-side probes don't need lan3).
tmpl_resp=$(docker exec -e TOK="$TOKEN" rangerdanger-firewall sh -c \
  'curl -s -m 10 -X POST -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
    -d "{\"template\":\"modbus-read-only\",\"preview\":true,\"sourceZones\":[\"lan1\"],\"destZones\":[\"lan2\"]}" \
    http://127.0.0.1:8080/api/v1/templates/ics/apply' 2>&1)
if echo "$tmpl_resp" | grep -q '"preview":true'; then
  rule_count=$(echo "$tmpl_resp" | python3 -c \
    'import json,sys; d=json.load(sys.stdin); print(len(d.get("rules") or []))' 2>/dev/null || echo 0)
  if [ "$rule_count" -ge 2 ]; then
    ok "modbus-read-only (hyphenated) accepted + previewed $rule_count rules"
  else
    err "preview returned $rule_count rules (expected ≥2)"
  fi
else
  err "template preview rejected hyphenated name: $tmpl_resp"
fi

# ─────────────────────────────────────────────────────────────────────
# Gate 3: ICS DPI function-code detection/logging (NFQUEUE path) —
# the L4 source pin enforces; DPI detects and logs.
# ─────────────────────────────────────────────────────────────────────
note "gate 3: ICS DPI function-code detection/logging"

# This observation requires NFQUEUE to bind on the host kernel. When
# the host cannot provide it, report an optional skip rather than
# claiming DPI evidence was observed.
nfq_alive=0
if docker exec rangerdanger-firewall sh -c "cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null" 2>/dev/null | grep -q '^[[:space:]]*101'; then
  nfq_alive=1
fi
if [ "$nfq_alive" != "1" ]; then
  skip "ICS DPI detection/logging" "NFQUEUE 101 not bound (kernel environment does not support this optional observation)"
else
  ok "NFQUEUE 101 bound by containd"

  # Confirm RTAC is reachable + can python (needed for the probe).
  if ! docker exec rangerdanger-rtac-sim which python3 >/dev/null 2>&1; then
    skip "ICS DPI detection/logging" "rtac-sim lacks required python3 probe" required
  else
    # Install probe — single source of truth so the smoke is self-contained.
    cat <<'PY' | docker exec -i rangerdanger-rtac-sim sh -c "cat > /tmp/_smoke_modbus.py"
import socket, struct, sys
target, fc = sys.argv[1], sys.argv[2]
def mbap(pdu, tx=1):
    return struct.pack(">HHHB", tx, 0, len(pdu)+1, 1) + pdu
fc_map = {
    "fc3": mbap(struct.pack(">BHH", 3, 0, 1)),     # read holding regs (allowlisted)
    "fc8": mbap(struct.pack(">BHH", 8, 0, 0)),     # diagnostics (NOT in allowlist)
}
s = socket.socket(); s.settimeout(3)
try:
    s.connect((target, 502)); s.send(fc_map[fc])
    try: print(s.recv(256).hex())
    except socket.timeout: print("timeout")
finally: s.close()
PY

    # Fire FC8 from RTAC and check fresh decoded DPI event evidence.
    dpi_boundary=$(docker exec rangerdanger-firewall date -u +%s%N) || {
      err "cannot capture range-clock Modbus event boundary"; exit 1;
    }
    if ! [[ "$dpi_boundary" =~ ^[0-9]{19}$ ]]; then
      err "invalid range-clock Modbus event boundary: $dpi_boundary"; exit 1
    fi
    docker exec rangerdanger-rtac-sim python3 /tmp/_smoke_modbus.py 10.40.40.20 fc8 >/dev/null 2>&1 || true
    if dpi_count=$(wait_for_dpi_request modbus 8 10.30.30.20 10.40.40.20 502 "$dpi_boundary"); then
      ok "$dpi_count fresh Modbus FC8 request event(s) for 10.30.30.20 -> 10.40.40.20:502"
    else
      result=$?
      if [ "$result" -eq 2 ]; then
        err "cannot read or parse bounded Modbus DPI events"
      else
        err "no fresh Modbus FC8 request event for 10.30.30.20 -> 10.40.40.20:502 within ${EVENT_POLL_BUDGET}s"
      fi
    fi
  fi
fi

# ─────────────────────────────────────────────────────────────────────
# Gate 4: DNP3 ICS DPI function-code detection/logging (NFQUEUE path)
# ─────────────────────────────────────────────────────────────────────
# Parallel to gate 3 but for DNP3: the hardened policy allows the RTAC to
# poll field devices on DNP3/20000 but the rule's ICS function-code
# allowlist is [1] (Read/integrity poll) only. The L4 source pin enforces;
# DPI detects and logs function-code evidence.
note "gate 4: DNP3 ICS DPI function-code detection/logging"

if [ "$nfq_alive" != "1" ]; then
  skip "DNP3 ICS DPI detection/logging" "NFQUEUE 101 not bound (kernel environment does not support this optional observation)"
elif ! docker exec rangerdanger-rtac-sim which dnp3cmd >/dev/null 2>&1; then
  skip "DNP3 ICS DPI detection/logging" "rtac-sim lacks required dnp3cmd probe" required
else
  # Make sure the hardened policy (with the DNP3 FC allowlist) is active -
  # gate 1 applied improved, but re-assert in case a later gate changed it.
  apply_policy improved || true

  # Fire a DNP3 Direct Operate (FC5) from the RTAC to the relay and check
  # the fresh decoded DPI event. Restore the relay afterwards.
  dpi_boundary=$(docker exec rangerdanger-firewall date -u +%s%N) || {
    err "cannot capture range-clock DNP3 event boundary"; exit 1;
  }
  if ! [[ "$dpi_boundary" =~ ^[0-9]{19}$ ]]; then
    err "invalid range-clock DNP3 event boundary: $dpi_boundary"; exit 1
  fi
  docker exec rangerdanger-rtac-sim dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip >/dev/null 2>&1 || true
  if dnp3_count=$(wait_for_dpi_request dnp3 5 10.30.30.20 10.40.40.20 20000 "$dpi_boundary"); then
    ok "$dnp3_count fresh DNP3 FC5 request event(s) for 10.30.30.20 -> 10.40.40.20:20000"
  else
    result=$?
    if [ "$result" -eq 2 ]; then
      err "cannot read or parse bounded DNP3 DPI events"
    else
      err "no fresh DNP3 FC5 request event for 10.30.30.20 -> 10.40.40.20:20000 within ${EVENT_POLL_BUDGET}s"
    fi
  fi

  # Restore the baseline before cleaning up the flow record, then close
  # the relay after the dataplane has returned to weak.
  # Ending on weak also matches the lab default that gate 1 moved away from.
  apply_policy weak || true
  docker exec rangerdanger-rtac-sim dnp3cmd 10.40.40.20:20000 -a 1 crob 0 close >/dev/null 2>&1 || true
  # Report cleanup only on failure so the summary does not count an
  # implicit pass. A breaker left open de-energizes the feeder for later gates.
  breaker=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    breaker=$(curl -fsS --max-time 5 "$API/api/substation/state" 2>/dev/null | python3 -c 'import json,sys; print(str(json.load(sys.stdin)["devices"]["relay"]["breaker_closed"]).lower())' 2>/dev/null)
    [ "$breaker" = "true" ] && break
    sleep 1
  done
  [ "$breaker" = "true" ] || err "cleanup: relay breaker still open after the DNP3 close (breaker_closed=${breaker:-unknown})"
fi

# ─────────────────────────────────────────────────────────────────────
note "summary"
echo "  passed: $passed / $total"
echo "  skipped: $skipped"
echo "  required skipped: $required_skipped"
if [ "$fail" = "0" ]; then
  echo "  ALL REQUIRED EVENT CHECKS PASSED"
  exit 0
fi
echo "  FAILED — $(( total - passed - skipped )) check(s) failed; $required_skipped required check(s) skipped"
exit 1
