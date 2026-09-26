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

if ! jq -e '
  (.total_tests | type == "number") and
  (.passed | type == "number") and
  (.failed | type == "number") and
  (.auto_passed | type == "number") and
  (.reset_failures | type == "number") and
  (.scenarios | type == "array") and
  ([.scenarios[] | (.passed | type == "boolean") and
                    (.reset_ok | type == "boolean") and
                    (.steps | type == "array")] | all)
' <<<"$response" >/dev/null; then
  err "workshop test-suite returned an invalid response"
  exit 1
fi

total=$(jq -r '.total_tests' <<<"$response")
passed=$(jq -r '.passed' <<<"$response")
failed=$(jq -r '.failed' <<<"$response")
auto_passed=$(jq -r '.auto_passed' <<<"$response")
reset_failures=$(jq -r '.reset_failures' <<<"$response")

while IFS= read -r scenario; do
  order=$(jq -r '.order // "-"' <<<"$scenario")
  id=$(jq -r '.scenario_id // "-"' <<<"$scenario")
  step_count=$(jq -r '.steps | length' <<<"$scenario")
  executed=$(jq -r '[.steps[] | select(.auto_pass != true)] | length' <<<"$scenario")
  reset_ok=$(jq -r '.reset_ok' <<<"$scenario")
  reset_detail=$(jq -r '(.reset_detail // "") | gsub("[\\r\\n]+"; " ")' <<<"$scenario")
  reset_suffix="${reset_detail:+ $reset_detail}"
  if [ "$(jq -r '.passed' <<<"$scenario")" = "true" ]; then
    mark="✓"
  else
    mark="✗"
  fi
  printf '  %s %s %s: %s steps, %s executed, reset_ok=%s%s\n' \
    "$mark" "$order" "$id" "$step_count" "$executed" "$reset_ok" "$reset_suffix"

  jq -r '.steps[] | select(.passed == false) |
    ((.step_title // "(untitled)") | gsub("[\\r\\n]+"; " ")) as $title |
    ((.detail // "(no detail)") | gsub("[\\r\\n]+"; " ")) as $detail |
    "  ✗ \($title): \($detail)"' \
    <<<"$scenario"
done < <(jq -c '.scenarios[]' <<<"$response")

note "summary"
echo "  passed: $passed / $total ($auto_passed auto-passed), reset failures: $reset_failures"
if [ "$failed" -gt 0 ] || [ "$reset_failures" -gt 0 ]; then
  exit 1
fi
