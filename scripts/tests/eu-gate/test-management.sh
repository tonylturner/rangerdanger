#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
python3 "$ROOT/scripts/tests/eu-gate/test_management.py"
