#!/usr/bin/env bash
# Headless increment-3 exit gate for the live EU IEC104 package.
#
# Usage: scripts/eu-iec104-smoke.sh [--dry-run]
# This script is intentionally destructive while running: it changes the
# active policy, breaker truth, and pauses range services. It always restores
# the weak policy + instructor reset and unpauses services it paused.

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

case "${1:-}" in
  --dry-run|--list)
    if [ "$#" -ne 1 ]; then
      echo "usage: $0 [--dry-run|--list]" >&2
      exit 2
    fi
    cat <<'PLAN'
EU IEC104 gate plan (no Docker or backend access):
  G0  Ready/package, policy warnings, nft rules, image/kernel/tcp_loose, sockets/routes/sysctls/Compose
  G1  GOOD CC baseline, truth/solver/operator agreement, advancing solve
  G2  GPS pause/unpause and timed source_time_invalid transitions
  G3  Open/close/no-op/lockout, both-hop capture, IEC104/Modbus decode
  G4  Kali GI succeeds under weak
  G5  Improved Kali connect fails; fresh DENY; CC stays started; validator FAIL
  G6  Established Kali monitor survives improved, exact tuple kill, exits 3, no later ASDUs, reconnect fails
  G7  Remove CC allow; read back; kill exact tuple; CC points link_lost
  G8  Fault/trip changes truth and solver while CC cache stays unchanged
  G9  Restore improved; CC reconnect/GI and truth converge
  G10 EU validator PASS
  G11 Remove RTU-to-breaker allow; exact Modbus kill; stale/reset/recover
  G12 OpenDSS pause yields measurement_invalid only; unpause recovers
  G13 RTU pause, truth revisions, solver audit, CC GI recovery
  G14 Instructor reset recovers the chain; apply weak
  EXIT Restore weak + instructor reset; unpause services paused by this run
PLAN
    exit 0
    ;;
  "")
    ;;
  *)
    echo "usage: $0 [--dry-run|--list]" >&2
    exit 2
    ;;
esac

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
gps_paused=0
opendss_paused=0
rtu_paused=0
TMP_DIR=""
CAPTURE_PID=""
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

# Validate the backend target before touching Docker, then arm restoration before
# the first policy mutation. A wrong or unavailable package is a hard refusal.
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
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/range.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/eu-range.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib/eu-monitor.sh"
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

preflight_ready=1
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
  "$RTU" "$CC" "$BREAKER" "$GPS" "$OPENDSS"; do
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

gate_start G0
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
check_listener "$BREAKER" "10.99.99.22:8080" "Feeder-breaker instructor API" 2
check_listener "$GPS" "10.30.30.50:8080" "GPS station-clock API"
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
gate_end G0
gate_start G1
if eu_api_post "/api/instructor/reset" '{}'; then
  eu_assert_device_state_response "G1 instructor baseline reset"
else
  fail "G1 instructor baseline reset returned no parseable response"
fi
if eu_wait_for_operator '.link.state == "started" and .link.gi.state == "complete"' 30 "G1 CC link and GI"; then
  eu_assert_jq "$WAIT_BODY" '
    ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort) and all(.points[]; .quality.iv == false and .quality.nt == false and (.stale_reason == null or .stale_reason == "")) and all(.points[] | select(.id == "feeder-breaker-position" or .id == "feeder-protection-trip"); .source_time_invalid == false and (.source_time | type == "string" and length > 0))
  ' "G1 four expected operator points GOOD with synchronized status-point time"
else
  fail "G1 CC did not reach started + GI-complete baseline"
fi
if eu_get_instructor; then
  truth="$API_BODY"
  if eu_get_operator; then
    operator="$API_BODY"
    # shellcheck disable=SC2016
    eu_assert_jq_truth "$operator" "$truth" '
      .points as $points
      | ($points | map(select(.id == "feeder-breaker-position")) | .[0].value)
          == $truth.device.position
      and ($points | map(select(.id == "feeder-protection-trip")) | .[0].value)
          == $truth.device.protection_tripped
      and (($points | map(select(.id == "feeder-current")) | .[0].value)
          - $truth.process.current_a | fabs) <= 0.5
      and (($points | map(select(.id == "feeder-voltage")) | .[0].value)
          - $truth.process.load_voltage_kv_ll | fabs) <= 0.05
      and $truth.process.truth_revision == $truth.device.revision
    ' "G1 operator position/trip/current/voltage match instructor truth and solver revision"
    first_seq=$(printf '%s' "$truth" | jq -r '.process.solve_seq // empty')
    sleep 2
    if eu_get_instructor; then
      next_seq=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
      if [[ "$first_seq" =~ ^[0-9]+$ ]] && [[ "$next_seq" =~ ^[0-9]+$ ]] \
        && [ "$next_seq" -gt "$first_seq" ]; then
        pass "G1 OpenDSS solve_seq advances ($first_seq -> $next_seq)"
      else
        fail "G1 solve_seq did not advance ($first_seq -> $next_seq)"
      fi
    else
      fail "G1 second instructor state read was unavailable"
    fi
  else
    fail "G1 operator state is unavailable or unparsable"
  fi
else
  fail "G1 instructor truth/process state is unavailable or unparsable"
fi
gate_end G1
gate_start G2
gps_paused=1
if docker pause "$GPS" >/dev/null 2>"$TMP_DIR/pause-gps.err"; then
  pass "G2 GPS container paused"
else
  fail "G2 could not pause GPS: $(cat "$TMP_DIR/pause-gps.err")"
fi
sleep 6
if eu_wait_command open completed; then
  :
fi
g2_invalid_seen=0
if eu_wait_for_operator '
  any(.points[]; .id == "feeder-breaker-position"
      and .value == "open" and .source_time_invalid == true
      and (.source_time | type == "string" and length > 0))
' 5 "G2 status point with invalid station time"; then
  g2_invalid_seen=1
  pass "G2 operator status change carries source_time_invalid=true"
else
  fail "G2 timed operator status change did not carry invalid source time"
fi
g2_gps_unpaused=0
if eu_unpause_owned "$GPS" gps "$gps_paused"; then
  g2_gps_unpaused=1
fi
if [ "$g2_gps_unpaused" -eq 1 ] \
  && eu_wait_for_instructor '.process.truth_revision == .device.revision' 10 "G2 solver after GPS resumes"; then
  :
fi
sleep 2
if eu_wait_command close completed; then
  :
fi
if [ "$g2_invalid_seen" -eq 1 ] && [ "$g2_gps_unpaused" -eq 1 ] \
  && eu_wait_for_operator '
  any(.points[]; .id == "feeder-breaker-position"
      and .value == "closed" and .source_time_invalid == false
      and (.source_time | type == "string" and length > 0))
' 10 "G2 status point with synchronized station time"; then
  pass "G2 next status change carries source_time_invalid=false"
else
  fail "G2 timed operator status change did not recover synchronized source time"
fi
gate_end G2
gate_start G3
CAPTURE_PATH="/tmp/eu-iec104-smoke-$$.pcap"
CAPTURE_PID_FILE="/tmp/eu-iec104-smoke-$$.tcpdump.pid"
CAPTURE_LOG="/tmp/eu-iec104-smoke-$$.tcpdump.log"
# shellcheck disable=SC2034
capture_running=1
if ! docker exec "$FW" sh -c "rm -f '$CAPTURE_PATH' '$CAPTURE_PID_FILE' '$CAPTURE_LOG'; \
  tcpdump -i any -s 0 -U -w '$CAPTURE_PATH' 'tcp port 2404 or tcp port 502' \
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
  if eu_wait_for_instructor '.device.position == "open" and .process.breaker == "open" and .process.current_a < 0.1 and .process.truth_revision == .device.revision' 10 "G3 open truth/solver"; then
    :
  fi
  if eu_wait_for_operator 'any(.points[]; .id == "feeder-breaker-position" and .value == "open")' 10 "G3 open operator feedback"; then
    :
  fi
fi
if eu_wait_command close completed; then
  if eu_wait_for_instructor '.device.position == "closed" and .process.breaker == "closed" and (.process.current_a - 28.9 | fabs) <= 1.5 and .process.truth_revision == .device.revision' 10 "G3 close truth/solver"; then
    :
  fi
  if eu_wait_for_operator 'any(.points[]; .id == "feeder-breaker-position" and .value == "closed")' 10 "G3 close operator feedback"; then
    :
  fi
fi
if eu_wait_command open completed; then
  if eu_wait_for_operator 'any(.points[]; .id == "feeder-breaker-position" and .value == "open")' 10 "G3 second open feedback"; then
    :
  fi
fi
if eu_get_instructor; then
  before_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.device.revision // empty')
else
  before_noop_revision=""
  fail "G3 pre-no-op truth read unavailable"
fi
if eu_wait_command open completed; then
  eu_assert_jq "$COMMAND_BODY" '.feedback.status == "matched" and .feedback.value == "open" and .feedback.received_at != null' "G3 no-op open completed with matched COT 11 feedback"
fi
if eu_get_instructor; then
  after_noop_revision=$(printf '%s' "$API_BODY" | jq -r '.device.revision // empty')
  if [ -n "$before_noop_revision" ] && [ "$after_noop_revision" = "$before_noop_revision" ]; then
    pass "G3 no-op command did not change device truth revision"
  else
    fail "G3 no-op changed or could not verify device revision ($before_noop_revision -> $after_noop_revision)"
  fi
else
  fail "G3 post-no-op truth read unavailable"
fi
if eu_api_post "/api/instructor/fault" '{"active":true}'; then
  eu_assert_device_state_response "G3 instructor local fault"
else
  fail "G3 instructor fault request returned no parseable response"
fi
if eu_wait_for_instructor '.device.lockout == true and .device.protection_tripped == true' 10 "G3 local lockout"; then
  :
fi
if eu_wait_command close rejected; then
  :
fi
if eu_api_post "/api/instructor/reset" '{}'; then
  eu_assert_device_state_response "G3 instructor reset"
else
  fail "G3 instructor reset returned no parseable response"
fi
if ! eu_stop_capture; then
  :
fi
if [ -n "$CAPTURE_PID" ]; then
  if docker cp "$FW:$CAPTURE_PATH" "$TMP_DIR/eu-iec104-gate.pcap" >/dev/null 2>"$TMP_DIR/docker-cp.err"; then
    eu_decode_capture
  else
    fail "G3 could not retrieve firewall capture: $(cat "$TMP_DIR/docker-cp.err")"
  fi
else
  fail "G3 capture PID is empty; cannot retrieve protocol evidence"
fi

if docker exec "$FW" rm -f "$CAPTURE_PATH" "$CAPTURE_PID_FILE" "$CAPTURE_LOG" >/dev/null 2>&1; then
  CAPTURE_PATH=""
  CAPTURE_PID_FILE=""
  CAPTURE_LOG=""
else
  fail "G3 temporary firewall capture files could not be removed"
fi
gate_end G3
gate_start G4
if eu_apply_named weak; then
  eu_run_kali_gi G4 0 || true
else
  fail "G4 cannot perform weak-policy Kali GI"
fi
gate_end G4
gate_start G5
if eu_apply_named improved; then
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
else
  fail "G5 cannot test improved-policy denial"
fi
gate_end G5
gate_start G6
g6_kill_verified=0
if eu_apply_named weak && eu_monitor_start; then
  if eu_monitor_wait_event connected 15 && eu_monitor_wait_event gi_complete 15; then
    pass "G6 monitor connected and completed GI under weak"
  else
    fail "G6 monitor failed to establish its IEC104 session"
  fi
  if eu_monitor_asdu_count; then
    asdus_before=$MONITOR_ASDUS
    if eu_apply_named improved; then
      if eu_monitor_read && [ "$MONITOR_LINES" -gt 0 ] \
        && printf '%s\n' "$MONITOR_BODY" | jq -s -e --arg peer "$RTU_IP:2404" '
          [.[] | select(.event == "connected")] as $connected
          | ($connected | length) == 1
            and $connected[0].local == null and $connected[0].peer == $peer
        ' >/dev/null 2>&1; then
        if eu_read_sessions 2404 10.10.10.50 \
          && eu_session_tuple "$SESSION_BODY" 10.10.10.50 "$RTU_IP" 2404; then
          kali_tuple="$SESSION_TUPLE"
          pass "G6 non-empty dport 2404 session list contains the monitor's exact Kali tuple"
          asdu_observation_valid=1
          for _ in 1 2 3 4 5 6 7 8 9 10; do
            sleep 1
            if ! eu_monitor_asdu_count; then
              asdu_observation_valid=0
              break
            fi
          done
          if [ "$asdu_observation_valid" -eq 1 ] \
            && [ "${MONITOR_ASDUS:-0}" -gt "$asdus_before" ]; then
            pass "G6 established IEC104 session continued receiving ASDUs for 10s after improved apply"
          else
            fail "G6 established monitor received no new ASDUs during the 10s observation"
          fi
          kill_started_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || kill_started_ms=""
          kill_epoch=$(python3 -c 'import time; print(time.time_ns())') || kill_epoch=""
          if ! [[ "$kill_started_ms" =~ ^[0-9]+$ ]] || [ -z "$kill_epoch" ]; then
            fail "G6 could not timestamp conntrack kill"
          elif eu_kill_tuple "$kali_tuple"; then
            if eu_monitor_finish 3 30 "$kill_started_ms"; then
              if eu_monitor_no_asdu_after "$kill_epoch"; then
                g6_kill_verified=1
              fi
            fi
          fi
        else
          fail "G6 session lookup did not yield exactly one Kali-to-RTU tuple"
        fi
      fi
      else
        fail "G6 monitor must emit exactly one connected event with local=null and peer=$RTU_IP:2404"
      fi
    else
      fail "G6 monitor ASDU JSON could not be parsed"
    fi
  if [ "$g6_kill_verified" -eq 1 ]; then
    eu_run_kali_gi G6-reconnect 2 || true
  else
    fail "G6 reconnect denial was not tested because the established-session kill was unverified"
  fi
else
  fail "G6 could not start a weak-policy persistent monitor"
fi
gate_end G6
gate_start G7
g7_loss_verified=0
if eu_get_operator; then
  before_g7="$API_BODY"
  if printf '%s' "$before_g7" | jq -e '.link.state == "started"' >/dev/null 2>&1; then
    g7_policy="$TMP_DIR/improved-without-cc.json"
    if eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$CC_IP" "$RTU_IP" 2404 "$g7_policy"; then
      if eu_policy_has_no_ephemeral_allow "$g7_policy"; then
        pass "G7 custom policy has no ICS keys or ephemeral destination-port allow"
      else
        fail "G7 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
      fi
      if eu_apply_custom "$g7_policy" "G7 improved minus CC allow"; then
        eu_check_nft_chain G7-custom || true
        if eu_read_sessions 2404 "$CC_IP" \
          && eu_session_tuple "$SESSION_BODY" "$CC_IP" "$RTU_IP" 2404; then
            cc_tuple="$SESSION_TUPLE"
            pass "G7 exact established CC tuple exists before kill"
            g7_kill_started_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || g7_kill_started_ms=""
            if ! [[ "$g7_kill_started_ms" =~ ^[0-9]+$ ]]; then
              fail "G7 cannot establish monotonic timeout before conntrack kill"
            elif eu_kill_tuple "$cc_tuple"; then
              if eu_wait_for_operator '
                .link.state != "started"
                and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
                  and all(.points[]; .quality.iv == true and .quality.nt == true
                    and .stale_reason == "link_lost"))
              ' 30 "G7 CC link loss with four link_lost points" "$g7_kill_started_ms"; then
                g7_loss_verified=1
                pass "G7 CC link is not started and all points are IV+NT link_lost"
              fi
            fi
        else
          fail "G7 session lookup did not yield exactly one CC-to-RTU tuple"
        fi
      fi
    fi
  else
    fail "G7 operator link state was not started before applying the CC restriction: $before_g7"
  fi
else
  fail "G7 operator state unavailable before restrictive policy"
fi
gate_end G7
gate_start G8
if [ "$g7_loss_verified" -eq 1 ] && eu_get_operator; then
  g8_before="$API_BODY"
  g8_epoch=$(printf '%s' "$g8_before" | jq -r '.cache_epoch // empty')
  g8_points=$(eu_get_point_snapshot "$g8_before") || g8_points=""
  if [ -z "$g8_epoch" ] || [ -z "$g8_points" ] \
    || ! printf '%s' "$g8_before" | jq -e \
      '([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)' \
      >/dev/null 2>&1; then
    fail "G8 cache epoch, four-point shape, or point snapshot is empty/invalid"
  fi
  for active in true false true; do
    if eu_api_post "/api/instructor/fault" "{\"active\":$active}"; then
      eu_assert_device_state_response "G8 instructor fault active=$active"
    else
      fail "G8 instructor fault active=$active returned no parseable response"
    fi
    if [ "$active" = "true" ]; then
      if eu_wait_for_instructor '.device.position == "open" and .device.lockout == true and .device.protection_tripped == true and .process.breaker == "open" and .process.current_a < 0.1 and .process.truth_revision == .device.revision' 10 "G8 local protection and open solver"; then
        :
      fi
    else
      if eu_wait_for_instructor '.process.truth_revision == .device.revision' 10 "G8 solver after fault cleared"; then
        :
      fi
    fi
    if eu_get_instructor; then
      g8_truth="$API_BODY"
      if eu_get_operator; then
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
          eu_assert_jq "$g8_truth" '.device.position == "open" and .device.lockout == true and .device.protection_tripped == true and .process.breaker == "open" and .process.current_a < 0.1' "G8 instructor truth and independent solver show open/tripped"
        fi
      else
        fail "G8 operator-state route was not HTTP 200 JSON during fault active=$active"
      fi
    else
      fail "G8 instructor truth/solver response unavailable during fault active=$active"
    fi
  done
  if eu_get_instructor; then
    g8_seq1=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
    sleep 2
    if eu_get_instructor; then
      g8_seq2=$(printf '%s' "$API_BODY" | jq -r '.process.solve_seq // empty')
      if [[ "$g8_seq1" =~ ^[0-9]+$ ]] && [[ "$g8_seq2" =~ ^[0-9]+$ ]] \
        && [ "$g8_seq2" -gt "$g8_seq1" ]; then
        pass "G8 solver solve_seq advances independently while operator cache remains stale"
      else
        fail "G8 solver solve_seq failed to advance ($g8_seq1 -> $g8_seq2)"
      fi
    else
      fail "G8 second solver state response failed"
    fi
  else
    fail "G8 solver state response failed"
  fi
else
  fail "G8 cannot test cache isolation without the verified G7 CC disconnect"
fi
gate_end G8

gate_start G9
if eu_apply_named improved; then
  if [ "$g7_loss_verified" -eq 1 ] && eu_wait_for_operator '
    .link.state == "started" and .link.gi.state == "complete"
    and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
    and all(.points[]; .quality.iv == false
      and .quality.nt == false and (.stale_reason == null or .stale_reason == ""))
  ' 30 "G9 CC reconnect/GI and four GOOD points"; then
    pass "G9 restored IEC104 observations after GI"
  fi
  if [ "$g7_loss_verified" -eq 1 ] && eu_get_instructor; then
    g9_truth="$API_BODY"
    if eu_get_operator; then
      # shellcheck disable=SC2016
      eu_assert_jq_truth "$API_BODY" "$g9_truth" '
        any(.points[]; .id == "feeder-breaker-position" and .value == $truth.device.position)
        and any(.points[]; .id == "feeder-protection-trip" and .value == $truth.device.protection_tripped)
        and $truth.device.position == "open" and $truth.device.protection_tripped == true
      ' "G9 restored operator state equals open/tripped instructor truth"
    else
      fail "G9 operator state unavailable after reconnect"
    fi
  else
    fail "G9 cannot verify recovery without the G7 disconnect or instructor truth"
  fi
else
  fail "G9 cannot restore improved policy"
fi
gate_end G9
gate_start G10
eu_validate_scenario PASS || true
gate_end G10
gate_start G11
g11_loss_verified=0
g11_policy="$TMP_DIR/improved-without-rtu-modbus.json"
if eu_make_custom_policy "$PACKAGE/firewall/improved.json" "$RTU_IP" "$BREAKER_IP" 502 "$g11_policy"; then
  if eu_policy_has_no_ephemeral_allow "$g11_policy"; then
    pass "G11 custom policy has no ICS keys or ephemeral destination-port allow"
  else
    fail "G11 custom policy has an ICS key, unbounded TCP allow, or ephemeral-port allow"
  fi
  if eu_apply_custom "$g11_policy" "G11 improved minus RTU-to-breaker allow"; then
    eu_check_nft_chain G11-custom || true
    if eu_get_operator; then
      :
    else
      fail "G11 cannot snapshot operator points before Modbus loss"
    fi
    if eu_read_sessions 502 "$RTU_IP" \
      && eu_session_tuple "$SESSION_BODY" "$RTU_IP" "$BREAKER_IP" 502; then
        g11_tuple="$SESSION_TUPLE"
        pass "G11 exact RTU-to-breaker Modbus session exists before kill"
        g11_loss_started_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || g11_loss_started_ms=""
        if ! [[ "$g11_loss_started_ms" =~ ^[0-9]+$ ]]; then
          fail "G11 cannot establish monotonic timeout before conntrack kill"
        elif eu_kill_tuple "$g11_tuple"; then
          stale_ms=$(jq -r '.devices[] | select(.id == "feeder-breaker") | .stale_after_ms' \
            "$PACKAGE/protocols/points.json" 2>/dev/null) || stale_ms=""
          if [[ "$stale_ms" =~ ^[0-9]+$ ]] && [ "$stale_ms" -eq 3000 ]; then
            if eu_wait_for_operator '
              .link.state == "started"
              and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
              and all(.points[]; .quality.iv == true and .quality.nt == true
                and .stale_reason == "device_unreachable")
            ' 5 "G11 RTU device_unreachable within stale_after_ms + 2s" "$g11_loss_started_ms"; then
              g11_loss_verified=1
              pass "G11 four points IV+NT device_unreachable with IEC104 link started"
            fi
          else
            fail "G11 points.json stale_after_ms missing/invalid or differs from 3000: $stale_ms"
          fi
        fi
        if eu_get_operator; then
          g11_stale="$API_BODY"
          g11_stale_points=$(eu_get_point_snapshot "$g11_stale") || g11_stale_points=""
          g11_stale_epoch=$(printf '%s' "$g11_stale" | jq -r '.cache_epoch // empty')
          if [ -z "$g11_stale_epoch" ] || [ -z "$g11_stale_points" ] \
            || ! printf '%s' "$g11_stale" | jq -e \
              '([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)' \
              >/dev/null 2>&1; then
            fail "G11 stale operator cache lacks its epoch or four expected points"
          fi
        else
          g11_stale_points=""
          g11_stale_epoch=""
          fail "G11 operator state unavailable after stale transition"
        fi
        if eu_api_post "/api/instructor/reset" '{}'; then
          eu_assert_device_state_response "G11 instructor reset changes truth"
        else
          fail "G11 instructor reset returned no parseable response"
        fi
        if eu_get_instructor; then
          g11_truth="$API_BODY"
          if eu_get_operator; then
            g11_after_reset="$API_BODY"
            g11_after_reset_points=$(eu_get_point_snapshot "$g11_after_reset") || g11_after_reset_points=""
            if [ -n "$g11_stale_points" ] && [ -n "$g11_after_reset_points" ] \
              && [ "$g11_after_reset_points" = "$g11_stale_points" ] \
              && [ "$(printf '%s' "$g11_after_reset" | jq -r '.cache_epoch // empty')" = "$g11_stale_epoch" ]; then
              pass "G11 instructor reset changed truth without changing cached values/times/epoch"
            else
              fail "G11 operator cache changed during out-of-band instructor reset"
            fi
            # shellcheck disable=SC2016
            eu_assert_jq_truth "$g11_truth" "$g9_truth" \
              '.device.position == "closed" and .device.protection_tripped == false and .device.revision != $truth.device.revision' \
              "G11 instructor reset changed breaker truth"
          else
            fail "G11 operator state unavailable after instructor reset"
          fi
        else
          fail "G11 instructor truth unavailable after reset"
        fi
        if eu_apply_named improved; then
          if [ "$g11_loss_verified" -eq 1 ] && eu_wait_for_operator '
            .link.state == "started" and .link.gi.state == "complete"
            and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
            and all(.points[]; .quality.iv == false
              and .quality.nt == false and (.stale_reason == null or .stale_reason == ""))
          ' 30 "G11 Modbus recovery after improved restore"; then
            g11_loss_verified=2
            pass "G11 four observations GOOD after restored Modbus conduit"
          fi
          if eu_get_instructor; then
            reset_truth="$API_BODY"
            if eu_get_operator; then
              # shellcheck disable=SC2016
              eu_assert_jq_truth "$API_BODY" "$reset_truth" '
                any(.points[]; .id == "feeder-breaker-position" and .value == $truth.device.position)
                and any(.points[]; .id == "feeder-protection-trip" and .value == $truth.device.protection_tripped)
              ' "G11 recovered operator values equal reset truth"
            else
              fail "G11 operator read after restore failed"
            fi
          else
            fail "G11 instructor state read after restore failed"
          fi
        fi
    else
      fail "G11 session lookup did not yield exactly one RTU-to-breaker tuple"
    fi
  fi
fi
gate_end G11

gate_start G12
g12_pause_started_ms=$(python3 -c 'import time; print(int(time.monotonic() * 1000))') || g12_pause_started_ms=""
if ! [[ "$g12_pause_started_ms" =~ ^[0-9]+$ ]]; then
  fail "G12 cannot establish monotonic timeout before pausing OpenDSS"
fi
g12_pause_succeeded=0
opendss_paused=1
if docker pause "$OPENDSS" >/dev/null 2>"$TMP_DIR/pause-opendss.err"; then
  g12_pause_succeeded=1
  pass "G12 OpenDSS container paused"
else
  fail "G12 could not pause OpenDSS: $(cat "$TMP_DIR/pause-opendss.err")"
fi
g12_measurement_loss_seen=0
if [ "$g11_loss_verified" -eq 2 ] && [[ "$g12_pause_started_ms" =~ ^[0-9]+$ ]] \
  && [ "$g12_pause_succeeded" -eq 1 ] \
  && eu_wait_for_operator '
  any(.points[]; .id == "feeder-current" and .quality.iv == true
    and .quality.nt == false and .stale_reason == "measurement_invalid")
  and any(.points[]; .id == "feeder-voltage" and .quality.iv == true
    and .quality.nt == false and .stale_reason == "measurement_invalid")
  and all(.points[] | select(.id == "feeder-breaker-position"
    or .id == "feeder-protection-trip"); .quality.iv == false and .quality.nt == false)
' 5 "G12 measurement-invalid quality within five seconds" "$g12_pause_started_ms"; then
  g12_measurement_loss_seen=1
  pass "G12 current/voltage IV measurement_invalid; status points remain GOOD"
fi
g12_unpause_succeeded=0
if eu_unpause_owned "$OPENDSS" opendss "$opendss_paused"; then
  g12_unpause_succeeded=1
fi
if [ "$g12_measurement_loss_seen" -eq 1 ] && [ "$g12_unpause_succeeded" -eq 1 ] \
  && eu_wait_for_operator '
  all(.points[]; .quality.iv == false and .quality.nt == false
    and (.stale_reason == null or .stale_reason == ""))
' 10 "G12 measurement recovery"; then
  pass "G12 all four points GOOD after OpenDSS resumes"
fi
gate_end G12

gate_start G13
g13_pause_succeeded=0
rtu_paused=1
if docker pause "$RTU" >/dev/null 2>"$TMP_DIR/pause-rtu.err"; then
  g13_pause_succeeded=1
  pass "G13 RTU container paused"
else
  fail "G13 could not pause RTU: $(cat "$TMP_DIR/pause-rtu.err")"
fi
if eu_get_instructor; then
  g13_before="$API_BODY"
  g13_revision=$(printf '%s' "$g13_before" | jq -r '.device.revision // empty')
  g13_generation=$(printf '%s' "$g13_before" | jq -r '.device.generation // empty')
else
  g13_revision=""
  g13_generation=""
  fail "G13 initial instructor state unavailable"
fi
if eu_api_get "/api/instructor/audit?after=0"; then
  g13_audit_before="$API_BODY"
  g13_audit_cursor=$(printf '%s' "$g13_audit_before" | jq -r '.latest_seq // empty')
else
  g13_audit_cursor=""
  fail "G13 device audit cursor unavailable or unparsable"
fi
g13_revisions=""
for active in true false; do
  if eu_api_post "/api/instructor/fault" "{\"active\":$active}"; then
    eu_assert_device_state_response "G13 fault active=$active while RTU paused"
  else
    fail "G13 fault active=$active returned no parseable response"
  fi
  if eu_get_instructor; then
    revision=$(printf '%s' "$API_BODY" | jq -r '.device.revision // empty')
    if [[ "$revision" =~ ^[0-9]+$ ]]; then
      g13_revisions="$g13_revisions $revision"
    else
      fail "G13 device revision after fault active=$active is missing/unparsable"
    fi
  else
    fail "G13 instructor state unavailable after fault active=$active"
  fi
done
g13_unpause_succeeded=0
if eu_unpause_owned "$RTU" rtu "$rtu_paused"; then
  g13_unpause_succeeded=1
fi
if [ "$g13_pause_succeeded" -eq 1 ] && [ "$g13_unpause_succeeded" -eq 1 ] \
  && eu_wait_for_operator '
  .link.state == "started" and .link.gi.state == "complete"
  and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
  and all(.points[]; .quality.iv == false
    and .quality.nt == false and (.stale_reason == null or .stale_reason == ""))
' 30 "G13 CC recovery by reconnect/GI after RTU resumes"; then
  pass "G13 CC recovered through IEC104 GI"
fi
if eu_api_get "/api/instructor/audit?after=$g13_audit_cursor"; then
  g13_audit="$API_BODY"
  g13_changed_revisions=$(printf '%s' "$g13_audit" | jq -r '
    [.entries[]
      | select(.operation != "first_solve"
        and (.before.revision | type == "number")
        and (.after.revision | type == "number")
        and .after.revision > .before.revision)
      | .after.revision]
    | unique | .[]
  ' 2>/dev/null) || g13_changed_revisions=""
  if ! [[ "$g13_revision" =~ ^[0-9]+$ ]] || [ -z "$g13_generation" ] \
    || ! [[ "$g13_audit_cursor" =~ ^[0-9]+$ ]] || [ -z "$g13_revisions" ] \
    || [ -z "$g13_changed_revisions" ]; then
    fail "G13 generation/revision/audit cursor or audited revision list is empty/invalid"
  fi
  eu_assert_jq "$g13_audit" \
    '(.entries | type == "array" and length > 0) and all(.entries[] | select(.operation == "first_solve"); (.detail.revision | type == "number"))' \
    "G13 device audit has parseable first_solve revision entries"
  if ! printf '%s' "$g13_audit" | jq -e --arg generation "$g13_generation" \
    --argjson baseline "${g13_revision:-0}" \
    '.generation == $generation and all(.entries[] | select(.operation == "first_solve"); .detail.revision > $baseline)' \
    >/dev/null 2>&1; then
    fail "G13 audit generation or post-baseline first_solve evidence is invalid"
  fi
  for revision in $g13_changed_revisions; do
    if printf '%s' "$g13_audit" | jq -e --argjson revision "$revision" '
      any(.entries[]; .operation == "first_solve" and .detail.revision == $revision)
    ' >/dev/null 2>&1; then
      pass "G13 solver first_solve audit covers device revision $revision"
    else
      fail "G13 solver lacks first_solve audit for device revision $revision"
    fi
  done
  for revision in $g13_revisions; do
    if printf '%s\n' "$g13_changed_revisions" | grep -Fxq "$revision"; then
      pass "G13 observed truth revision $revision is present in device audit"
    else
      fail "G13 observed truth revision $revision is missing from the change audit"
    fi
  done
else
  fail "G13 post-fault device audit response is unavailable or unparsable"
fi
gate_end G13

gate_start G14
if eu_api_post "/api/instructor/reset" '{}'; then
  eu_assert_device_state_response "G14 instructor reset"
else
  fail "G14 instructor reset returned no parseable response"
fi
if eu_wait_for_instructor '.device.position == "closed" and .device.protection_tripped == false and .process.truth_revision == .device.revision' 15 "G14 reset truth and process recovery"; then
  :
fi
if eu_wait_for_operator '
  .link.state == "started" and .link.gi.state == "complete"
  and ([.points[].id] | sort) == (["feeder-breaker-position","feeder-protection-trip","feeder-current","feeder-voltage"] | sort)
  and all(.points[]; .quality.iv == false
    and .quality.nt == false and (.stale_reason == null or .stale_reason == ""))
  and any(.points[]; .id == "feeder-breaker-position" and .value == "closed")
' 30 "G14 CC observes reset through IEC104"; then
  pass "G14 reset state recovered through RTU/IEC104/CC chain"
fi
if eu_apply_named weak; then
  pass "G14 applied weak policy"
else
  fail "G14 could not leave range on weak policy"
fi
gate_end G14
exit 0
