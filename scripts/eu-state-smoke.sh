#!/usr/bin/env bash
# Opt-in EU electrical/CC/DOM state matrix. Never pauses/restarts services or applies policy.
# shellcheck source-path=SCRIPTDIR
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if [ "${1:-}" != --worker ]; then
  exec python3 "$SCRIPT_DIR/lib/eu-state-run.py" "$@"
fi
shift
mode="$1"; CASE_DIR="$2"
API="${RANGERDANGER_API:-http://127.0.0.1:8088}"
FW=rangerdanger-firewall
PACKAGE="$ROOT/lab-definitions/packages/eu-iec104-substation"
TMP_DIR="$CASE_DIR"
failures=0; passes=0
pass() { printf 'PASS %s\n' "$1"; passes=$((passes + 1)); }
fail() { printf 'FAIL %s\n' "$1" >&2; failures=$((failures + 1)); }
# shellcheck source=lib/docker-bounded.sh
. "$SCRIPT_DIR/lib/docker-bounded.sh"
# shellcheck source=lib/eu-range.sh
. "$SCRIPT_DIR/lib/eu-range.sh"
# shellcheck source=lib/eu-evidence.sh
. "$SCRIPT_DIR/lib/eu-evidence.sh"
# shellcheck source=lib/eu-gates-process.sh
. "$SCRIPT_DIR/lib/eu-gates-process.sh"

finish() {
  local result=$?
  trap - EXIT
  set +e
  if eu_get_field; then
    printf '%s\n' "$API_BODY" > "$CASE_DIR/latest-field.json" || result=1
  else result=1; fi
  if eu_get_operator; then
    printf '%s\n' "$API_BODY" > "$CASE_DIR/latest-operator.json" || result=1
  else result=1; fi
  exit "$result"
}
trap finish EXIT

checkpoint() {
  local label="$1" row="$2"; shift 2
  local out="$CASE_DIR/$label" since
  mkdir -p "$out"
  eu_get_field
  printf '%s\n' "$API_BODY" > "$out/before-field.json"
  since=$(eu_utc) || return
  printf '%s\n' "$since" > "$out/since.txt"
  # A strictly advancing solve is required, even if no device revision changed.
  sleep 1.1
  eu_settle_for_gi 10 "$label"
  eu_interrogate_since "$since" 15 "$label"
  eu_get_field
  printf '%s\n' "$API_BODY" > "$out/field.json"
  eu_get_operator
  printf '%s\n' "$API_BODY" > "$out/operator.json"
  node "$ROOT/frontend/e2e/eu-state-dom.mjs" "$API" "$out" "$row"
  python3 "$SCRIPT_DIR/lib/eu-state-check.py" "$row" "$out" "$@" > "$out/result.json" || {
    cat "$out/result.json"; return 1;
  }
  cat "$out/result.json"
}

switch_command() {
  eu_command "$1-command" "$2" completed
  eu_settle_for_gi 10 "post $1 $2"
}

reference() {
  eu_set_profile false
  eu_reset_all
  eu_reference_tap 0
  eu_settle_for_gi 10 reference
}

switching() {
  local row="$1" sw="${1%%-*}" f2="${1#*-}" label="${2:-final}"
  reference
  if [ "$f2" = tripped ]; then
    eu_action process fault_s2
    eu_wait_for_field '.devices["breaker-1"].lockout and .devices["breaker-1"].protection_tripped and .process.buses.s2.live == false' 10 'S2 trip'
    switch_command rmu-1 open
    switch_command rmu-2 close
    eu_wait_for_field '.devices["f2-breaker"].protection_tripped and .devices["f2-breaker"].lockout
      and .devices["f2-breaker"].position == "open"
      and (.devices["f2-breaker"].protection | .instantaneous_a == 2000 and .pickup_a == 400
        and .delay_ms == 1000 and .last_trip_current_a >= .instantaneous_a)
      and .process.buses.s2.live == false and .process.buses.s3.live == false' 10 'F2 trip amps and settled dead buses'
    eu_action process clear_fault
    eu_action breaker-1 reset_lockout
    eu_action rmu-1 reset_fault_passage
    eu_action rmu-2 reset_fault_passage
    [ "${sw:2:1}" = C ] || switch_command rmu-2 open
  else
    switch_command feeder-breaker open
    switch_command rmu-1 open
    [ "${sw:2:1}" = O ] || switch_command rmu-2 close
  fi
  [ "${sw:1:1}" = O ] || switch_command rmu-1 close
  [ "${sw:0:1}" = O ] || switch_command feeder-breaker close
  checkpoint "$label" "$row"
}

case "$mode" in
  cleanup)
    # Reset is intentionally the existing workshop barrier, not individual best-effort writes.
    eu_set_profile true
    eu_reset_all
    eu_get_field
    printf '%s\n' "$API_BODY" > "$CASE_DIR/reset-field.json"
    printf '%s' "$API_BODY" | jq -e '.devices["breaker-1"].position == "closed" and (.devices["breaker-1"].lockout | not)
      and (.devices["breaker-1"].protection_tripped | not) and .devices["rmu-1"].position == "closed"
      and .devices["rmu-2"].position == "open" and .devices["oltc-1"].auto
      and (.devices["oltc-1"].blocked | not) and all(.devices[]; .remote_enabled == true)
      and .devices["f2-breaker"].position == "closed"
      and (.devices["f2-breaker"] | (.lockout | not) and (.protection_tripped | not))
      and all(.devices["rmu-1"], .devices["rmu-2"]; (.earth_closed | not) and (.fault_passage | not))
      and .process.fault_section == "none" and (.process.f2_tripped | not) and .process.profile.enabled' >/dev/null
    ;;
  baseline) reference; checkpoint baseline CCO-normal ;;
  CCC-normal-interlock)
    switching OCC-normal predecessor
    eu_command feeder-breaker-command close rejected live_live
    printf '%s\n' "$COMMAND_BODY" > "$CASE_DIR/interlock-command.json"
    printf '%s\n' "$API_BODY" > "$CASE_DIR/interlock-audit.json"
    checkpoint rejected OCC-normal --interlock CCC-normal-interlock
    ;;
  ???-normal|???-tripped) switching "$mode" ;;
  f2-cycle)
    switching OOO-tripped tripped
    eu_action f2-breaker reset
    checkpoint reset OOO-normal
    ;;
  s1-cycle)
    reference
    eu_action process fault_s1
    eu_wait_for_field '.devices["breaker-1"].lockout and .devices["breaker-1"].protection_tripped and .process.buses.s1.live == false and .process.buses.s2.live == false' 10 'S1 trip/lockout'
    checkpoint lockout S1-lockout
    switch_command rmu-1 open
    switch_command rmu-2 close
    checkpoint isolated S1-isolated
    eu_action process clear_fault
    checkpoint cleared S1-isolated --cleared
    eu_action breaker-1 reset_lockout
    checkpoint reset-lockout OOC-normal
    switch_command feeder-breaker close
    checkpoint cc-close COC-normal
    switch_command rmu-2 open
    switch_command rmu-1 close
    checkpoint restored CCO-normal
    ;;
  rmu-local)
    reference
    checkpoint before CCO-normal
    eu_action rmu-1 set_local
    eu_get_field
    revision=$(printf '%s' "$API_BODY" | jq -er '.devices["rmu-1"].revision')
    eu_command rmu-1-command open rejected not_remote
    eu_get_field
    printf '%s' "$API_BODY" | jq -e --argjson revision "$revision" '.devices["rmu-1"].revision == $revision and .devices["rmu-1"].position == "closed"' >/dev/null
    checkpoint rejected CCO-normal --local
    eu_action rmu-1 set_remote
    checkpoint remote CCO-normal
    ;;
  oltc)
    reference
    checkpoint manual CCO-normal
    eu_command oltc-1-step raise completed
    checkpoint raised CCO-normal --tap 1
    eu_command oltc-1-step lower completed
    checkpoint lowered CCO-normal
    eu_command oltc-1-mode auto completed
    eu_wait_for_field '.devices["oltc-1"].tap == 2 and .devices["oltc-1"].moving == false and .devices["oltc-1"].measurements.voltage_kv_ll >= 20.196 and .devices["oltc-1"].measurements.voltage_kv_ll <= 20.604' 100 'auto two delayed steps into PLC band'
    checkpoint auto CCO-normal --tap 2 --auto
    ;;
  *) echo "Unknown internal case $mode" >&2; exit 2 ;;
esac
[ "$failures" -eq 0 ]
