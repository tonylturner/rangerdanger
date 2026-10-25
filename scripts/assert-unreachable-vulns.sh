#!/usr/bin/env bash
#
# Guards the "unreachable" govulncheck exceptions.
#
# When a GOID in the ALLOWED list of scripts/govulncheck-gate.sh is
# accepted specifically because its vulnerable package is NOT in this
# project's build graph, register that package and GOID in GUARDED below.
# This script verifies that documented exception remains true.
#
# The gate filters called findings by GOID, not by the package rationale.
# If a future dependency change makes a guarded package reachable, this
# script fails instead of silently keeping an unreachability exception.
#
# Raised by Codex review on PR #91.
#
# Usage: scripts/assert-unreachable-vulns.sh [module-dir ...]
#        defaults to: backend services dnp3go iec104go

set -uo pipefail

# package-prefix : the GOIDs whose exception depends on it staying out
# Prefix match, so subpackages count as present too. The current allowlist
# has no entries that rely on an unreachable-package rationale.
GUARDED=()

if [ "$#" -eq 0 ]; then
  modules=(backend services dnp3go iec104go)
else
  modules=("$@")
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fail=0

for dir in "${modules[@]}"; do
  mod_path="$repo_root/$dir"
  if [ ! -f "$mod_path/go.mod" ]; then
    echo "::error::$dir has no go.mod - cannot verify unreachability"
    fail=1
    continue
  fi

  deps="$(cd "$mod_path" && go list -deps ./... 2>/dev/null)"
  if [ -z "$deps" ]; then
    # An empty dep list would make every check below vacuously pass,
    # which is exactly the silent-green failure this script exists to
    # prevent. Treat it as an error, not a pass.
    echo "::error::go list -deps produced no output for $dir"
    fail=1
    continue
  fi

  for entry in "${GUARDED[@]-}"; do
    [ -n "$entry" ] || continue
    pkg="${entry%%:*}"
    goids="${entry#*:}"
    if printf '%s\n' "$deps" | grep -q "^${pkg}\(/\|$\)"; then
      echo "::error::$dir now imports ${pkg}"
      echo ""
      echo "  The govulncheck allowlist accepts ${goids}"
      echo "  on the grounds that this package is not in the build"
      echo "  graph. That is no longer true, so the exception is void."
      echo ""
      echo "  Fix by removing the import, or - if the import is"
      echo "  intended - drop those GOIDs from ALLOWED in"
      echo "  scripts/govulncheck-gate.sh and re-triage them in"
      echo "  docs/security-known-issues.md on their real merits."
      fail=1
    fi
  done
done

if [ "$fail" -eq 0 ]; then
  echo "unreachable-exception packages are absent from the build graph"
fi

exit "$fail"
