#!/usr/bin/env bash
# Offline Docker coverage and watchdog regressions. Never contact a daemon.
set -uo pipefail
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python3 "$dir/test_docker_bounds.py"
