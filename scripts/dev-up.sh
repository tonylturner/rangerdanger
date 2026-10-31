#!/usr/bin/env bash
#
# Build RangerDanger from source and start it: the platform, then the
# range through the backend (POST /api/range), waiting until the range
# reports ready. A thin wrapper over
# `./setup.sh --from-source --skip-firewall-gate`: the workshop gate
# applies and resets firewall policy, which a dev bring-up (and the smoke
# gates that follow it) must not do. Extra arguments pass through to
# setup.sh, e.g. `--package <id>` for the first-start range.
#
# Stop with ./scripts/dev-down.sh.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

exec "$ROOT_DIR/setup.sh" --from-source --skip-firewall-gate "$@"
