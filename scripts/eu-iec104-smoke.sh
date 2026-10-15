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
# shellcheck disable=SC2034
SCENARIO_ID="iec104-telecontrol-boundary"
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
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/range.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/eu-range.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/eu-monitor.sh"

failures=0
passes=0
gate_failures=0
# shellcheck disable=SC2034
state_touched=0
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
  G5  Improved Kali connect fails; fresh DENY; CC stays started; validator FAIL
  G6  Earlier Kali conntrack rows drain; established Kali monitor survives improved, exact tuple kill, exits 3, no later ASDUs, reconnect fails
  G7  Remove CC allow; read back; kill exact tuple; CC points link_lost
  G8  Fault/trip changes truth and solver while CC cache stays unchanged (needs G7)
  G9  Restore improved; CC reconnect/GI and truth converge (needs G7)
  G10 EU validator PASS
  G11 Remove RTU-to-breaker allow; exact Modbus kill; stale/reset/recover
  G12 OpenDSS pause yields measurement_invalid only; unpause recovers
  G13 RTU pause, process fault/breaker reset each awaiting its first_solve, CC link loss, new connection + GI, audit
  G15 RMU switching, local/earth/live-live interlocks, S1 restore and S2 FPI
  G16 OLTC manual/auto, load raise, source limit and negative settle
  G17 Real PLC scan-thread freeze; responsive Modbus, heartbeat stale and recovery
  G18 Historian CC outage gap, schema-2 snapshots and ingest isolation
  G19 Improved denies Kali solver and historian ingest
  G14 Device reset recovers the chain; apply weak
  EXIT Keep evidence on failure; restore weak + device reset; unpause services paused by this run
PLAN
}

# Validate the backend target before touching Docker, then arm restoration before
# the first policy mutation. A wrong or unavailable package is a hard refusal.
eu_gate_preflight() {
  local health_status range_http_status preflight_ready=1 required_container
  if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
    echo "FAIL required host tools curl and jq are needed to verify the EU range" >&2
    exit 1
  fi
  health_status=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' \
    "$API/api/health" 2>/dev/null) || health_status=""
  if [ "$health_status" != "200" ]; then
    echo "FAIL backend is not healthy at $API" >&2
    exit 1
  fi
  range_http_status=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' \
    "$API/api/range" 2>/dev/null) || range_http_status=""
  if [ "$range_http_status" != "200" ]; then
    echo "FAIL GET $API/api/range returned HTTP ${range_http_status:-missing}, expected 200" >&2
    exit 1
  fi
  if ! range_is_ready "$PACKAGE_ID"; then
    echo "FAIL refusing non-ready or wrong package; GET $API/api/range: $(range_status)" >&2
    exit 1
  fi

  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/eu-iec104-smoke.XXXXXX") || {
    echo "FAIL cannot create a temporary directory" >&2
    exit 1
  }
  trap eu_on_exit EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  eu_check_tools || preflight_ready=0
  if [ ! -d "$PACKAGE" ] || [ ! -f "$PACKAGE/package.yml" ] \
    || [ ! -f "$PACKAGE/firewall/weak.json" ] || [ ! -f "$PACKAGE/firewall/improved.json" ] \
    || [ ! -f "$PACKAGE/protocols/points.json" ]; then
    fail "EU package, weak/improved policies, or points contract files are missing"
    preflight_ready=0
  elif ! grep -Eq '^id:[[:space:]]*eu-iec104-substation[[:space:]]*$' "$PACKAGE/package.yml"; then
    fail "$PACKAGE/package.yml does not declare id: $PACKAGE_ID"
    preflight_ready=0
  fi
  if [ "$preflight_ready" -eq 0 ]; then
    exit 1
  fi

  for required_container in "$FW" "$KALI" rangerdanger-vendor-jump rangerdanger-eng-ws \
    "$RTU" "$CC" "$BREAKER" "$GPS" "$OPENDSS" rangerdanger-rmu-1 rangerdanger-rmu-2 \
    rangerdanger-oltc-1 rangerdanger-openplc rangerdanger-historian; do
    if eu_container_running "$required_container"; then
      pass "$required_container running"
    else
      fail "$required_container is not running"
      preflight_ready=0
    fi
  done
  if [ "$preflight_ready" -eq 0 ]; then
    exit 1
  fi
}

eu_gate_G0() {
  local image_id kernel tcp_loose ephemeral_range policy_name policy_file compose_json candidate \
    network_count forwarding
  image_id=$(docker inspect -f '{{.Image}}' "$FW" 2>/dev/null) || image_id=""
  kernel=$(docker exec "$FW" uname -r 2>/dev/null) || kernel=""
  tcp_loose=$(docker exec "$FW" cat /proc/sys/net/netfilter/nf_conntrack_tcp_loose 2>/dev/null) || tcp_loose=""
  ephemeral_range=$(docker exec "$FW" cat /proc/sys/net/ipv4/ip_local_port_range 2>/dev/null) || ephemeral_range=""
  if [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]] && [ -n "$kernel" ] \
    && [[ "$tcp_loose" =~ ^[01]$ ]] && [ "$tcp_loose" = "1" ] \
    && [[ "$ephemeral_range" =~ ^[0-9]+[[:space:]]+[0-9]+$ ]]; then
    EPHEMERAL_LOW=$(printf '%s' "$ephemeral_range" | awk '{print $1}')
    EPHEMERAL_HIGH=$(printf '%s' "$ephemeral_range" | awk '{print $2}')
    if [ "$EPHEMERAL_LOW" -lt "$EPHEMERAL_HIGH" ]; then
      printf 'G0 evidence: containd image digest=%s kernel=%s nf_conntrack_tcp_loose=%s ephemeral_ports=%s-%s\n' \
        "$image_id" "$kernel" "$tcp_loose" "$EPHEMERAL_LOW" "$EPHEMERAL_HIGH"
      pass "G0 kernel, containd image digest, tcp_loose=1 and ephemeral-port range recorded"
    else
      fail "G0 ephemeral-port range is invalid: $ephemeral_range"
    fi
  else
    fail "G0 missing or invalid digest/kernel/tcp_loose/ephemeral-range evidence: digest=$image_id kernel=$kernel tcp_loose=$tcp_loose ephemeral=$ephemeral_range"
  fi
  if ! eu_make_firewall_token; then
    fail "G0 could not create containd admin token"
  fi

  for policy_name in weak improved; do
    if [ "$policy_name" = "weak" ]; then
      policy_file="$PACKAGE/firewall/weak.json"
    else
      policy_file="$PACKAGE/firewall/improved.json"
    fi
    if ! jq -e 'type == "object" and (.firewall.rules | type == "array")' "$policy_file" >/dev/null 2>&1; then
      fail "G0 $policy_name policy is empty or invalid JSON"
      continue
    fi
    if eu_apply_named "$policy_name"; then
      eu_check_policy_and_nft "$policy_name" "$policy_file" || true
    fi
  done

  # The firewall build and package must describe the same internal-only solver net.
  compose_json="$TMP_DIR/eu-compose.json"
  if RANGERDANGER_ROOT="$ROOT" docker compose -p rangerdanger \
    -f "$PACKAGE/compose.source.yml" config --format json >"$compose_json" 2>"$TMP_DIR/compose.err"; then
    if jq -e '.networks.process_net.internal == true' "$compose_json" >/dev/null 2>&1; then
      pass "G0 docker compose config keeps process_net internal"
    else
      fail "G0 normalized docker compose config does not mark process_net internal"
    fi
    if jq -e '
      [.services[] | .ports[]?] as $ports
      | ($ports | length == 3)
        and all($ports[]; .host_ip == "127.0.0.1")
        and any($ports[]; .target == 8080 and (.published | tostring) == "9080")
        and any($ports[]; .target == 8443 and (.published | tostring) == "9443")
        and any($ports[]; .target == 2222 and (.published | tostring) == "2222")
    ' "$compose_json" >/dev/null 2>&1; then
      pass "G0 Compose exposes only the three loopback firewall bindings"
    else
      fail "G0 Compose host port bindings differ from the loopback-only §1 contract"
    fi
  else
    fail "G0 docker compose config failed: $(cat "$TMP_DIR/compose.err")"
  fi

  check_listener "$RTU" "$RTU_IP:2404" "IEC104 RTU"
  check_listener "$CC" "10.99.99.21:8080" "Control-centre HTTP API"
  check_listener "$BREAKER" "$BREAKER_IP:502" "Feeder-breaker Modbus"
  check_listener "$BREAKER" "10.50.50.30:8080" "Feeder-breaker process truth API" 2
  check_listener "$BREAKER" "10.99.99.22:8080" "Feeder-breaker sim API" 2
  # The contract keeps the existing GPS image (§2): its shared HTTP server binds :8080.
  check_listener "$GPS" "*:8080" "GPS station-clock API (existing wildcard bind)"
  check_listener "$OPENDSS" "0.0.0.0:8080" "OpenDSS process/mgmt API"

  check_gateway "$KALI" 10.10.10.2
  check_gateway rangerdanger-vendor-jump 10.20.20.2
  check_gateway rangerdanger-eng-ws 10.20.20.2
  check_gateway "$RTU" 10.30.30.2
  check_gateway "$GPS" 10.30.30.2
  check_gateway "$BREAKER" 10.40.40.2
  check_gateway "$CC" 10.60.60.2

  for candidate in "$KALI" rangerdanger-vendor-jump rangerdanger-eng-ws "$RTU" "$CC" "$BREAKER" "$GPS" "$OPENDSS"; do
    network_count=$(docker inspect -f '{{len .NetworkSettings.Networks}}' "$candidate" 2>/dev/null) || network_count=""
    if ! [[ "$network_count" =~ ^[0-9]+$ ]] || [ "$network_count" -lt 1 ]; then
      fail "$candidate network membership count is missing or invalid"
      continue
    fi
    if [ "$network_count" -gt 1 ]; then
      forwarding=$(docker exec "$candidate" cat /proc/sys/net/ipv4/ip_forward 2>/dev/null) || forwarding=""
      if [ "$forwarding" = "0" ]; then
        pass "$candidate is multihomed with ip_forward=0"
      else
        fail "$candidate ip_forward is '$forwarding', expected 0"
      fi
    fi
  done
  eu_inc4_boundaries
}

eu_gate_G1() {
  local first_seq next_seq
  eu_reset_all || return
  # The reset always bumps the device revision; the solver and the RTU's next
  # Modbus poll follow asynchronously, so poll until every view agrees.
  if eu_wait_for_baseline "G1 GOOD baseline" 30; then
    pass "G1 link started, GI complete, four GOOD points; operator = truth = solver generation/revision"
  fi
  if [ -z "$BASELINE_TRUTH" ]; then
    fail "G1 physical state/process state is unavailable or unparsable"
    return
  fi
  first_seq=$(printf '%s' "$BASELINE_TRUTH" | jq -r '.process.solve_seq // empty')
  sleep 2
  if eu_get_field; then
    next_seq=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
    if [[ "$first_seq" =~ ^[0-9]+$ ]] && [[ "$next_seq" =~ ^[0-9]+$ ]] \
      && [ "$next_seq" -gt "$first_seq" ]; then
      pass "G1 OpenDSS solve_seq advances ($first_seq -> $next_seq)"
    else
      fail "G1 solve_seq did not advance ($first_seq -> $next_seq)"
    fi
  else
    fail "G1 second field state read was unavailable"
  fi
}

eu_gate_G2() {
  gps_paused=1
  if docker pause "$GPS" >/dev/null 2>"$TMP_DIR/pause-gps.err"; then
    pass "G2 GPS container paused"
  else
    fail "G2 could not pause GPS: $(cat "$TMP_DIR/pause-gps.err")"
    return 1
  fi
  sleep 6
  # The one-second GI can replace a timed event before the API is read. Prove
  # the actual event on the wire, not whichever cache update won that race.
  eu_window_start G2-invalid || return
  eu_wait_command open completed || return
  eu_window_finish G2-invalid || return
  eu_check_timed_status "$TMP_DIR/G2-invalid.pcap" 1 1 || return
  eu_unpause_owned "$GPS" gps "$gps_paused" || return
  eu_wait_for_field '.process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G2 solver after GPS resumes" || return
  sleep 2
  eu_window_start G2-synced || return
  eu_wait_command close completed || return
  eu_window_finish G2-synced || return
  eu_check_timed_status "$TMP_DIR/G2-synced.pcap" 2 0
}

eu_gate_G3() {
  local before_noop_revision after_noop_revision
  CAPTURE_PATH="/tmp/eu-iec104-smoke-$$.pcap"
  CAPTURE_PID_FILE="/tmp/eu-iec104-smoke-$$.tcpdump.pid"
  CAPTURE_LOG="/tmp/eu-iec104-smoke-$$.tcpdump.log"
  # shellcheck disable=SC2034
  capture_running=1
  if ! docker exec "$FW" sh -c "rm -f '$CAPTURE_PATH' '$CAPTURE_PID_FILE' '$CAPTURE_LOG'; \
    tcpdump --immediate-mode -i any -s 0 -U -w '$CAPTURE_PATH' 'tcp port 2404 or tcp port 502' \
      >'$CAPTURE_LOG' 2>&1 & echo \$! > '$CAPTURE_PID_FILE'"; then
    fail "G3 could not start tcpdump for both protocol hops"
  else
    CAPTURE_PID=$(docker exec "$FW" cat "$CAPTURE_PID_FILE" 2>/dev/null) || CAPTURE_PID=""
    if ! [[ "$CAPTURE_PID" =~ ^[0-9]+$ ]]; then
      fail "G3 tcpdump PID is missing or unparsable"
    else
      pass "G3 capture started for IEC104 and Modbus TCP hops"
    fi
  fi

  if eu_wait_command open completed; then
    eu_wait_for_field '.devices["breaker-1"].position == "open" and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G3 open truth/solver" || true
    eu_wait_for_operator 'point("feeder-breaker-position").value == "open"' 10 "G3 open operator feedback" || true
  fi
  if eu_wait_command close completed; then
    eu_wait_for_field '.devices["breaker-1"].position == "closed" and .process.buses.s1.live == true and .devices["breaker-1"].measurements.current_a > 0 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G3 close truth/solver" || true
    eu_wait_for_operator 'point("feeder-breaker-position").value == "closed"' 10 "G3 close operator feedback" || true
  fi
  if eu_wait_command open completed; then
    eu_wait_for_operator 'point("feeder-breaker-position").value == "open"' 10 "G3 second open feedback" || true
  fi
  if eu_get_field; then
    before_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.devices["breaker-1"].revision // empty')
  else
    before_noop_revision=""
    fail "G3 pre-no-op truth read unavailable"
  fi
  if eu_wait_command open completed; then
    eu_assert_jq "$COMMAND_BODY" '.result == "completed" and .feedback.ioa == 1001 and .feedback.value == "open" and .feedback.at != null' "G3 no-op open completed with matched COT 11 feedback"
  fi
  if eu_get_field; then
    after_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.devices["breaker-1"].revision // empty')
    if [ -n "$before_noop_revision" ] && [ "$after_noop_revision" = "$before_noop_revision" ]; then
      pass "G3 no-op command did not change device truth revision"
    else
      fail "G3 no-op changed or could not verify device revision ($before_noop_revision -> $after_noop_revision)"
    fi
  else
    fail "G3 post-no-op truth read unavailable"
  fi
  eu_wait_command close completed || true
  eu_device_step local-trip "G3 sim local fault" || true
  eu_wait_for_field '.devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true' 10 "G3 local lockout" || true
  eu_wait_command close rejected || true
  eu_device_step clear-fault "G3 device reset clear process fault" || true
  eu_device_step restore-device "G3 device reset" || true
  eu_stop_capture || true
  if [ -n "$CAPTURE_PID" ]; then
    if docker cp "$FW:$CAPTURE_PATH" "$TMP_DIR/eu-iec104-gate.pcap" >/dev/null 2>"$TMP_DIR/docker-cp.err"; then
      eu_decode_capture
    else
      fail "G3 could not retrieve firewall capture: $(cat "$TMP_DIR/docker-cp.err")"
    fi
  else
    fail "G3 capture PID is empty; cannot retrieve protocol evidence"
  fi

  # The decoded copy stays in TMP_DIR (evidence on failure).
  if docker exec "$FW" rm -f "$CAPTURE_PATH" "$CAPTURE_PID_FILE" "$CAPTURE_LOG" >/dev/null 2>&1; then
    CAPTURE_PATH=""
    CAPTURE_PID_FILE=""
    CAPTURE_LOG=""
  else
    fail "G3 temporary firewall capture files could not be removed"
  fi
}

eu_gate_G4() {
  if eu_apply_named weak; then
    eu_run_kali_gi G4 0 || true
  else
    fail "G4 cannot perform weak-policy Kali GI"
  fi
}

eu_gate_G5() {
  local event_started
  if ! eu_apply_named improved; then
    fail "G5 cannot test improved-policy denial"
    return
  fi
  event_started=$(python3 -c 'import time; print(time.time_ns())') || event_started=""
  if [ -z "$event_started" ]; then
    fail "G5 cannot establish a precise event timestamp cursor"
  else
    eu_run_kali_gi G5 2 || true
    eu_fresh_deny_event 10.10.10.50 "$event_started" 30 || true
  fi
  if eu_get_operator; then
    eu_assert_jq "$API_BODY" '.link.state == "started"' "G5 CC IEC104 link remains started"
  else
    fail "G5 CC state unavailable"
  fi
  eu_validate_scenario FAIL || true
}

eu_gate_G6() {
  local g6_kill_verified=0
  # §13 requires exactly one Kali tuple once the monitor runs, so it starts only
  # after earlier Kali sessions (the G4 one-shot's TIME_WAIT) have expired.
  if ! eu_wait_session_history_drained 10.10.10.50 "$RTU_IP" 2404 "G6 Kali session baseline"; then
    fail "G6 monitor not started without a clean Kali-to-RTU session baseline"
    return
  fi
  if ! eu_apply_named weak || ! eu_monitor_start; then
    fail "G6 could not start a weak-policy persistent monitor"
    return
  fi
  if eu_g6_cut_monitor_session; then
    g6_kill_verified=1
  fi
  if [ "$g6_kill_verified" -eq 1 ]; then
    eu_run_kali_gi G6-reconnect 2 || true
  else
    fail "G6 reconnect denial was not tested because the established-session kill was unverified"
  fi
}

# G6 body: the established monitor survives improved, then its exact tuple is
# killed and it must exit 3 with no later ASDU.
eu_g6_cut_monitor_session() {
  local asdus_before kali_tuple asdu_observation_valid=1 kill_started_ms kill_epoch
  if eu_monitor_wait_event connected 15 && eu_monitor_wait_event gi_complete 15; then
    pass "G6 monitor connected and completed GI under weak"
  else
    fail "G6 monitor failed to establish its IEC104 session"
  fi
  if ! eu_monitor_asdu_count; then
    fail "G6 monitor ASDU JSON could not be parsed"
    return 1
  fi
  asdus_before=$MONITOR_ASDUS
  eu_apply_named improved || return 1
  if ! eu_monitor_read || [ "$MONITOR_LINES" -eq 0 ] \
    || ! printf '%s\n' "$MONITOR_BODY" | jq -s -e --arg peer "$RTU_IP:2404" '
      [.[] | select(.event == "connected")] as $connected
      | ($connected | length) == 1
        and $connected[0].local == null and $connected[0].peer == $peer
    ' >/dev/null 2>&1; then
    fail "G6 monitor must emit exactly one connected event with local=null and peer=$RTU_IP:2404"
    return 1
  fi
  if ! eu_read_sessions 2404 10.10.10.50 \
    || ! eu_session_tuple "$SESSION_BODY" 10.10.10.50 "$RTU_IP" 2404; then
    fail "G6 session lookup did not yield exactly one Kali-to-RTU tuple"
    return 1
  fi
  kali_tuple="$SESSION_TUPLE"
  pass "G6 non-empty dport 2404 session list contains the monitor's exact Kali tuple"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    sleep 1
    if ! eu_monitor_asdu_count; then
      asdu_observation_valid=0
      break
    fi
  done
  if [ "$asdu_observation_valid" -eq 1 ] && [ "${MONITOR_ASDUS:-0}" -gt "$asdus_before" ]; then
    pass "G6 established IEC104 session continued receiving ASDUs for 10s after improved apply"
  else
    fail "G6 established monitor received no new ASDUs during the 10s observation"
  fi
  kill_started_ms=$(eu_now_ms) || kill_started_ms=""
  kill_epoch=$(python3 -c 'import time; print(time.time_ns())') || kill_epoch=""
  if ! [[ "$kill_started_ms" =~ ^[0-9]+$ ]] || [ -z "$kill_epoch" ]; then
    fail "G6 could not timestamp conntrack kill"
    return 1
  fi
  eu_kill_tuple "$kali_tuple" && eu_monitor_finish 3 30 "$kill_started_ms" \
    && eu_monitor_no_asdu_after "$kill_epoch"
}

eu_gate_G7() {
  local before_g7 g7_policy g7_kill_started_ms
  g7_loss_verified=0
  if ! eu_get_operator; then
    fail "G7 operator state unavailable before restrictive policy"
    return
  fi
  before_g7="$API_BODY"
  if ! printf '%s' "$before_g7" | jq -e '.link.state == "started"' >/dev/null 2>&1; then
    fail "G7 operator link state was not started before applying the CC restriction: $before_g7"
    return
  fi
  g7_policy="$TMP_DIR/improved-without-cc.json"
  eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$g7_policy" || return
  if eu_policy_has_no_ephemeral_allow "$g7_policy"; then
    pass "G7 custom policy has no ICS keys or ephemeral destination-port allow"
  else
    fail "G7 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
  fi
  eu_apply_custom "$g7_policy" "G7 improved minus CC allow" || return
  eu_check_nft_chain G7-custom || true
  if ! eu_read_sessions 2404 "$CC_IP" \
    || ! eu_session_tuple "$SESSION_BODY" "$CC_IP" "$RTU_IP" 2404; then
    fail "G7 session lookup did not yield exactly one CC-to-RTU tuple"
    return
  fi
  pass "G7 exact established CC tuple exists before kill"
  g7_kill_started_ms=$(eu_now_ms) || g7_kill_started_ms=""
  if ! [[ "$g7_kill_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G7 cannot establish monotonic timeout before conntrack kill"
  elif eu_execute_step cut-session "G7 CC session cut" && eu_wait_for_operator \
    '.link.state != "started" and all(.points[]; .quality.iv and .quality.nt and .stale_reason == "link_lost")' \
    30 "G7 CC link loss with four link_lost points" "$g7_kill_started_ms"; then
    g7_loss_verified=1
    pass "G7 CC link is not started and all points are IV+NT link_lost"
  fi
}

eu_gate_G8() {
  local fault_step g8_before g8_epoch g8_points active g8_truth g8_operator current_points current_epoch g8_seq1 g8_seq2
  if [ "$g7_loss_verified" -ne 1 ]; then
    fail "G8 cannot test cache isolation without the verified G7 CC disconnect"
    return
  fi
  if ! eu_get_operator; then
    fail "G8 operator state unavailable before the fault sequence"
    return
  fi
  g8_before="$API_BODY"
  g8_epoch=$(printf '%s' "$g8_before" | jq -r '.cache_epoch // empty')
  g8_points=$(eu_get_point_snapshot "$g8_before") || g8_points=""
  if [ -z "$g8_epoch" ] || [ -z "$g8_points" ] \
    || ! printf '%s' "$g8_before" | jq -e "$EU_JQ_DEFS four_points" >/dev/null 2>&1; then
    fail "G8 cache epoch, four-point shape, or point snapshot is empty/invalid"
  fi
  for active in true false true; do
    if [ "$active" = true ]; then fault_step=local-trip; else fault_step=clear-fault; fi
    if [ "$active" = true ]; then
      eu_device_step clear-fault "G8 clear before trip" || return
      eu_device_step restore-device "G8 reclose before trip" || return
      eu_wait_for_field '.devices["breaker-1"].measurements.valid == true' 10 "G8 reclose solved" || return
    fi
    eu_device_step "$fault_step" "G8 sim fault active=$active" || true
    if [ "$active" = "true" ]; then
      eu_wait_for_field '.devices["breaker-1"].position == "open" and .devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1 and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G8 local protection and open solver" || true
    else
      eu_wait_for_field '.process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 10 "G8 solver after fault cleared" || true
    fi
    if ! eu_get_field; then
      fail "G8 physical state/solver response unavailable during fault active=$active"
      continue
    fi
    g8_truth="$API_BODY"
    if ! eu_get_operator; then
      fail "G8 operator-state route was not HTTP 200 JSON during fault active=$active"
      continue
    fi
    g8_operator="$API_BODY"
    current_points=$(eu_get_point_snapshot "$g8_operator") || current_points=""
    current_epoch=$(printf '%s' "$g8_operator" | jq -r '.cache_epoch // empty')
    if [ -n "$g8_points" ] && [ -n "$current_points" ] \
      && [ "$current_points" = "$g8_points" ] && [ "$current_epoch" = "$g8_epoch" ]; then
      pass "G8 operator values/received_at/source_time and cache_epoch unchanged with fault active=$active"
    else
      fail "G8 operator cache changed while CC path denied with fault active=$active"
    fi
    if printf '%s' "$g8_operator" | jq -e '.link.state != "started"' >/dev/null 2>&1; then
      pass "G8 operator-state route remains HTTP 200 while CC link is down"
    else
      fail "G8 operator route did not report the expected disconnected CC while fault active=$active"
    fi
    if [ "$active" = "true" ]; then
      eu_assert_jq "$g8_truth" '.devices["breaker-1"].position == "open" and .devices["breaker-1"].lockout == true and .devices["breaker-1"].protection_tripped == true and .process.buses.s1.live == false and .devices["breaker-1"].measurements.current_a < 0.1' "G8 physical state and independent solver show open/tripped"
    fi
  done
  if ! eu_get_field; then
    fail "G8 solver state response failed"
    return
  fi
  g8_seq1=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
  sleep 2
  if ! eu_get_field; then
    fail "G8 second solver state response failed"
    return
  fi
  g8_seq2=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
  if [[ "$g8_seq1" =~ ^[0-9]+$ ]] && [[ "$g8_seq2" =~ ^[0-9]+$ ]] \
    && [ "$g8_seq2" -gt "$g8_seq1" ]; then
    pass "G8 solver solve_seq advances independently while operator cache remains stale"
  else
    fail "G8 solver solve_seq failed to advance ($g8_seq1 -> $g8_seq2)"
  fi
}

eu_gate_G9() {
  local g9_truth
  if ! eu_apply_named improved; then
    fail "G9 cannot restore improved policy"
    return
  fi
  if [ "$g7_loss_verified" -ne 1 ]; then
    fail "G9 cannot verify recovery without the verified G7 CC disconnect"
    return
  fi
  if eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and all_good' \
    30 "G9 CC reconnect/GI and four GOOD points"; then
    pass "G9 restored IEC104 observations after GI"
  fi
  if ! eu_get_field; then
    fail "G9 physical state unavailable after reconnect"
    return
  fi
  g9_truth="$API_BODY"
  if eu_get_operator; then
    # shellcheck disable=SC2016
    eu_assert_jq_truth "$API_BODY" "$g9_truth" '
      point("feeder-breaker-position").value == $truth.devices["breaker-1"].position
      and point("feeder-protection-trip").value == $truth.devices["breaker-1"].protection_tripped
      and $truth.devices["breaker-1"].position == "open" and $truth.devices["breaker-1"].protection_tripped == true
    ' "G9 restored operator state equals open/tripped physical state"
  else
    fail "G9 operator state unavailable after reconnect"
  fi
}

eu_gate_G10() {
  eu_validate_scenario PASS || true
}

eu_gate_G11() {
  local g11_policy g11_tuple g11_loss_started_ms stale_ms g11_before_reset g11_stale g11_stale_points \
    g11_stale_epoch g11_truth g11_after_reset g11_after_reset_points reset_truth poll_since poll_until
  g11_loss_verified=0
  # Earlier gates may have left a restrictive policy or tripped device behind.
  eu_apply_named improved || return
  eu_reset_all || return
  eu_wait_for_baseline "G11 GOOD baseline before Modbus loss" 30 || return
  g11_policy="$TMP_DIR/improved-without-rtu-modbus.json"
  eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$RTU_IP" "$BREAKER_IP" 502 "$g11_policy" || return
  if eu_policy_has_no_ephemeral_allow "$g11_policy"; then
    pass "G11 custom policy has no ICS keys or ephemeral destination-port allow"
  else
    fail "G11 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
  fi
  eu_apply_custom "$g11_policy" "G11 improved minus RTU-to-breaker allow" || return
  eu_check_nft_chain G11-custom || true
  eu_get_operator || fail "G11 cannot snapshot operator points before Modbus loss"
  if ! eu_read_sessions 502 "$RTU_IP" \
    || ! eu_session_tuple "$SESSION_BODY" "$RTU_IP" "$BREAKER_IP" 502; then
    fail "G11 session lookup did not yield exactly one RTU-to-breaker tuple"
    return
  fi
  g11_tuple="$SESSION_TUPLE"
  eu_window_start G11 || return
  poll_since=$(python3 -c 'import time; print(time.time())')
  pass "G11 exact RTU-to-breaker Modbus session exists before kill"
  g11_loss_started_ms=$(eu_now_ms) || g11_loss_started_ms=""
  if ! [[ "$g11_loss_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G11 cannot establish monotonic timeout before conntrack kill"
  elif eu_kill_tuple "$g11_tuple"; then
    stale_ms=$(jq -r '.devices[] | select(.id == "feeder-breaker") | .stale_after_ms' \
      "$PACKAGE/protocols/points.json" 2>/dev/null) || stale_ms=""
    if ! [[ "$stale_ms" =~ ^[0-9]+$ ]] || [ "$stale_ms" -ne 3000 ]; then
      fail "G11 points.json stale_after_ms missing/invalid or differs from 3000: $stale_ms"
    elif eu_wait_for_operator '.link.state == "started" and all_stale("device_unreachable")' \
      5 "G11 RTU device_unreachable within stale_after_ms + 2s" "$g11_loss_started_ms"; then
      g11_loss_verified=1
      pass "G11 four points IV+NT device_unreachable with IEC104 link started"
    fi
  fi
  poll_until=$(python3 -c 'import time; print(time.time())')
  eu_window_finish G11 || return
  eu_peer_poll_gaps "$TMP_DIR/G11.pcap" "$poll_since" "$poll_until" || true
  g11_stale_points=""
  g11_stale_epoch=""
  if eu_get_operator; then
    g11_stale="$API_BODY"
    g11_stale_points=$(eu_get_point_snapshot "$g11_stale" | jq -c 'map({id,value})') || g11_stale_points=""
    g11_stale_epoch=$(printf '%s' "$g11_stale" | jq -r '.cache_epoch // empty')
    if [ -z "$g11_stale_epoch" ] || [ -z "$g11_stale_points" ] \
      || ! printf '%s' "$g11_stale" | jq -e "$EU_JQ_DEFS four_points" >/dev/null 2>&1; then
      fail "G11 stale operator cache lacks its epoch or four expected points"
    fi
  else
    fail "G11 operator state unavailable after stale transition"
  fi
  g11_before_reset=""
  if eu_get_field; then
    g11_before_reset="$API_BODY"
  else
    fail "G11 physical state unavailable before reset"
  fi
  eu_device_step clear-fault "G11 device reset changes truth clear process fault" || true
  eu_device_step restore-device "G11 device reset changes truth" || true
  if eu_get_field; then
    g11_truth="$API_BODY"
    if eu_get_operator; then
      g11_after_reset="$API_BODY"
      g11_after_reset_points=$(eu_get_point_snapshot "$g11_after_reset" | jq -c 'map({id,value})') || g11_after_reset_points=""
      if [ -n "$g11_stale_points" ] && [ -n "$g11_after_reset_points" ] \
        && [ "$g11_after_reset_points" = "$g11_stale_points" ] \
        && [ "$(printf '%s' "$g11_after_reset" | jq -r '.cache_epoch // empty')" = "$g11_stale_epoch" ]; then
        if printf '%s' "$g11_after_reset" | jq -e "$EU_JQ_DEFS all_stale(\"device_unreachable\")" >/dev/null; then
          pass "G11 reset truth cannot change stale cached values/epoch (received_at may advance on real stale GI)"
        else fail "G11 reset unexpectedly made dropped breaker points good"; fi
      else
        fail "G11 operator cache changed during out-of-band device reset"
      fi
      # shellcheck disable=SC2016
      eu_assert_jq_truth "$g11_truth" "${g11_before_reset:-null}" \
        '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false and .devices["breaker-1"].revision != $truth.devices["breaker-1"].revision' \
        "G11 device reset changed breaker truth"
    else
      fail "G11 operator state unavailable after device reset"
    fi
  else
    fail "G11 physical state unavailable after reset"
  fi
  eu_apply_named improved || return
  if [ "$g11_loss_verified" -ne 1 ]; then
    fail "G11 Modbus recovery not tested without the verified device_unreachable transition"
  elif eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and all_good' \
    30 "G11 Modbus recovery after improved restore"; then
    g11_loss_verified=2
    pass "G11 four observations GOOD after restored Modbus conduit"
  fi
  if ! eu_get_field; then
    fail "G11 field state read after restore failed"
    return
  fi
  reset_truth="$API_BODY"
  if eu_get_operator; then
    # shellcheck disable=SC2016
    eu_assert_jq_truth "$API_BODY" "$reset_truth" '
      point("feeder-breaker-position").value == $truth.devices["breaker-1"].position
      and point("feeder-protection-trip").value == $truth.devices["breaker-1"].protection_tripped
    ' "G11 recovered operator values equal reset truth"
  else
    fail "G11 operator read after restore failed"
  fi
}

eu_gate_G12() {
  local g12_pause_started_ms g12_measurement_loss_seen=0
  # This outage needs a healthy chain, not a successful G11 outage/recovery.
  eu_apply_named improved || return
  eu_reset_all || return
  eu_wait_for_baseline "G12 GOOD baseline before measurement loss" 30 || return
  g12_pause_started_ms=$(eu_now_ms) || g12_pause_started_ms=""
  if ! [[ "$g12_pause_started_ms" =~ ^[0-9]+$ ]]; then
    fail "G12 cannot establish monotonic timeout before pausing OpenDSS"
    return
  fi
  opendss_paused=1
  if ! docker pause "$OPENDSS" >/dev/null 2>"$TMP_DIR/pause-opendss.err"; then
    fail "G12 could not pause OpenDSS: $(cat "$TMP_DIR/pause-opendss.err")"
    eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || true
    return
  fi
  pass "G12 OpenDSS container paused"
  if eu_wait_for_operator '
    all(point("feeder-current"), point("feeder-voltage");
      .quality.iv == true and .quality.nt == false and .stale_reason == "measurement_invalid")
    and all(point("feeder-breaker-position"), point("feeder-protection-trip"); good)
  ' 5 "G12 measurement-invalid quality within five seconds" "$g12_pause_started_ms"; then
    g12_measurement_loss_seen=1
    pass "G12 current/voltage IV measurement_invalid; status points remain GOOD"
  fi
  eu_command rmu-1-command open rejected no_measurement || true
  eu_unpause_owned "$OPENDSS" opendss "$opendss_paused" || return
  if [ "$g12_measurement_loss_seen" -ne 1 ]; then
    fail "G12 measurement recovery not tested without the measurement_invalid transition"
  elif eu_wait_for_operator 'all(.points[]; good)' 10 "G12 measurement recovery"; then
    pass "G12 all four points GOOD after OpenDSS resumes"
  fi
}

eu_gate_G13() {
  local connection_seq generation baseline cursor revisions="[]" active revision audit fault_step
  # Own the initial truth even if a previous gate aborted while tripped or
  # restrictive. The next local-trip must change the device revision.
  eu_apply_named improved || return
  eu_reset_all || return
  # Settle the CC and the reset revision before taking the audit cursor.
  if ! eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete" and all_good' \
    30 "G13 CC baseline before the RTU outage"; then
    return
  fi
  connection_seq=$(printf '%s' "$WAIT_BODY" | jq -er '.link.connection_seq | numbers') || {
    fail "G13 CC connection_seq is missing or not a number: $WAIT_BODY"
    return
  }
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false
    and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision
    and .process.truth_stale == false' 10 "G13 solver agrees with the device" || return
  generation=$(printf '%s' "$WAIT_BODY" | jq -er '.devices["breaker-1"].generation | numbers') || generation=""
  baseline=$(printf '%s' "$WAIT_BODY" | jq -er '.devices["breaker-1"].revision | numbers') || baseline=""
  if ! eu_api_get "/api/field/audit?after=0"; then
    fail "G13 device audit cursor unavailable or unparsable"
    return
  fi
  cursor=$(printf '%s' "$API_BODY" | jq -er '.latest_seq | numbers') || cursor=""
  if [ -z "$generation" ] || [ -z "$baseline" ] || [ -z "$cursor" ]; then
    fail "G13 device generation, revision or audit cursor is not a number"
    return
  fi
  rtu_paused=1
  if ! docker pause "$RTU" >/dev/null 2>"$TMP_DIR/pause-rtu.err"; then
    fail "G13 could not pause RTU: $(cat "$TMP_DIR/pause-rtu.err")"
    eu_unpause_owned "$RTU" rtu "$rtu_paused" || true
    return
  fi
  pass "G13 RTU container paused"
  # Each truth change must be solved before the next one replaces it: the solver
  # polls current truth and cannot solve a superseded revision.
  for active in true false; do
    if [ "$active" = true ]; then fault_step=local-trip; else
      eu_device_step clear-fault "G13 clear process fault" || break
      fault_step=restore-device
    fi
    eu_device_step "$fault_step" \
      "G13 fault active=$active while RTU paused" || break
    revision=$(printf '%s' "$API_BODY" | jq -er '.revision | numbers') || {
      fail "G13 fault active=$active answered no numeric revision"
      break
    }
    revisions=$(printf '%s' "$revisions" | jq -c --argjson revision "$revision" '. + [$revision]')
    eu_wait_for_state "/api/field/audit?after=$cursor" \
      ".generation == $generation and any(.entries[]; .operation == \"first_solve\" and .detail.revision == $revision)" \
      10 "G13 solver first_solve of device revision $revision while RTU paused" || break
  done
  # Recovery must be a new connection: wait until the CC drops the paused RTU (t3 + t1).
  if eu_wait_for_operator '.link.state != "started"' 30 "G13 CC link loss while RTU paused"; then
    pass "G13 CC lost the IEC104 link to the paused RTU"
  fi
  eu_unpause_owned "$RTU" rtu "$rtu_paused" || return
  if eu_wait_for_operator ".link.state == \"started\" and .link.connection_seq > $connection_seq
    and .link.gi.state == \"complete\" and all_good" 30 "G13 CC recovery by a new connection and GI"; then
    pass "G13 CC recovered through a new IEC104 connection (connection_seq > $connection_seq) and a completed GI"
  fi
  if ! eu_api_get "/api/field/audit?after=$cursor"; then
    fail "G13 post-fault device audit response is unavailable or unparsable"
    return
  fi
  audit="$API_BODY"
  if [ "$revisions" = "[]" ]; then
    fail "G13 recorded no device revision from the fault sequence"
  fi
  eu_check_g13_audit "$audit" "$generation" "$baseline" "$cursor" "$revisions" || true
}

eu_gate_G14() {
  eu_reset_all || return
  eu_wait_for_field '.devices["breaker-1"].position == "closed" and .devices["breaker-1"].protection_tripped == false and .process.devices["feeder-breaker"].delivered == true and .process.devices["feeder-breaker"].solved_revision == .devices["breaker-1"].revision' 15 "G14 reset truth and process recovery" || true
  if eu_wait_for_operator '
    .link.state == "started" and .link.gi.state == "complete" and all_good
    and point("feeder-breaker-position").value == "closed"
  ' 30 "G14 CC observes reset through IEC104"; then
    pass "G14 reset state recovered through RTU/IEC104/CC chain"
  fi
  if eu_apply_named weak; then
    pass "G14 applied weak policy"
  else
    fail "G14 could not leave range on weak policy"
  fi
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
  for gate in G0 G1 G2 G3 G4 G5 G6 G7 G8 G9 G10 G11 G12 G13 G15 G16 G17 G18 G19 G14; do
    eu_run_gate "$gate"
  done
  exit 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  eu_gate_main "$@"
fi
