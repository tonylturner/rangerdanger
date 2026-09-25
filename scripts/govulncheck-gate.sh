#!/usr/bin/env bash

set -uo pipefail

ALLOWED="${ALLOWED-GO-2026-4887 GO-2026-4883 GO-2026-5617 GO-2026-5668 GO-2026-5746 GO-2026-5932}"

modules=("$@")
if [ ${#modules[@]} -eq 0 ]; then
  modules=(backend services dnp3go)
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)" || {
  echo "ERROR: could not create a temporary directory for govulncheck output" >&2
  exit 1
}
trap 'rm -rf "$tmp_dir"' EXIT

report_error() {
  if [ -n "${GITHUB_ACTIONS:-}" ]; then
    echo "::error::$*"
  else
    echo "ERROR: $*"
  fi
}

allowed_sorted="$(printf '%s\n' "$ALLOWED" | tr '[:space:]' '\n' | sed '/^$/d' | sort -u)"
fail=0

for index in "${!modules[@]}"; do
  dir="${modules[$index]}"
  module_path="$repo_root/$dir"
  output="$tmp_dir/$index.json"

  echo ""
  echo "=== govulncheck: $dir ==="

  if [ ! -f "$module_path/go.mod" ]; then
    report_error "$dir has no go.mod"
    fail=1
    continue
  fi

  (cd "$module_path" && govulncheck -format json ./...) > "$output" 2>&1 || true
  if [ ! -s "$output" ]; then
    report_error "govulncheck produced no output for $dir"
    fail=1
    continue
  fi

  # Match the CI filter: count only findings with a nonempty trace.
  if ! found="$(jq -r '.finding | select(has("osv") and (.trace // [] | length) > 0) | .osv' "$output" 2>/dev/null | sort -u)"; then
    report_error "govulncheck produced invalid JSON for $dir"
    fail=1
    continue
  fi

  if [ -z "$found" ]; then
    echo "  no findings"
    continue
  fi

  unexpected="$(comm -23 <(printf '%s\n' "$found" | sort -u) <(printf '%s\n' "$allowed_sorted"))"
  count="$(printf '%s\n' "$found" | wc -l | tr -d ' ')"
  if [ -n "$unexpected" ]; then
    report_error "Unexpected vulnerabilities in $dir:"
    printf '%s\n' "$unexpected" | sed 's/^/  - /'
    echo ""
    echo "Allowlist (docs/security-known-issues.md): $ALLOWED"
    echo ""
    echo "Either (1) add a triage entry to docs/security-known-issues.md"
    echo "and the ALLOWED list in scripts/govulncheck-gate.sh, or (2) update"
    echo "the affected dependency."
    fail=1
  else
    echo "  $count finding(s), all allowlisted"
  fi
done

exit "$fail"
