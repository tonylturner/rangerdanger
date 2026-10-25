#!/usr/bin/env bash
# Request the backend's validation report for the currently running policy.
# An applied plan is judged against its plan proof and compared with Hardened
# acceptance. This client never applies a policy or runs probes itself.
#
# Usage:
#   ./scripts/validation-report.sh
#   ./scripts/validation-report.sh --out report.md
#
# For an applied plan, the exit verdict follows plan proof; without plan
# proof, it follows Hardened acceptance. Hardened remains the comparison.

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

# Plan proof and Hardened acceptance are independent judgments. For a plan,
# proof decides whether the running policy passes; otherwise use acceptance.
if ! decision=$(printf '%s' "$response" | python3 -c '
import json
import sys

try:
    report = json.load(sys.stdin)
    proof = report["planProof"]
    applicable = proof["applicable"]
    proof_pass = proof["pass"]
    acceptance_pass = report["acceptance"]["pass"]
    markdown = report["markdown"]
except (KeyError, TypeError, ValueError) as exc:
    print(f"invalid validation-report response: {exc}", file=sys.stderr)
    sys.exit(1)

if (not isinstance(applicable, bool) or not isinstance(proof_pass, bool)
        or not isinstance(acceptance_pass, bool)
        or not isinstance(markdown, str) or not markdown):
    print("invalid validation-report response: expected boolean planProof.applicable, "
          "planProof.pass, acceptance.pass, and non-empty markdown",
          file=sys.stderr)
    sys.exit(1)

if applicable:
    print(("PASS" if proof_pass else "FAIL") + "\nplan proof")
else:
    print(("PASS" if acceptance_pass else "FAIL") + "\nHardened acceptance")
'); then
  err "could not read a valid validation report"
  exit 1
fi

result="${decision%%$'\n'*}"
judgment="${decision#*$'\n'}"

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

err "$judgment: $result"
if [[ "$result" != "PASS" ]]; then
  exit 1
fi
