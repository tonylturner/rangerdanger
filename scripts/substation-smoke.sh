#!/usr/bin/env bash
# Substation physics + closed-loop control smoke.
#
# Confirms field-device state flows through the RTAC -> OpenDSS power-flow and
# back to the API, and that the RTAC's AUTO-mode control loops actually close:
#
#   1. all four field devices report state
#   2. capacitor bank physics (MANUAL): switch in injects VARs -> power factor
#      rises, and the state echoes through both the device map and the
#      electrical results
#   3. cap bank AUTO loop: with auto enabled and the cap out, the RTAC
#      switches it back in to correct the lagging power factor
#   4. regulator AVR loop: after a manual tap sag, enabling auto steps the tap
#      back up toward the voltage setpoint
#
# MANUAL mode is used for the deterministic A/B in step 2 precisely because
# AUTO would otherwise override the switch-out (which is itself the point of
# step 3). Assumes the stack is up and the backend is healthy.
# Exit 0 = pass, non-zero = fail.

set -uo pipefail

API="${API:-http://localhost:8088}"
sub="$API/api/substation"
fail=0
ok()  { printf '  \xe2\x9c\x93 %s\n' "$1"; }
err() { printf '  \xe2\x9c\x97 %s\n' "$1"; fail=1; }

cmd() { curl -fsS -X POST -H 'Content-Type: application/json' -d "{\"command\":\"$2\"}" "$sub/command/$1" >/dev/null; }
get() { curl -fsS "$sub/state"; }
val() { get | jq -r "$1"; }

# wait_until <jq-bool-expr> <timeout-s>: poll the state until the expression
# evaluates true, or fail after the timeout. Lets the 2s control loop act
# without baking in fixed sleeps that make CI flaky.
wait_until() {
  local expr="$1" t="${2:-15}" i
  for ((i = 0; i < t; i++)); do
    [ "$(get | jq -r "$expr")" = "true" ] && return 0
    sleep 1
  done
  return 1
}

# 1. State endpoint exposes the full field-device set.
state=$(get) || { echo "  cannot reach $sub/state"; exit 1; }
for d in relay recloser regulator capbank; do
  echo "$state" | jq -e ".devices.$d" >/dev/null 2>&1 && ok "devices.$d present" || err "devices.$d missing"
done

# Clear the cap's 6-operation switch-lockout up front: this script performs
# several switch ops, so without a reset a repeated local run (or a run after
# manual testing) would hit lockout and the switch/auto checks would be
# (correctly) refused. A fresh CI stack starts at zero, so this is a no-op there.
cmd capbank reset_lockout

# 2. Capacitor bank physics A/B in MANUAL (so AUTO can't override the switch-out).
cmd capbank set_manual
cmd capbank switch_out; sleep 4
pf_out=$(val '.electrical.power_factor')
cap_out=$(val '.devices.capbank.switched_in')
cmd capbank switch_in; sleep 4
s=$(get)
pf_in=$(echo "$s"   | jq -r '.electrical.power_factor')
cap_in=$(echo "$s"  | jq -r '.devices.capbank.switched_in')
echo_in=$(echo "$s" | jq -r '.electrical.capbank_switched_in')

[ "$cap_out" = "false" ]                          && ok "manual switch_out holds cap out"          || err "cap stayed in after manual switch_out (cap_out=$cap_out)"
[ "$cap_in" = "true" ] && [ "$echo_in" = "true" ] && ok "manual switch_in echoes through OpenDSS"  || err "switch_in not reflected (cap_in=$cap_in echo=$echo_in)"
if [ -n "$pf_out" ] && [ -n "$pf_in" ] && awk "BEGIN{exit !($pf_in > $pf_out)}"; then
  ok "switch-in raises power factor ($pf_out -> $pf_in)"
else
  err "power factor did not rise on switch-in ($pf_out -> $pf_in)"
fi

# 3. Capacitor bank AUTO loop: out + enable auto -> RTAC switches it back in.
cmd capbank switch_out; sleep 2
cmd capbank set_auto
if wait_until '.devices.capbank.switched_in' 15; then
  ok "cap AUTO re-engages to correct power factor"
else
  err "cap AUTO did not switch in within 15s"
fi

# 4. Regulator AVR loop: manual sag, then auto steps the tap back up.
cmd regulator set_manual
for _ in 1 2 3; do cmd regulator lower_tap; done
if ! wait_until '(.electrical.regulator_tap <= -2)' 10; then
  err "manual tap sag did not register"
fi
tap_lo=$(val '.electrical.regulator_tap')
cmd regulator set_auto
if wait_until "(.electrical.regulator_tap > $tap_lo)" 20; then
  ok "regulator AVR raises tap from $tap_lo back toward setpoint"
else
  err "regulator AVR did not correct the sag (tap stuck at $tap_lo)"
fi

# Leave both devices in AUTO — their normal operating mode.
if [ "$fail" = "0" ]; then echo "  substation-smoke PASS"; else echo "  substation-smoke FAIL"; fi
exit "$fail"
