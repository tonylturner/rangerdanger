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
#   5. pausing OpenDSS makes RTAC physics stale; unpausing restores freshness
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

physics_container=rangerdanger-opendss-sim
physics_paused=0
unpause_physics() {
  if docker unpause "$physics_container" >/dev/null 2>&1; then
    physics_paused=0
  elif [ "$physics_paused" = "1" ]; then
    err "could not unpause $physics_container"
  fi
}

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

wait_for_physics_healthy() {
  local health_state i
  for ((i = 0; i < 30; i++)); do
    health_state=$(docker inspect -f '{{.State.Health.Status}}' "$physics_container" 2>/dev/null) || health_state=""
    [ "$health_state" = "healthy" ] && return 0
    sleep 1
  done
  return 1
}

cleanup_physics() {
  local was_paused="$physics_paused"
  unpause_physics
  if [ "$was_paused" = "1" ] && ! wait_for_physics_healthy; then
    err "OpenDSS container did not become healthy within 30s during cleanup"
  fi
}
trap cleanup_physics EXIT

# These checks assert the US range. Refuse another package or a range
# mid-switch: GET /api/range must report us-dnp3-substation ready.
# shellcheck source=lib/range.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/range.sh"
if range_is_ready us-dnp3-substation; then
  ok "range us-dnp3-substation ready"
else
  err "range is not us-dnp3-substation ready — GET $API/api/range: $(range_status)"
  exit 1
fi

# 1. State endpoint exposes the full field-device set.
state=$(get) || { echo "  cannot reach $sub/state"; exit 1; }
for d in relay recloser regulator capbank; do
  echo "$state" | jq -e ".devices.$d" >/dev/null 2>&1 && ok "devices.$d present" || err "devices.$d missing"
done

# Reset the feeder to a clean energized baseline first. Earlier smoke steps
# (lab-commands, events) and any prior manual testing run documented commands
# that trip the breaker, open the recloser, drive the regulator to an extreme
# tap, or inject faults — which would leave the feeder de-energized (PF and
# voltage read 0) with the tap pinned, and every assertion below would run
# against a dead feeder. Also clears the cap's 6-op switch lockout.
cmd relay clear_fault; cmd relay unlock; cmd relay close
cmd recloser clear_fault; cmd recloser reset_lockout; cmd recloser enable_reclose; cmd recloser close
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"command":"set_tap","value":0}' "$sub/command/regulator" >/dev/null
cmd regulator set_auto
cmd capbank reset_lockout
sleep 5  # let the RTAC poll + OpenDSS re-solve the energized state
state=$(get) || { err "cannot refresh $sub/state after baseline reset"; state='{}'; }
echo "$state" | jq -e '.physics.stale == false' >/dev/null 2>&1 \
  && ok "physics is fresh" || err "physics is stale after baseline reset"
echo "$state" | jq -e '.electrical.converged == true' >/dev/null 2>&1 \
  && ok "electrical solve converged" || err "electrical solve is not converged"
if echo "$state" | jq -e '
  (.electrical.solved_at | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) as $solved_at
  | (now - $solved_at) as $age
  | ($age >= -10 and $age <= 10)
' >/dev/null 2>&1; then
  ok "electrical solve timestamp is within 10s"
else
  err "electrical solve timestamp is missing, invalid, or older than 10s"
fi

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
# Wait window exceeds the cap auto-switch dwell (capAutoDwellSec in rtac-sim,
# the anti-hunt rate limit) so this isn't a race with the last auto switch.
cmd capbank switch_out; sleep 2
cmd capbank set_auto
if wait_until '.devices.capbank.switched_in' 35; then
  ok "cap AUTO re-engages to correct power factor"
else
  err "cap AUTO did not switch in within 35s"
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

# 5. Stopping the physics engine must transition the RTAC to stale and recover
# after the engine is resumed. The EXIT trap is a final safety net if any
# assertion or command exits early while the container is paused.
if docker pause "$physics_container" >/dev/null; then
  physics_paused=1
  if wait_until '.physics.stale' 15; then
    ok "physics becomes stale while OpenDSS is paused"
  else
    err "physics did not become stale within 15s while OpenDSS was paused"
  fi
else
  err "could not pause $physics_container"
fi
unpause_physics
# Docker reports a paused container unhealthy until its next successful health probe after unpause.
if wait_for_physics_healthy; then
  ok "OpenDSS container healthy again after unpause"
else
  err "OpenDSS container did not become healthy within 30s after unpause"
fi
if wait_until '.physics.stale | not' 15; then
  ok "physics freshness recovers after OpenDSS resumes"
else
  err "physics did not recover within 15s after OpenDSS resumed"
fi

# Leave both devices in AUTO — their normal operating mode.
if [ "$fail" = "0" ]; then echo "  substation-smoke PASS"; else echo "  substation-smoke FAIL"; fi
exit "$fail"
