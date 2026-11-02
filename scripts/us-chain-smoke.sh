#!/usr/bin/env bash
# Observe the US DNP3 -> feeder outage -> recovery chain on an existing range.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
exec python3 "$ROOT/scripts/lib/us_chain_smoke.py" "$@"
