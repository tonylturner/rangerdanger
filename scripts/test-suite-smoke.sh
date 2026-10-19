#!/usr/bin/env bash
# Workshop test-suite smoke gate.
#
# POSTs to the workshop test-suite endpoint, which resets the lab state
# before each scenario. Run only against a disposable/test stack.
#
# Exit 0 = every scenario and reset passed; non-zero = a failure.

set -euo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"

note() { printf '\n=== %s ===\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; }
err()  { printf '  ✗ %s\n' "$1" >&2; }

note "gate 1: backend health preflight"
if ! curl -fsS --max-time 10 "$API/api/health" >/dev/null 2>&1; then
  err "backend $API not healthy — bring stack up first"
  exit 1
fi
ok "backend healthy"

note "gate 2: workshop test-suite (resets lab state)"
if ! response=$(curl -fsS --max-time 900 -X POST "$API/api/workshop/test-suite"); then
  err "workshop test-suite request failed"
  exit 1
fi

# Independently reconcile every row and all totals against the mounted YAML.
# Manual and decision steps are reported separately, never called execution.
python3 scripts/lib/suite-accounting.py <<<"$response"
