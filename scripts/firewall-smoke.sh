#!/usr/bin/env bash
# Firewall traffic smoke test.
#
# Applies each named policy via the lab's POST /api/firewall/apply
# endpoint (same path the lab UI uses, so the backend's
# ensureEnforcementOn shim and candidate/commit flow are exercised),
# then probes a positive+negative traffic matrix from inside the lab
# containers to confirm the dataplane enforces what the policy says.
#
# A row passes when the observed verdict (allow|deny) matches the
# expected verdict for that policy. Allow = SYN got through the
# firewall (TCP connect succeeds, OR fast RST from a closed dest).
# Deny  = SYN was dropped (timeout).
#
# Assumes the rangerdanger compose stack is already up and the
# backend is healthy. Run scripts/smoke-test.sh first if not.
#
# Usage:
#   ./scripts/firewall-smoke.sh                  # both policies
#   ./scripts/firewall-smoke.sh weak             # only weak
#   ./scripts/firewall-smoke.sh improved         # only improved
#
# Exit 0 = all rows passed, non-zero = at least one mismatch.

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
PROBE_TIMEOUT="${PROBE_TIMEOUT:-3}"   # seconds per probe
SETTLE_SECS="${SETTLE_SECS:-3}"        # wait after apply for nft reconcile

fail=0
total=0
passed=0

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; passed=$((passed+1)); }
err()  { printf '  ✗ %s\n' "$1"; fail=1; }

# ---------------------------------------------------------------------
# Probe helpers — run inside source container, return verdict string.
# Uses bash /dev/tcp because every lab container has bash. timeout(1)
# distinguishes drop (124) from refused (1) from connect (0).
# ---------------------------------------------------------------------

probe_tcp() {
  local src="$1" dst="$2" port="$3"
  local rc
  # Prefer bash /dev/tcp (works on every glibc/musl bash). Fall back
  # to nc -w for containers that don't ship bash (busybox-based sims).
  # Both distinguish drop (timeout/124) from refused (1) from
  # connect (0).
  if docker exec "$src" sh -c 'command -v bash >/dev/null 2>&1' 2>/dev/null; then
    docker exec "$src" timeout "$PROBE_TIMEOUT" bash -c "exec 3<>/dev/tcp/$dst/$port" >/dev/null 2>&1
    rc=$?
  else
    # busybox nc: -w sets I/O timeout; redirect stdin from /dev/null
    # so the connection closes after the SYN/ACK handshake.
    docker exec "$src" sh -c "timeout $PROBE_TIMEOUT nc -w 1 $dst $port < /dev/null > /dev/null 2>&1"
    rc=$?
  fi
  case $rc in
    0)   echo allow ;;   # connected
    1)   echo allow ;;   # refused — packet reached destination, just no listener
    124) echo deny  ;;   # timeout — firewall dropped
    *)   echo "deny:rc=$rc" ;;
  esac
}

# UDP probe: nc -u sends a probe byte and waits for response. Negatives
# are checked by snapshotting the firewall's drop counter; if it
# incremented, the probe was dropped.
probe_udp() {
  local src="$1" dst="$2" port="$3"
  local before after
  before=$(docker exec rangerdanger-firewall nft list table inet containd 2>/dev/null \
           | awk '/policy drop/{getline; print}' | grep -oE 'packets [0-9]+' | head -1 | awk '{print $2}')
  docker exec "$src" bash -c "echo probe | timeout $PROBE_TIMEOUT nc -u -w1 $dst $port" >/dev/null 2>&1 || true
  sleep 0.5
  after=$(docker exec rangerdanger-firewall nft list table inet containd 2>/dev/null \
          | awk '/policy drop/{getline; print}' | grep -oE 'packets [0-9]+' | head -1 | awk '{print $2}')
  before=${before:-0}; after=${after:-0}
  if [ "$after" -gt "$before" ]; then
    echo deny
  else
    echo allow
  fi
}

probe() {
  local proto="$1"; shift
  case "$proto" in
    tcp) probe_tcp "$@" ;;
    udp) probe_udp "$@" ;;
    *)   echo "deny:bad-proto" ;;
  esac
}

# ---------------------------------------------------------------------
# Matrix runner. Each row: src_container | dst_ip | proto | port | weak | improved
# ---------------------------------------------------------------------

# Firewall zone-side IPs (used as destination for INPUT-chain mgmt
# access probes — each zone reaches the firewall on its own subnet).
FW_WAN=10.10.10.2
FW_DMZ=10.20.20.2
FW_LAN1=10.30.30.2
FW_LAN2=10.40.40.2

# format: src|dst|proto|port|expect_weak|expect_improved|note
read -r -d '' MATRIX <<EOF || true
rangerdanger-kali|10.20.20.10|tcp|22|allow|allow|kali->vendor-jump SSH
rangerdanger-kali|10.20.20.10|tcp|3389|allow|deny|kali->vendor RDP (weak only)
rangerdanger-kali|10.30.30.20|tcp|8080|allow|deny|kali->rtac HTTP API
rangerdanger-kali|10.30.30.20|tcp|502|allow|deny|kali->rtac Modbus (multi-proto bug pin)
rangerdanger-kali|10.30.30.20|tcp|20000|allow|deny|kali->rtac DNP3 (multi-proto bug pin)
rangerdanger-kali|10.40.40.30|tcp|8080|allow|deny|kali->openplc HTTP
rangerdanger-kali|10.40.40.30|tcp|502|allow|deny|kali->openplc Modbus (multi-proto)
rangerdanger-kali|10.40.40.30|tcp|20000|allow|deny|kali->openplc DNP3 (multi-proto)
rangerdanger-eng-ws|10.30.30.20|tcp|8080|allow|deny|eng->rtac HTTP (weak wide)
rangerdanger-eng-ws|10.30.30.20|tcp|22|allow|allow|eng->rtac SSH
rangerdanger-eng-ws|10.30.30.20|tcp|443|allow|allow|eng->rtac HTTPS
rangerdanger-eng-ws|10.40.40.30|tcp|502|allow|deny|eng->openplc Modbus (weak wide)
rangerdanger-fuxa-hmi|10.40.40.30|tcp|502|allow|deny|fuxa(non-rtac OT)->openplc Modbus
rangerdanger-historian-sim|10.40.40.30|tcp|502|allow|deny|historian->openplc Modbus
rangerdanger-rtac-sim|10.40.40.30|tcp|502|allow|allow|rtac->openplc Modbus (canonical)
rangerdanger-rtac-sim|10.40.40.30|tcp|20000|allow|allow|rtac->openplc DNP3 (canonical)
rangerdanger-rtac-sim|10.40.40.30|tcp|8080|allow|allow|rtac->openplc HTTP (canonical)
rangerdanger-kali|${FW_WAN}|tcp|8080|allow|deny|kali->fw mgmt (improved blocks wan)
rangerdanger-kali|${FW_WAN}|tcp|2222|allow|deny|kali->fw SSH (improved blocks wan)
rangerdanger-eng-ws|${FW_DMZ}|tcp|8080|allow|allow|eng->fw mgmt (always allowed)
rangerdanger-eng-ws|${FW_DMZ}|tcp|2222|allow|allow|eng->fw SSH (always allowed)
rangerdanger-rtac-sim|${FW_LAN1}|tcp|8080|allow|allow|rtac->fw mgmt (always allowed)
rangerdanger-openplc|${FW_LAN2}|tcp|8080|allow|deny|openplc->fw mgmt (improved blocks lan2)
EOF

apply_policy() {
  local name="$1"
  note "applying policy: $name"
  local resp
  resp=$(curl -fsS -X POST -H 'Content-Type: application/json' \
              -d "{\"config\":\"$name\"}" "$API/api/firewall/apply" 2>&1) \
      || { err "apply $name failed: $resp"; return 1; }
  ok "applied: $resp"
  sleep "$SETTLE_SECS"
}

run_matrix() {
  local policy="$1"   # weak | improved
  local field         # 5 = weak, 6 = improved
  case "$policy" in
    weak)     field=5 ;;
    improved) field=6 ;;
    *) err "unknown policy $policy"; return 1 ;;
  esac

  while IFS='|' read -r src dst proto port exp_weak exp_improved label; do
    [ -z "$src" ] && continue
    local expect
    [ "$field" = "5" ] && expect="$exp_weak" || expect="$exp_improved"
    total=$((total+1))
    actual=$(probe "$proto" "$src" "$dst" "$port")
    if [ "$actual" = "$expect" ]; then
      ok "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$actual"
    else
      err "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$actual"
    fi
  done <<< "$MATRIX"
}

# ---------------------------------------------------------------------
# Preflight: backend healthy + key containers running.
# ---------------------------------------------------------------------
note "preflight"
curl -fsS "$API/api/health" >/dev/null 2>&1 \
    && ok "backend $API healthy" \
    || { err "backend not reachable at $API — bring stack up first"; exit 1; }

REQUIRED_CONTAINERS=(
  rangerdanger-firewall
  rangerdanger-kali
  rangerdanger-eng-ws
  rangerdanger-rtac-sim
  rangerdanger-fuxa-hmi
  rangerdanger-historian-sim
  rangerdanger-openplc
)
for c in "${REQUIRED_CONTAINERS[@]}"; do
  if docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null | grep -q running; then
    ok "$c running"
  else
    err "$c not running"
  fi
done
[ "$fail" = "0" ] || { note "summary"; echo "  preflight failed; aborting"; exit 1; }

# ---------------------------------------------------------------------
# Run policies.
# ---------------------------------------------------------------------
TARGETS="${1:-both}"
case "$TARGETS" in
  weak)     POLICIES="weak" ;;
  improved) POLICIES="improved" ;;
  both|"")  POLICIES="weak improved" ;;
  *) echo "usage: $0 [weak|improved|both]" >&2; exit 2 ;;
esac

for p in $POLICIES; do
  apply_policy "$p" || continue
  note "matrix: $p"
  run_matrix "$p"
done

# ---------------------------------------------------------------------
# Summary.
# ---------------------------------------------------------------------
note "summary"
echo "  passed: $passed / $total"
if [ "$fail" = "0" ]; then
  echo "  ALL TRAFFIC ROWS MATCH EXPECTED"
  exit 0
else
  echo "  FAILED — $((total - passed)) rows mismatched"
  exit 1
fi
