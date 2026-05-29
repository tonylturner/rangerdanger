#!/usr/bin/env bash
# Events smoke test — exercises the v0.1.25 firewall.rule.hit event
# pipeline + ICS DPI enforcement that the traffic-only smoke
# (firewall-smoke.sh) doesn't cover.
#
# Three gates:
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
#   3. ICS DPI function-code allowlist enforces (Linux/containerd
#      with NFQUEUE-capable kernel only — skipped if the host kernel's
#      netlink library can't bind nflog group, e.g. some macOS Docker
#      Desktop kernels). RTAC sends Modbus FC8 → expect block_flows
#      entry within a few seconds. Catches: NFQueueGroup wiring,
#      compiler chain reorder, BlockFlowTemp verdict path.
#
# Assumes rangerdanger stack is up + backend healthy (run
# scripts/smoke-test.sh --keep first).

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
FIREWALL_API="${CONTAINTD_API:-http://localhost:9080}"
JWT_SECRET="${CONTAIND_JWT_SECRET:-rangerdanger-dev}"
PROBE_WAIT="${PROBE_WAIT:-4}"          # initial wait before first event check
EVENT_POLL_BUDGET="${EVENT_POLL_BUDGET:-20}"  # max additional seconds to wait
                                              # for an event to materialise
                                              # in containd's store

fail=0
passed=0
total=0

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; passed=$((passed+1)); total=$((total+1)); }
err()  { printf '  ✗ %s\n' "$1"; fail=1; total=$((total+1)); }
skip() { printf '  ⊘ %s (skipped: %s)\n' "$1" "$2"; }

note "preflight"
if ! curl -fsS "$API/api/health" >/dev/null 2>&1; then
  err "backend $API not healthy — bring stack up first"
  exit 1
fi
ok "backend healthy"

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

# ─────────────────────────────────────────────────────────────────────
# Gate 1: L4 firewall.rule.hit events flow under improved policy
# ─────────────────────────────────────────────────────────────────────
note "gate 1: L4 firewall.rule.hit event flow"

resp=$(curl -fsS -m 30 -X POST "$API/api/firewall/apply" \
  -H 'Content-Type: application/json' \
  -d '{"config":"improved"}' 2>&1) \
  || { err "apply improved failed: $resp"; exit 1; }
ok "apply improved: $resp"
sleep 2

# Kali probe — port 502 to a field device should hit deny-enterprise-to-field
# (an L4 log+drop rule, not an ICS-eligible one). Use bash with timeout
# because the SYN drops, so nc would otherwise wait forever.
docker exec rangerdanger-kali sh -c "nc -nv -w 3 10.40.40.20 502 < /dev/null" >/dev/null 2>&1 || true
sleep "$PROBE_WAIT"

# Poll for the DENY event. The nflog consumer + engine event store + REST
# endpoint occasionally take longer than PROBE_WAIT (4s) to surface the
# event under CI runner load — previously this gate failed with
# "nflog consumer regressed" on what is actually just a polling-window
# race. Polling within an EVENT_POLL_BUDGET budget (default 20s) makes
# the gate deterministic against the same propagation race without
# papering over a real regression: if the consumer is genuinely broken
# the event will never appear and we still fail at the budget edge.
#
# Query the engine's full event store directly (not the substation
# endpoint, which limits to 50 most recent and gets drowned out by RTAC's
# continuous allow-traffic). limit=500 gives plenty of room for kali
# drops to appear.
deny_count=0
elapsed=0
while [ "$elapsed" -lt "$EVENT_POLL_BUDGET" ]; do
  deny_count=$(docker exec rangerdanger-firewall sh -c \
    "curl -s 'http://127.0.0.1:8081/internal/events?limit=500'" 2>/dev/null \
    | python3 -c '
import json, sys
events = json.load(sys.stdin) or []
denies = [e for e in events
          if e.get("kind") == "firewall.rule.hit"
          and e.get("attributes",{}).get("action") == "DENY"
          and e.get("srcIp","").startswith("10.10.10.")
          and e.get("dstIp","").startswith("10.40.40.")
          and e.get("dstPort") == 502]
print(len(denies))
' 2>/dev/null || echo 0)
  if [ "$deny_count" -ge 1 ]; then
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

if [ "$deny_count" -lt 1 ]; then
  err "no firewall.rule.hit DENY for kali(10.10.10.50)->field(10.40.40.x):502 in engine events after ${EVENT_POLL_BUDGET}s — nflog consumer regressed"
else
  ok "$deny_count kali->field:502 DENY event(s) in engine event store (after ${elapsed}s of polling)"
fi

# Also verify the backend's substation surface returns SOME events at all
# (catches a broken backend Event JSON schema mapping; the kali DENY
# specifically may not be in the 50-most-recent window if RTAC is busy).
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
# Gate 3: ICS DPI function-code enforcement (NFQUEUE path)
# ─────────────────────────────────────────────────────────────────────
note "gate 3: ICS DPI function-code allowlist enforcement"

# This gate requires NFQUEUE to actually bind on the host kernel. If
# the netlink layer can't (older mdlayher/netlink + LinuxKit panics,
# kernels missing CONFIG_NETFILTER_NETLINK_QUEUE, etc), containd
# silently falls back to plain accept and FC8 won't be blocked. Skip
# gracefully rather than fail the smoke when the path is unavailable.
nfq_alive=0
if docker exec rangerdanger-firewall sh -c "cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null" 2>/dev/null | grep -q '^[[:space:]]*101'; then
  nfq_alive=1
fi
if [ "$nfq_alive" != "1" ]; then
  skip "ICS DPI enforcement" "NFQUEUE 101 not bound (kernel/library env doesn't support it)"
else
  ok "NFQUEUE 101 bound by containd"

  # Confirm RTAC is reachable + can python (needed for the probe).
  if ! docker exec rangerdanger-rtac-sim which python3 >/dev/null 2>&1; then
    skip "ICS DPI enforcement" "rtac-sim lacks python3"
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

    # Fire FC8 from RTAC. DPI should parse it, see fc=8 NOT in [1..6],
    # apply BlockFlowTemp → entry appears in block_flows nft set.
    # The first FC8 packet itself may pass through (verdict is added
    # async); we check the SET, not whether the packet was blocked.
    block_before=$(docker exec rangerdanger-firewall sh -c \
      "nft list set inet containd block_flows 2>/dev/null | grep -c 'elements ='" 2>/dev/null || echo 0)
    docker exec rangerdanger-rtac-sim python3 /tmp/_smoke_modbus.py 10.40.40.20 fc8 >/dev/null 2>&1 || true
    sleep "$PROBE_WAIT"
    block_lines=$(docker exec rangerdanger-firewall sh -c \
      "nft list set inet containd block_flows 2>/dev/null" 2>/dev/null \
      | grep -c '10.30.30.20.*10.40.40.20.*502' || echo 0)
    if [ "$block_lines" -lt 1 ]; then
      err "FC8 from RTAC did not produce a block_flows entry — DPI parser may not be wired to BlockFlowTemp"
    else
      ok "FC8 (not in allowlist [1..6]) triggered block_flows entry for 10.30.30.20.10.40.40.20.502"
    fi
  fi
fi

# ─────────────────────────────────────────────────────────────────────
note "summary"
echo "  passed: $passed / $total"
if [ "$fail" = "0" ]; then
  echo "  ALL EVENT GATES PASSED"
  exit 0
fi
echo "  FAILED — $(( total - passed )) gate(s) regressed"
exit 1
