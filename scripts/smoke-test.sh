#!/usr/bin/env bash
# End-to-end lab smoke test.
#
# Brings RangerDanger up from source (./scripts/dev-up.sh: platform, then
# the range through the backend), checks that the US range is ready, hits
# the API endpoints, validates that the right lab inventory is loaded
# with the expected IDs and order strings, and confirms sims report
# healthy.
#
# Usage:
#   ./scripts/smoke-test.sh           # full: build, up, test, tear down
#   ./scripts/smoke-test.sh --keep    # leave the lab running afterwards
#
# Exit 0 = pass, non-zero = fail. Prints what failed.

set -uo pipefail

KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
cd "$ROOT_DIR" || exit 1
API="http://localhost:8088"

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

# Label-only Compose calls (ps) run from an empty directory: from the repo
# root Compose would load docker-compose.yml as the model.
WORKDIR=$(mktemp -d)

cleanup() {
  if [ "$KEEP" = "0" ]; then
    note "tearing down"
    ./scripts/dev-down.sh >/dev/null 2>&1 || err "teardown left resources behind (run ./scripts/dev-down.sh)"
  else
    note "lab left running (--keep)"
  fi
  rmdir "$WORKDIR"
  exit "$fail"
}
trap cleanup EXIT

# --- preflight --------------------------------------------------------------
# Platform files under the platform project, every package's range files
# under the range project; H is the project directory, as setup runs them.
note "validate compose syntax"
compose_config() { # <project> <file>
  RANGERDANGER_ROOT="$ROOT_DIR" docker compose -p "$1" --project-directory "$ROOT_DIR" \
    -f "$ROOT_DIR/$2" config -q
}
for f in docker-compose.yml docker-compose.release.yml; do
  if compose_config rangerdanger-platform "$f"; then ok "$f"; else err "$f"; fi
done
for f in lab-definitions/packages/*/compose.source.yml lab-definitions/packages/*/compose.release.yml; do
  if compose_config rangerdanger "$f"; then ok "$f"; else err "$f"; fi
done

# The backend bind-mounts this database. Stop it before unlinking the file so
# it cannot keep using a stale SQLite handle; the bring-up below starts it on
# the clean database and runs migrations.
backend=$(docker ps -q --filter label=com.docker.compose.project=rangerdanger-platform \
    --filter label=com.docker.compose.service=backend)
if [ -n "$backend" ] && ! docker stop "$backend" >/dev/null; then
  err "backend could not be stopped; stale database not cleared"
  exit 1
fi
rm -f backend/data/rangerdanger.db
ok "stale database cleared"

# --- bring up ---------------------------------------------------------------
# dev-up builds every image, starts the platform, waits for the backend and
# for POST /api/range to bring the range to ready.
note "build + up"
if ./scripts/dev-up.sh >/tmp/smoke-up.log 2>&1; then
  ok "build, platform up, range ready"
else
  err "dev-up failed; see /tmp/smoke-up.log"
  exit 1
fi

# --- range -----------------------------------------------------------------
# Every assertion below is about the US range.
note "range"
range=$(curl -fsS "$API/api/range") || { err "GET /api/range failed"; exit 1; }
if [ "$(echo "$range" | jq -r '.package + " " + .phase')" = "us-dnp3-substation ready" ]; then
  ok "range us-dnp3-substation ready"
else
  err "range is not us-dnp3-substation ready: $(echo "$range" | jq -c '{package, phase, error}')"
  exit 1
fi

# --- probe endpoints --------------------------------------------------------
note "probe /api/health and /api/build"
if curl -fsS "$API/api/health" | jq -e . >/dev/null; then
  ok "/api/health JSON"
else
  err "/api/health"
fi
if curl -fsS "$API/api/build" | jq -e . >/dev/null; then
  ok "/api/build JSON"
else
  err "/api/build"
fi

# --- lab inventory ----------------------------------------------------------
note "validate lab inventory"
inv_json=$(curl -fsS "$API/api/scenarios") \
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
# Platform + range: the same services the single project used to list.
compose_ps() {
  (cd "$WORKDIR" && docker compose -p rangerdanger-platform ps "$@" && docker compose -p rangerdanger ps "$@")
}
compose_ps --format '{{.Service}}\t{{.Status}}' | while read -r line; do
  echo "  $line"
done
healthy=$(compose_ps --format '{{.Status}}' | grep -c '(healthy)')
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
