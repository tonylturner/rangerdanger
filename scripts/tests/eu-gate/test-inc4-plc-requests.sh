#!/usr/bin/env bash
# Automatic taps need new PLC-origin Modbus requests, including restraint at
# limits: ignored past-limit requests must not disappear behind an end state.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
AUDIT='{"generation":42,"oldest_seq":1,"latest_seq":21,"entries":[{"seq":21,"operation":"plc_request","result":"accepted","context":{"protocol":"modbus","peer":"10.30.30.30:1234"}}]}'
EDIT='.'
# shellcheck disable=SC2034 # read by eu_plc_requests
eu_management() { API_BODY=$(printf '%s' "$AUDIT" | jq -c "$EDIT"); }
t_run eu_plc_requests 20 1 42
t_expect_fails 0 "one new accepted PLC write proves the automatic step"
for EDIT in '.entries[0].seq = 20' '.generation = 43' \
  '.entries[0].context.peer = "10.30.30.20:1234"' '.entries[0].result = "ignored"' \
  '.entries += .entries' '.entries += [.entries[0] | .result = "ignored" | .detail = "limit"]'; do
  t_run eu_plc_requests 20 1 42
  t_expect_fails 1 "not valid automatic regulation evidence: $EDIT"
done
t_finish
