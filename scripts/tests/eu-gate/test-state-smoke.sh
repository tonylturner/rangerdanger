#!/usr/bin/env bash
# Included automatically by run.sh's test-*.sh discovery. No range or browser.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
python3 "$root/scripts/tests/eu-gate/test_state_smoke.py"
