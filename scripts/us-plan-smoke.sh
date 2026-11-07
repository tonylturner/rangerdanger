#!/usr/bin/env bash
# Compiled US plans: full, partial, then every enforce/detect catalog action.
# Run from the repository root against a ready, disposable US range.
set -euo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/range.sh
. "$SCRIPT_DIR/lib/range.sh"
if ! range_is_ready us-dnp3-substation; then
  echo "range is not us-dnp3-substation ready — GET $API/api/range: $(range_status)" >&2
  exit 1
fi

oracle=scripts/tests/fixtures
catalog=frontend/lib/testdata/plans/us-full.json
base=build/smoke-receipts
mkdir -p "$base/rd01-singletons"
restore_weak() {
  local rc=$?
  trap - EXIT
  if ! curl -fsS --max-time 60 -X POST -H 'Content-Type: application/json' \
      -d '{"config":"weak"}' "$API/api/firewall/apply" >"$base/rd01-singletons/restore.json" \
      || ! python3 "$SCRIPT_DIR/lib/containd_readiness.py" --backend-receipt \
        <"$base/rd01-singletons/restore.json"; then
    echo "US compiled-plan cleanup failed" >&2
    rc=1
  fi
  exit "$rc"
}
trap restore_weak EXIT

run_case() {
  local golden="$1" receipts="$2"
  printf '\n=== compiled plan: %s ===\n' "$golden"
  mkdir -p "$receipts"
  jq -L "$oracle" 'include "plan-gate"; plan_request' "$golden" >"$receipts/plan.json"
  curl -fsS --max-time 60 -X POST -H 'Content-Type: application/json' \
    --data-binary @"$receipts/plan.json" "$API/api/firewall/apply-plan" >"$receipts/apply.json"
  jq -e -L "$oracle" --slurpfile golden "$golden" \
    'include "plan-gate"; plan_applied($golden[0])' "$receipts/apply.json"
  python3 "$SCRIPT_DIR/lib/containd_readiness.py" --backend-receipt <"$receipts/apply.json"
  curl -fsS --max-time 90 -X POST "$API/api/firewall/validation-report" >"$receipts/report.json"
  jq -e -L "$oracle" --slurpfile golden "$golden" \
    'include "plan-gate"; plan_report($golden[0])' "$receipts/report.json"
}

run_case "$catalog" "$base/rd01"
run_case frontend/lib/testdata/plans/us-partial.json "$base/rd01-partial"
actions=$(jq -er '[.actions[] | select(.kind == "enforce" or .kind == "detect") | .id]
  | if length > 0 then .[] else error("catalog has no technical actions") end' "$catalog")
for action in $actions; do
  run_case "frontend/lib/testdata/plans/us-$action.json" "$base/rd01-singletons/$action"
done
