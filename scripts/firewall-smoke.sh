#!/usr/bin/env bash
# Firewall traffic smoke test.
#
# Applies each named policy via the lab's POST /api/firewall/apply
# endpoint (same path the lab UI uses, so the backend's
# ensureEnforcementOn shim and candidate/commit flow are exercised),
# then probes a positive+negative traffic matrix from inside the lab
# containers to confirm the dataplane enforces what the policy says.
#
# Verdict mapping:
#   allow = SYN got through the firewall (TCP connect succeeds, OR
#           fast RST from a closed port — packet reached destination)
#   deny  = SYN was dropped (timeout)
#
# The probe uses bash /dev/tcp where available, busybox nc otherwise.
# Both can return rc=1 for either "refused" or "timeout"; we
# disambiguate by elapsed time (refused returns in <300ms, drops sit
# at the timeout). This matters because Alpine sims (rtac, historian,
# gps) have only busybox nc which conflates the two return paths.
#
# Matrix probe ports are chosen so the destination is actually
# listening — testing "policy ALLOWS this" requires a listener on the
# far side, otherwise a no-listener RST (which Docker Desktop's
# bridge sometimes drops as a runt frame) is indistinguishable from
# a firewall drop. Where the workshop narrative implies a service
# the destination doesn't actually run (e.g. SSH on vendor-jump),
# the row is omitted from the matrix until the listener exists.
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
# Threshold for fast-fail vs slow-timeout disambiguation. Refused TCP
# (RST received) typically completes within tens of ms. Firewall drop
# completes at PROBE_TIMEOUT seconds. 500ms is a safe middle.
REFUSED_THRESHOLD_MS="${REFUSED_THRESHOLD_MS:-500}"

fail=0
total=0
passed=0

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; passed=$((passed+1)); }
err()  { printf '  ✗ %s\n' "$1"; fail=1; }

# ---------------------------------------------------------------------
# Probe helpers — run inside source container, return verdict string.
#
# Timing is done host-side via python3 because container-side `date`
# has inconsistent %3N support (busybox skips it, GNU emits ms,
# alpine bash emits full ns). Host-side timing includes a fixed
# docker-exec overhead (~50-100ms) but the gap between fast-refused
# (~50-150ms) and timeout-drop (~3000ms) is still wide enough to
# disambiguate cleanly.
# ---------------------------------------------------------------------

ms_now() { python3 -c 'import time; print(int(time.time()*1000))'; }

# probe_tcp emits "VERDICT DURATION_MS" (space-separated) on stdout
# so callers can read both. Verdict is "allow", "deny", or "deny:rc=N".
probe_tcp() {
  local src="$1" dst="$2" port="$3"
  local rc start end dur
  start=$(ms_now)
  if docker exec "$src" sh -c 'command -v bash >/dev/null 2>&1' 2>/dev/null; then
    docker exec "$src" timeout "$PROBE_TIMEOUT" bash -c "exec 3<>/dev/tcp/$dst/$port" >/dev/null 2>&1
    rc=$?
  else
    # busybox nc — -w is I/O timeout. stdin from /dev/null closes
    # after the SYN/ACK handshake.
    docker exec "$src" sh -c "timeout $PROBE_TIMEOUT nc -w $PROBE_TIMEOUT $dst $port < /dev/null > /dev/null 2>&1"
    rc=$?
  fi
  end=$(ms_now)
  dur=$(( end - start ))

  local verdict
  case "$rc" in
    0)
      verdict=allow
      ;;
    124|143)
      # GNU timeout returns 124 on expiry, 143 on SIGTERM. Both =
      # firewall drop. (busybox timeout uses 143.)
      verdict=deny
      ;;
    1)
      # Ambiguous: bash /dev/tcp returns 1 for "refused" (RST
      # received). busybox nc returns 1 for both refused AND timeout.
      # Disambiguate by elapsed time — a real RST roundtrip is fast
      # (<500ms even with docker exec overhead), a firewall drop
      # sits at the full PROBE_TIMEOUT seconds.
      if [ "$dur" -lt "$REFUSED_THRESHOLD_MS" ]; then
        verdict=allow
      else
        verdict=deny
      fi
      ;;
    *)
      verdict="deny:rc=$rc"
      ;;
  esac
  printf '%s %s\n' "$verdict" "$dur"
}

# probe_udp uses the firewall's nft drop counter snapshot. Rough but
# sufficient for the one UDP rule we currently care about (NTP). When
# the matrix grows we'll switch to a per-rule counter.
probe_udp() {
  local src="$1" dst="$2" port="$3"
  local before after
  before=$(docker exec rangerdanger-firewall nft list table inet containd 2>/dev/null \
           | awk '/policy drop/{getline; print}' | grep -oE 'packets [0-9]+' | head -1 | awk '{print $2}')
  docker exec "$src" sh -c "echo probe | timeout $PROBE_TIMEOUT nc -u -w1 $dst $port" >/dev/null 2>&1 || true
  sleep 0.5
  after=$(docker exec rangerdanger-firewall nft list table inet containd 2>/dev/null \
          | awk '/policy drop/{getline; print}' | grep -oE 'packets [0-9]+' | head -1 | awk '{print $2}')
  before=${before:-0}; after=${after:-0}
  if [ "$after" -gt "$before" ]; then
    printf 'deny 0\n'
  else
    printf 'allow 0\n'
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
#
# Probe ports are chosen so the destination listens on them today.
# rtac-sim now hosts sshd on :22 and nginx on :443 (see
# services/Dockerfile rtac-sim stage + rtac-mgmt-init.sh) so the
# vendor → OT mgmt rows are probeable. Other workshop-narrative
# ports without listeners (vendor:3389 RDP, eng->rtac:22/443 from
# enterprise) remain omitted until corresponding listeners are
# added — see scripts/lab-commands-smoke.sh for the broader
# CLI-command coverage matrix.
# ---------------------------------------------------------------------

# Firewall zone-side IPs (each zone reaches firewall on its own subnet).
FW_WAN=10.10.10.2
FW_DMZ=10.20.20.2
FW_LAN1=10.30.30.2
FW_LAN2=10.40.40.2

# format: src|dst|proto|port|expect_weak|expect_improved|note
read -r -d '' MATRIX <<EOF || true
rangerdanger-kali|10.20.20.10|tcp|22|allow|allow|kali->vendor SSH (vendor portal)
rangerdanger-kali|10.20.20.10|tcp|80|allow|allow|kali->vendor HTTP (portal page)
rangerdanger-kali|10.20.20.10|tcp|443|allow|allow|kali->vendor HTTPS (portal page TLS)
rangerdanger-kali|10.20.20.10|tcp|3389|allow|deny|kali->vendor RDP (improved blocks)
rangerdanger-kali|10.20.20.10|tcp|5900|allow|deny|kali->vendor VNC (improved blocks)
rangerdanger-kali|10.30.30.20|tcp|8080|allow|deny|kali->rtac HTTP API
rangerdanger-kali|10.30.30.20|tcp|502|allow|deny|kali->rtac Modbus (multi-proto pin)
rangerdanger-kali|10.30.30.20|tcp|20000|allow|deny|kali->rtac DNP3 (multi-proto pin)
rangerdanger-kali|10.30.30.30|tcp|8080|allow|deny|kali->openplc HTTP
rangerdanger-kali|10.30.30.30|tcp|502|allow|deny|kali->openplc Modbus (multi-proto pin)
rangerdanger-eng-ws|10.30.30.20|tcp|8080|allow|deny|eng->rtac HTTP (weak wide)
rangerdanger-vendor-jump|10.30.30.20|tcp|22|allow|allow|vendor->rtac SSH mgmt (improved keeps for monitoring)
rangerdanger-vendor-jump|10.30.30.20|tcp|443|allow|allow|vendor->rtac HTTPS mgmt (improved keeps for monitoring)
rangerdanger-vendor-jump|10.30.30.20|tcp|502|allow|deny|vendor->rtac Modbus (improved blocks)
rangerdanger-eng-ws|10.30.30.30|tcp|502|allow|deny|eng->openplc Modbus (weak wide)
rangerdanger-fuxa-hmi|10.30.30.30|tcp|502|allow|allow|fuxa(intra-zone OT)->openplc Modbus
rangerdanger-historian-sim|10.30.30.30|tcp|502|allow|allow|historian(intra-zone OT)->openplc Modbus
rangerdanger-rtac-sim|10.30.30.30|tcp|502|allow|allow|rtac->openplc Modbus (intra-zone)
rangerdanger-rtac-sim|10.30.30.30|tcp|20000|allow|allow|rtac->openplc DNP3 (intra-zone)
rangerdanger-rtac-sim|10.30.30.30|tcp|8080|allow|allow|rtac->openplc HTTP (intra-zone)
rangerdanger-kali|${FW_WAN}|tcp|8080|allow|deny|kali->fw mgmt (improved blocks wan)
rangerdanger-kali|${FW_WAN}|tcp|2222|allow|deny|kali->fw SSH (improved blocks wan)
rangerdanger-eng-ws|${FW_DMZ}|tcp|8080|allow|allow|eng->fw mgmt (always allowed)
rangerdanger-eng-ws|${FW_DMZ}|tcp|2222|allow|allow|eng->fw SSH (always allowed)
rangerdanger-rtac-sim|${FW_LAN1}|tcp|8080|allow|allow|rtac->fw mgmt (always allowed)
rangerdanger-relay-sim|${FW_LAN2}|tcp|8080|allow|deny|relay->fw mgmt (improved blocks lan2; openplc moved off lan2 in F-011)
EOF

# Dataplane canary — probes kali->rtac:502 with a 1s timeout so we
# can poll cheaply. The verdict diverges between weak (allow) and
# improved (deny), so it tells us whether the dataplane has caught
# up with the policy the API just reported as active.
canary_tcp() {
  if docker exec rangerdanger-kali timeout 1 bash -c \
      'exec 3<>/dev/tcp/10.30.30.20/502' >/dev/null 2>&1; then
    echo allow
  else
    echo deny
  fi
}

# Poll the canary until verdict matches expected (or budget exhausts).
# Returns 0 if reconciled, 1 if budget exhausted.
wait_for_dataplane() {
  local expected="$1"
  local budget="${2:-15}"
  local i
  for i in $(seq 1 $((budget * 2))); do
    [ "$(canary_tcp)" = "$expected" ] && return 0
    sleep 0.5
  done
  return 1
}

apply_policy() {
  local name="$1"
  note "applying policy: $name"
  local resp
  resp=$(curl -fsS -X POST -H 'Content-Type: application/json' \
              -d "{\"config\":\"$name\"}" "$API/api/firewall/apply" 2>&1) \
      || { err "apply $name failed: $resp"; return 1; }
  ok "applied: $resp"

  # Poll the canary instead of fixed-sleeping. The API can report the
  # new active_config before nft rules and NFQUEUE consumers have
  # reconciled — that's the race that caused intermittent first-run
  # failures when the dataplane was probed during the gap.
  local expected_canary
  case "$name" in
    weak|baseline)     expected_canary=allow ;;
    improved|hardened) expected_canary=deny  ;;
    *) sleep "$SETTLE_SECS"; return 0 ;;
  esac
  if wait_for_dataplane "$expected_canary" 15; then
    ok "dataplane reconciled to $name"
  else
    err "dataplane never reconciled to $name (canary kali->rtac:502 still wrong after 15s)"
    return 1
  fi
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
    local expect actual dur out
    [ "$field" = "5" ] && expect="$exp_weak" || expect="$exp_improved"
    total=$((total+1))
    out=$(probe "$proto" "$src" "$dst" "$port")
    actual=$(echo "$out" | awk '{print $1}')
    dur=$(echo "$out" | awk '{print $2}')
    if [ "$actual" = "$expect" ]; then
      ok "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$actual  ${dur}ms"
    else
      err "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$actual  ${dur}ms"
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

# Wait for cross-zone routing to actually be ready. The kasm-based
# webtops (eng-ws, vendor-jump) install the firewall as their
# default gateway via /custom-cont-init.d/set-gateway.sh — that
# step can finish AFTER backend reports healthy on slow runners,
# so cross-zone probes from those containers will time out. Poll
# until the default gateway points at the per-zone firewall IP
# before declaring preflight done.
note "wait for cross-zone routing"
declare -a WEBTOP_GATEWAYS=(
  "rangerdanger-eng-ws|10.20.20.2"
  "rangerdanger-vendor-jump|10.20.20.2"
)
for entry in "${WEBTOP_GATEWAYS[@]}"; do
  c="${entry%|*}"
  gw="${entry#*|}"
  ready=0
  for i in $(seq 1 30); do
    if docker exec "$c" ip route show default 2>/dev/null | grep -q "via $gw"; then
      ok "$c default route via $gw (after ${i}s)"
      ready=1
      break
    fi
    sleep 1
  done
  [ "$ready" = "1" ] || err "$c never installed default route via $gw — set-gateway.sh may have failed"
done
[ "$fail" = "0" ] || { note "summary"; echo "  preflight failed; aborting"; exit 1; }

# Wait for canonical listeners to actually accept connections.
# `docker compose ps healthy` doesn't catch every service: openplc's
# Modbus/DNP3 daemons start AFTER its HTTP healthcheck passes, so
# probing openplc:502 too early lands on a closed port — busybox nc
# returns rc=1 with high duration → our heuristic maps that to
# "deny" → the matrix flags an enforcement failure that isn't.
# Probe each expected listener directly from the firewall (which has
# direct connectivity to every zone) until it accepts.
note "wait for canonical service listeners"
declare -a LISTENERS=(
  "10.30.30.20:8080|rtac HTTP API"
  "10.30.30.20:502|rtac Modbus"
  "10.30.30.20:20000|rtac DNP3"
  "10.30.30.20:22|rtac SSH mgmt"
  "10.30.30.20:443|rtac HTTPS mgmt"
  "10.30.30.30:502|openplc Modbus"
  "10.30.30.30:8080|openplc HTTP"
  "10.40.40.20:502|relay Modbus"
  "10.20.20.10:8082|vendor-jump kasm"
  "10.20.20.10:22|vendor-jump SSH"
)
# Apply weak briefly so listener probes can traverse zones; the matrix
# resets policy per-iteration anyway.
curl -fsS -X POST -H 'Content-Type: application/json' \
     -d '{"config":"weak"}' "$API/api/firewall/apply" >/dev/null 2>&1 || true
sleep 2

for entry in "${LISTENERS[@]}"; do
  hp="${entry%|*}"
  label="${entry#*|}"
  host="${hp%:*}"
  port="${hp#*:}"
  ready=0
  for i in $(seq 1 30); do
    if docker exec rangerdanger-firewall sh -c \
        "timeout 1 bash -c 'exec 3<>/dev/tcp/$host/$port'" >/dev/null 2>&1; then
      [ "$i" -gt 1 ] && ok "$label ($hp) listening (after ${i}s)" || ok "$label ($hp) listening"
      ready=1
      break
    fi
    sleep 1
  done
  [ "$ready" = "1" ] || err "$label ($hp) never came up — image still booting? service crashed?"
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

# Reset matrix counters (preflight ok calls inflate `passed`; only
# matrix rows count toward the policy verdict).
matrix_total=0
matrix_passed=0
matrix_passed_start=$passed

for p in $POLICIES; do
  apply_policy "$p" || continue
  note "matrix: $p"
  run_matrix "$p"
done

matrix_passed=$(( passed - matrix_passed_start ))
# subtract the apply_policy ✓ entries from the matrix-passed count —
# two per policy: one for "applied" and one for "dataplane reconciled".
matrix_passed=$(( matrix_passed - $(echo "$POLICIES" | wc -w) * 2 ))

# ---------------------------------------------------------------------
# Summary.
# ---------------------------------------------------------------------
note "summary"
echo "  matrix passed: $matrix_passed / $total"
if [ "$fail" = "0" ]; then
  echo "  ALL TRAFFIC ROWS MATCH EXPECTED"
  exit 0
else
  echo "  FAILED — $((total - matrix_passed)) rows mismatched"
  exit 1
fi
