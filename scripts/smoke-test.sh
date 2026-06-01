#!/usr/bin/env bash
# End-to-end lab smoke test.
#
# Brings the RangerDanger stack up, hits the API endpoints, validates
# that the right lab inventory is loaded with the expected IDs and
# order strings, and confirms sims report healthy.
#
# Usage:
#   ./scripts/smoke-test.sh           # full: build, up, test, tear down
#   ./scripts/smoke-test.sh --keep    # leave the stack running afterwards
#
# Exit 0 = pass, non-zero = fail. Prints what failed.

set -uo pipefail

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

# Expected lab inventory after the workshop-deck-aligned restructure.
# Format: order|id (sorted lexicographically — same order containd
# returns from /api/scenarios).
EXPECTED=(
  "1.2|baseline-assessment"
  "1.3|segmentation-requirements"
  "1.4|remediation-planning"
  "2.2|firewall-implementation"
  "2.3|hardening-configurations"
  "2.3-bonus|vendor-rdp-compromise"
  "2.4|validation-evidence"
)
EXPECTED_COUNT="${#EXPECTED[@]}"

fail=0
note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; }
err()  { printf '  ✗ %s\n' "$1"; fail=1; }

cleanup() {
  if [ "$KEEP" = "0" ]; then
    note "tearing down"
    docker compose down -v >/dev/null 2>&1 || true
  else
    note "stack left running (--keep)"
  fi
  exit "$fail"
}
trap cleanup EXIT

# --- preflight --------------------------------------------------------------
note "validate compose syntax"
docker compose config -q && ok "docker-compose.yml" || err "docker-compose.yml"
docker compose -f docker-compose.release.yml config -q \
    && ok "docker-compose.release.yml" \
    || err "docker-compose.release.yml"

# Stale DB from before the order int→string change would break boot.
rm -f backend/data/rangerdanger.db
ok "stale labs.db cleared"

# --- bring up ---------------------------------------------------------------
note "build + up"
docker compose build --parallel >/tmp/smoke-build.log 2>&1 \
    && ok "build complete" \
    || { err "build failed; see /tmp/smoke-build.log"; exit 1; }
docker compose up -d >/tmp/smoke-up.log 2>&1 \
    && ok "compose up" \
    || { err "compose up failed; see /tmp/smoke-up.log"; exit 1; }

# --- wait for backend healthy ----------------------------------------------
note "wait for backend health (5min budget)"
for i in $(seq 1 30); do
  if curl -fsS http://localhost:8088/api/health >/dev/null 2>&1; then
    ok "backend healthy after ${i}0s"
    break
  fi
  sleep 10
  if [ "$i" = "30" ]; then
    err "backend never healthy"
    docker compose ps --format '{{.Service}} {{.Status}}'
    exit 1
  fi
done

# --- probe endpoints --------------------------------------------------------
note "probe /api/health and /api/build"
curl -fsS http://localhost:8088/api/health  | jq -e . >/dev/null && ok "/api/health JSON" || err "/api/health"
curl -fsS http://localhost:8088/api/build   | jq -e . >/dev/null && ok "/api/build JSON"  || err "/api/build"

# --- lab inventory ----------------------------------------------------------
note "validate lab inventory"
inv_json=$(curl -fsS http://localhost:8088/api/scenarios) \
    || { err "/api/scenarios fetch failed"; exit 1; }

actual_count=$(echo "$inv_json" | jq '.scenarios | length')
if [ "$actual_count" = "$EXPECTED_COUNT" ]; then
  ok "scenario count = $EXPECTED_COUNT"
else
  err "scenario count: expected $EXPECTED_COUNT, got $actual_count"
  echo "$inv_json" | jq -r '.scenarios[] | "  \(.order // "-")  \(.id)"'
fi

# Each expected (order,id) must appear.
for entry in "${EXPECTED[@]}"; do
  order="${entry%%|*}"
  id="${entry##*|}"
  found=$(echo "$inv_json" | jq -r --arg id "$id" --arg ord "$order" \
      '.scenarios[] | select(.id == $id and .order == $ord) | .id')
  if [ "$found" = "$id" ]; then
    ok "Lab $order  $id"
  else
    err "Lab $order  $id  (missing or wrong order)"
  fi
done

# Per-lab step counts (smoke check — catches accidental empty steps).
# .steps is a JSON-stringified blob with literal newlines inside the
# string descriptions, which makes `fromjson` reject it. Counting the
# "description":  occurrences gives an accurate step count without
# re-parsing (each step has exactly one, action items don't have any).
note "step counts per lab"
for entry in "${EXPECTED[@]}"; do
  id="${entry##*|}"
  steps=$(echo "$inv_json" | jq -r --arg id "$id" \
      '.scenarios[] | select(.id == $id) | .steps | [scan("\"description\":")] | length')
  if [ "$steps" -ge 3 ]; then
    ok "$id: $steps steps"
  else
    err "$id: $steps steps (expected >=3)"
  fi
done

# --- substation physics -----------------------------------------------------
# Field-device state -> RTAC -> OpenDSS power flow -> API, focused on the
# capacitor bank (see scripts/substation-smoke.sh). Health/inventory above
# don't exercise the physics.
note "substation: capacitor bank physics"
if ./scripts/substation-smoke.sh; then ok "substation physics (capbank -> OpenDSS)"; else err "substation physics"; fi

# --- service health ---------------------------------------------------------
note "compose services health"
docker compose ps --format '{{.Service}}\t{{.Status}}' | while read -r line; do
  echo "  $line"
done
healthy=$(docker compose ps --format '{{.Status}}' | grep -c '(healthy)')
if [ "$healthy" -ge 8 ]; then
  ok "$healthy services report (healthy)"
else
  err "only $healthy services healthy (expected ≥8)"
fi

# --- summary ----------------------------------------------------------------
note "summary"
if [ "$fail" = "0" ]; then
  echo "  ALL CHECKS PASSED"
else
  echo "  FAILED — see ✗ entries above"
fi
