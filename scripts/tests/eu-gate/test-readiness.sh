#!/usr/bin/env bash
# Offline readiness transitions; pure dependency-injected clocks and transport.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
python3 -m unittest discover -s "$root/scripts/tests" -p test_containd_readiness.py
