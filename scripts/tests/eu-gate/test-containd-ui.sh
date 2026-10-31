#!/usr/bin/env bash
# The shared offline probe/route tests also run in the targeted EU suite.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

(cd "$T_REPO" && python3 -m unittest scripts.tests.test_containd_ui_prefix scripts.tests.test_containd_ui_turbopack) || exit 1

# shellcheck disable=SC2329 # dispatched by G23
python3() {
  printf 'probe %s %s\n' "$1" "$2"
  return 1
}
# shellcheck disable=SC2329 # a failed UI probe must not get as far as policy changes
eu_apply_named() { printf 'unexpected policy change\n'; }
t_run eu_gate_G23
t_expect_fails 1 "G23 fails on an incomplete containd UI check"
t_expect_out "containd-ui-probe.py $API" "G23 checks the portal origin through the shared probe"
t_expect_no_out "unexpected policy change" "the UI check runs before policy mutation"
t_finish
