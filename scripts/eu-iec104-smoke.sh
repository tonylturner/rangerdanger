#!/usr/bin/env bash
# Headless increment-4 exit gate for the live EU IEC104 package.
#
# Usage: scripts/eu-iec104-smoke.sh [--dry-run|--list]
# This script is intentionally destructive while running: it changes the
# active policy, breaker truth, and pauses range services. It always restores
# the weak policy + device reset and unpauses services it paused. A failed
# run first keeps its evidence in EU_GATE_EVIDENCE_DIR (default
# build/deploy-test/eu-evidence/<UTC timestamp>/). Sourcing this file only
# defines the gates; scripts/tests/eu-gate/ runs them against stubs.

set -uo pipefail

API="${RANGERDANGER_API:-http://localhost:8088}"
# shellcheck disable=SC2034
JWT_SECRET="${CONTAIND_JWT_SECRET:-rangerdanger-dev}"
PACKAGE_ID="eu-iec104-substation"
RTU_IP="10.30.30.20"
CC_IP="10.60.60.20"
BREAKER_IP="10.40.40.20"
FW="rangerdanger-firewall"
KALI="rangerdanger-kali"
RTU="rangerdanger-iec104-rtu"
CC="rangerdanger-control-centre"
BREAKER="rangerdanger-feeder-breaker"
GPS="rangerdanger-gps-sim"
OPENDSS="rangerdanger-opendss-sim"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PACKAGE="$ROOT/lab-definitions/packages/$PACKAGE_ID"
# shellcheck source-path=SCRIPTDIR source=lib/range.sh
. "$SCRIPT_DIR/lib/range.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-range.sh
. "$SCRIPT_DIR/lib/eu-range.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-monitor.sh
. "$SCRIPT_DIR/lib/eu-monitor.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-policy.sh
. "$SCRIPT_DIR/lib/eu-policy.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-cleanup.sh
. "$SCRIPT_DIR/lib/eu-cleanup.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-sessions.sh
. "$SCRIPT_DIR/lib/eu-sessions.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-capture.sh
. "$SCRIPT_DIR/lib/eu-capture.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-evidence.sh
. "$SCRIPT_DIR/lib/eu-evidence.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-readiness.sh
. "$SCRIPT_DIR/lib/eu-gates-readiness.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-protocol.sh
. "$SCRIPT_DIR/lib/eu-gates-protocol.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-segmentation.sh
. "$SCRIPT_DIR/lib/eu-gates-segmentation.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-resilience.sh
. "$SCRIPT_DIR/lib/eu-gates-resilience.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-process.sh
. "$SCRIPT_DIR/lib/eu-gates-process.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-history.sh
. "$SCRIPT_DIR/lib/eu-gates-history.sh"
# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-labs.sh
. "$SCRIPT_DIR/lib/eu-gates-labs.sh"

# shellcheck source-path=SCRIPTDIR source=lib/eu-gates-management.sh
. "$SCRIPT_DIR/lib/eu-gates-management.sh"

failures=0
passes=0
gate_failures=0
# shellcheck disable=SC2034
state_touched=0
# shellcheck disable=SC2034 # consumed by eu-cleanup.sh
profile_disabled=0
# shellcheck disable=SC2034
cleanup_running=0
# shellcheck disable=SC2034
capture_running=0
# shellcheck disable=SC2034
monitor_running=0
# shellcheck disable=SC2034
MONITOR_STOP_FAILED=0
gps_paused=0
opendss_paused=0
rtu_paused=0
# shellcheck disable=SC2034 # shared with cleanup
cc_paused=0
g7_loss_verified=0
g11_loss_verified=0
# shellcheck disable=SC2034
BASELINE_OPERATOR=""
BASELINE_TRUTH=""
TMP_DIR=""
CAPTURE_PID=""
# shellcheck disable=SC2034 # capture host defaults to firewall outside window helpers
CAPTURE_HOST=""
# shellcheck disable=SC2034 # shared with tracer cleanup
PLC_SCAN_HELPER=""
# shellcheck disable=SC2034
MONITOR_PID=""
# shellcheck disable=SC2034
MONITOR_LOG=""
# shellcheck disable=SC2034
MONITOR_ERR=""
# shellcheck disable=SC2034
MONITOR_RC=""
# shellcheck disable=SC2034
CAPTURE_PATH=""
# shellcheck disable=SC2034
CAPTURE_PID_FILE=""
# shellcheck disable=SC2034
CAPTURE_LOG=""
# shellcheck disable=SC2034
FW_TOKEN=""
EPHEMERAL_LOW=""
EPHEMERAL_HIGH=""

pass() {
  printf 'PASS %s\n' "$1"
  passes=$((passes + 1))
}

fail() {
  printf 'FAIL %s\n' "$1" >&2
  failures=$((failures + 1))
}

gate_start() {
  gate_failures=$failures
  printf '\n=== %s ===\n' "$1"
}

gate_end() {
  if [ "$failures" -eq "$gate_failures" ]; then
    printf 'GATE %s PASS\n' "$1"
  else
    printf 'GATE %s FAIL\n' "$1"
  fi
}

eu_gate_plan() {
  cat <<'PLAN'
EU IEC104 gate plan (no Docker or backend access):
  G0  Ready/package, policy warnings, nft rules, image/kernel/tcp_loose, sockets/routes/sysctls/Compose
  G1  Reset, then poll to a GOOD CC baseline with operator = truth = solver; advancing solve
  G2  GPS pause/unpause and timed source_time_invalid transitions
  G3  Open/close/no-op/lockout, both-hop capture, TypeID 46 select/execute and Modbus FC6 decode
  G4  Kali GI succeeds under weak
  G5  Improved Kali connect fails; fresh DENY; CC stays started (physical denial only)
  G6  Earlier Kali conntrack rows drain; established Kali monitor survives improved, exact tuple kill, exits 3, no later ASDUs, reconnect fails
  G7  Remove CC allow; read back; kill exact tuple; CC points link_lost
  G8  Fault/trip changes truth and solver while CC cache stays unchanged (needs G7)
  G9  Restore improved; CC reconnect/GI and truth converge (needs G7)
  G10 Reset to closed-breaker + supply state, then eu-validation-evidence validator PASS
  G11 Remove RTU-to-breaker allow; exact Modbus kill; stale/reset/recover
  G12 OpenDSS pause yields measurement_invalid only; unpause recovers
  G13 RTU pause, process fault/breaker reset each awaiting its first_solve, CC link loss, new connection + GI, audit
  G15 RMU switching, local/earth/live-live interlocks, S1 restore and S2 FPI
  G16 OLTC manual/auto, load raise, source limit and negative settle
  G17 Real PLC scan-thread freeze; responsive Modbus, heartbeat stale and recovery
  G18 Historian CC outage gap, schema-3 snapshots and ingest isolation
  G19 Improved denies Kali solver and historian ingest
  G20 eu-baseline-assessment validator: weak baseline, traffic capture and student walk
  G21 eu-firewall-implementation validator: weak-to-improved conduit and policy walk
  G22 eu-hardening-configurations validator: IEC104 wire attack, restore, harden and retest
  G23 Embedded containd bundle/session, weak/improved management INPUT isolation, loopback-only engine, backend apply/reset/events/flows
  G24 eu-vendor-rdp-compromise validator: enterprise-to-vendor and vendor-to-field walk
  G25 eu-validation-evidence validator: hardened report and continuity walk
  G26 eu-segmentation-requirements served decisions and package-bound catalog
  G27 eu-remediation-planning served decision step and package-bound catalog
  G28 Full and partial compiled plans: faithful proof, hardened acceptance and golden prevention gaps; restore weak without provenance
  G14 Device reset recovers the chain; apply weak
  EXIT Keep evidence on failure; restore weak + device reset; unpause services paused by this run
PLAN
}

eu_run_gate() {
  local gate="$1" before="$failures"
  gate_start "$gate"
  if ! "eu_gate_$gate" && [ "$failures" -eq "$before" ]; then
    fail "$gate aborted without complete attempt evidence"
  fi
  eu_gate_resources_finish "$gate"
  gate_end "$gate"
}

eu_gate_main() {
  local gate
  case "${1:-}" in
    --dry-run|--list)
      if [ "$#" -ne 1 ]; then
        echo "usage: $0 [--dry-run|--list]" >&2
        exit 2
      fi
      eu_gate_plan
      exit 0
      ;;
    "")
      ;;
    *)
      echo "usage: $0 [--dry-run|--list]" >&2
      exit 2
      ;;
  esac
  eu_gate_preflight
  for gate in G0 G1 G2 G3 G4 G5 G6 G7 G8 G9 G10 G11 G12 G13 G15 G16 G17 G18 G19 G20 G21 G22 G23 G24 G25 G26 G27 G28 G14; do
    eu_run_gate "$gate"
  done
  exit 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  eu_gate_main "$@"
fi
