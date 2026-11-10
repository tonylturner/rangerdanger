#!/usr/bin/env bash
# Both packages use the same reviewed inventory, capture and cleanup lifecycle.
# Extraction/pins fail closed BEFORE live work. Disposable ranges only.
set -euo pipefail
exec python3 "$(dirname "${BASH_SOURCE[0]}")/lib/lab_commands_smoke.py" "$@"
