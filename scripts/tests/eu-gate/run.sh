#!/usr/bin/env bash
# Offline tests for scripts/eu-iec104-smoke.sh and its scripts/lib/eu-*.sh
# helpers. No Docker, backend or range is touched; the G3 decode cases need
# the host's tshark. Usage: bash scripts/tests/eu-gate/run.sh
set -uo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
files=0
failed=0
for test_file in "$dir"/test-*.sh; do
  files=$((files + 1))
  printf '\n== %s\n' "${test_file##*/}"
  if ! bash "$test_file"; then
    failed=$((failed + 1))
    printf 'FAILED %s\n' "${test_file##*/}" >&2
  fi
done
printf '\n%d test file(s), %d failed\n' "$files" "$failed"
[ "$files" -gt 0 ] && [ "$failed" -eq 0 ]
