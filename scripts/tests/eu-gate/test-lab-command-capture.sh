#!/usr/bin/env bash
# This CI job installs tshark; release-tooling's Python-only job does not.
set -euo pipefail
command -v tshark >/dev/null
python3 -m unittest scripts.tests.test_lab_commands_capture.ReaderBoundaryTests.test_real_tshark_exposes_sll_ifindex_and_short_syn_rows_with_reviewed_oracles
