#!/usr/bin/env bash
# Reconnect/GI proves protocol refresh, not physical measurement validity.
# shellcheck disable=SC2329 # stubs are called by the sourced gate
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

RECOVERED=$(jq -c '
  (.points[] | select(.id == "feeder-breaker-position") | .value) = "open"
  | (.points[] | select(.id == "feeder-protection-trip") | .value) = true
  | (.points[] | select(.id == "feeder-pf")) |=
      (.quality.iv = true | .stale_reason = "measurement_invalid")
' "$T_FIXTURES/operator-gi-untimed.json")
TRUTH=$(jq -c '(.devices["breaker-1"] |= (.position = "open" | .protection_tripped = true))
  | .devices["oltc-1"] = {tap:0}' \
  "$T_FIXTURES/field-baseline.json")
OPERATOR="$RECOVERED"
# shellcheck disable=SC2034 # gate precondition
g7_loss_verified=1
eu_apply_named() { :; }
eu_get_field() { API_BODY="$TRUTH"; }
# shellcheck disable=SC2034 # consumed by eu_gate_G9
eu_get_operator() { API_BODY="$OPERATOR"; }
# Exercise the gate's real predicate and shape check without a live poll.
eu_wait_for_operator() {
  if ! jq -e "$EU_JQ_DEFS $1" <<<"$OPERATOR" >/dev/null; then
    fail "$3 predicate rejected operator state"
    return 1
  fi
  eu_assert_operator_shape "$OPERATOR" "$3"
}

t_run eu_gate_G9
t_expect_fails 0 "G9 accepts refreshed IV measurement_invalid PF after G8 trips CB1"
t_expect_out "restored IEC104 observations after GI" "recovery actually passed the predicate"
t_expect_out "equals open/tripped physical state" "open/tripped equality remains required"

OPERATOR=$(jq -c '(.points[] | select(.id == "feeder-pf")) |=
  (.quality.iv = false | .stale_reason = null)' <<<"$RECOVERED")
t_run eu_gate_G9
t_expect_fails 0 "an entirely GOOD inventory still passes G9"

for expression in \
  '.points[-1].quality.nt = true' \
  '.points[-1].value = null' \
  '.points[-1].stale_reason = "link_lost"' \
  '.points[-1].quality.iv = true' \
  '.points[-1] |= (.quality.iv = true | .stale_reason = "device_unreachable")' \
  '.points[-1].quality.sb = true' \
  '.points[-1].quality.bl = true' \
  '(.points[] | select(.id == "feeder-pf") | .quality.ov) = true' \
  '(.points[] | select(.id == "feeder-pf") | .quality.iv) = false' \
  'del(.points[-1])' \
  '.points += [.points[0]]' \
  '.link.state = "disconnected"' \
  '.link.gi.state = "running"'; do
  OPERATOR=$(jq -c "$expression" <<<"$RECOVERED")
  t_run eu_gate_G9
  t_expect_fails 1 "G9 rejects incomplete or unrefreshed recovery: $expression"
done
OPERATOR=$(jq -c '(.points[] | select(.id == "feeder-breaker-position") | .value) = "closed"' <<<"$RECOVERED")
t_run eu_gate_G9
t_expect_fails 1 "G9 rejects operator/truth disagreement despite refreshed inventory"

OPERATOR=$(jq -c '(.points[] | select(.id == "oltc-1-tap") | .value.value) = -1' <<<"$RECOVERED")
TRUTH=$(jq -c '.devices["oltc-1"].tap = -1' <<<"$TRUTH")
t_run eu_gate_G9
t_expect_fails 0 "G9 learns the autonomous OLTC step after reconnect and GI"
OPERATOR="$RECOVERED"
t_run eu_gate_G9
t_expect_fails 1 "G9 rejects a retained pre-disconnect tap even when breaker truth agrees"
t_expect_out "physical state and OLTC tap" "the truth comparison includes the OLTC tap"

t_finish
