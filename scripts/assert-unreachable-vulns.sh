#!/usr/bin/env bash
#
# Guards the "unreachable" govulncheck exceptions.
#
# Some GOIDs in the ALLOWED list of scripts/govulncheck-gate.sh are
# accepted specifically because the vulnerable package is NOT in this
# project's build graph - not because of any mitigation, and not
# because upstream shipped a fix. That rationale is written down in
# docs/security-known-issues.md.
#
# The gate itself filters findings by GOID alone. So if a future code
# or dependency change ever pulls one of these packages in, govulncheck
# would report the same GOID, the GOID would still be allowlisted, and
# the gate would stay green while the documented justification had
# quietly become false. This script is the missing half of that
# control: it fails if a package whose exception rests on
# unreachability ever appears in the build graph.
#
# Raised by Codex review on PR #91.
#
# Usage: scripts/assert-unreachable-vulns.sh [module-dir ...]
#        defaults to: backend services dnp3go

set -uo pipefail

# package-prefix : the GOIDs whose exception depends on it staying out
# Prefix match, so subpackages (openpgp/packet, daemon/logger, ...)
# count as present too.
GUARDED=(
  "golang.org/x/crypto/openpgp:GO-2026-5932"
  "github.com/docker/docker/daemon:GO-2026-5617, GO-2026-5668, GO-2026-5746"
)

modules=("$@")
if [ ${#modules[@]} -eq 0 ]; then
  modules=(backend services dnp3go)
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

  for entry in "${GUARDED[@]}"; do
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
      echo "  .github/workflows/ci.yml and re-triage them in"
      echo "  docs/security-known-issues.md on their real merits."
      fail=1
    fi
  done
done

if [ "$fail" -eq 0 ]; then
  echo "unreachable-exception packages are absent from the build graph"
fi

exit "$fail"
