#!/usr/bin/env bash
# Request the backend's validation report for the currently running policy.
# An applied plan is judged against its plan proof and compared with Hardened
# acceptance. This client never applies a policy or runs probes itself.
#
# Usage:
#   ./scripts/validation-report.sh
#   ./scripts/validation-report.sh --out report.md
#
# The backend report judges the live policy, not a policy applied by this
# script. Its summary.result reflects Hardened acceptance (not plan proof).

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
OUT_FILE=""

err() { echo "validation-report: $*" >&2; }

usage() {
  cat <<'USAGE'
Usage: validation-report.sh [--out|-o <file>]
       validation-report.sh --help

Requests the backend validation report for the policy currently running in
the ready range. By default, prints the report markdown to stdout.
USAGE
}

while (($#)); do
  case "$1" in
    --out|-o)
      if (($# < 2)) || [[ -z "$2" ]]; then
        err "$1 requires a path"
        exit 2
      fi
      OUT_FILE="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      err "unknown option: $1"
      usage >&2
      exit 2
      ;;
  esac
done

# Keep the shared range-response helpers, but accept any selected package
# whose phase is ready.
# shellcheck source=lib/range.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/range.sh"
range=$(range_status)
package=$(range_field "$range" package)
phase=$(range_field "$range" phase)
if [[ -z "$package" || "$phase" != "ready" ]]; then
  err "range is not ready — GET $API/api/range: $range"
  exit 1
fi

err "requesting validation report from $API/api/firewall/validation-report"
if ! response=$(curl -fsS --max-time 180 -X POST \
    -H 'Accept: application/json' \
    "$API/api/firewall/validation-report" 2>&1); then
  err "report request failed: $response"
  exit 1
fi

# summary.result is the backend's overall hardened-acceptance verdict; a
# planProof failure is independent and is described separately in the report.
if ! result=$(printf '%s' "$response" | python3 -c '
import json
import sys

try:
    report = json.load(sys.stdin)
    result = report["summary"]["result"]
    markdown = report["markdown"]
except (KeyError, TypeError, ValueError) as exc:
    print(f"invalid validation-report response: {exc}", file=sys.stderr)
    sys.exit(1)

if result not in ("PASS", "FAIL") or not isinstance(markdown, str) or not markdown:
    print("invalid validation-report response: expected summary.result PASS/FAIL and non-empty markdown",
          file=sys.stderr)
    sys.exit(1)

print(result)
'); then
  err "could not read a valid validation report"
  exit 1
fi

write_markdown() {
  printf '%s' "$response" | python3 -c '
import json
import sys

sys.stdout.write(json.load(sys.stdin)["markdown"])
'
}

if [[ -n "$OUT_FILE" ]]; then
  if ! write_markdown > "$OUT_FILE"; then
    err "could not write report to $OUT_FILE"
    exit 1
  fi
  err "report written to $OUT_FILE"
else
  if ! write_markdown; then
    err "could not write report to stdout"
    exit 1
  fi
fi

if [[ "$result" != "PASS" ]]; then
  err "backend report result: $result"
  exit 1
fi
