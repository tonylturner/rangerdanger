#!/usr/bin/env bash
# Observation-only RTAC host controls and OT Ops -> physics transit check.
set -euo pipefail
exec python3 "$(dirname "${BASH_SOURCE[0]}")/lib/us-rtac-check.py" "$@"
