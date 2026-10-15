# shellcheck shell=bash
# EU range readiness, network boundaries and the initial GOOD baseline (G0-G1).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_check_tools() {
  local tool missing=0
  for tool in curl jq python3 docker tshark; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      fail "required host tool missing: $tool"
      missing=1
    fi
  done
  if ! docker compose version >/dev/null 2>&1; then
    fail "required Docker Compose v2 is unavailable"
    missing=1
  fi
  if [ "$missing" -ne 0 ]; then
    return 1
  fi
  return 0
}

# Check a service's required listening address and count, using ss inside the
# service image. A missing ss utility or an unparsable table fails closed.
# Address *:PORT means one wildcard listener (ss prints *, 0.0.0.0 or [::]).
check_listener() {
  local container="$1" address="$2" label="$3" expected_count="${4:-1}" listeners port
  port="${address##*:}"
  listeners=$(docker exec "$container" ss -ltn 2>"$TMP_DIR/ss.err") || {
    fail "$label listener inspection failed (ss -ltn): $(cat "$TMP_DIR/ss.err")"
    return 1
  }
  if printf '%s\n' "$listeners" | awk -v endpoint="$address" -v port="$port" -v expected="$expected_count" '
    $4 == endpoint ||
      (endpoint == "*:" port && ($4 == "0.0.0.0:" port || $4 == "[::]:" port)) { found++ }
    NR > 1 && substr($4, length($4) - length(port)) == ":" port { count++ }
    END { exit(found == 1 && count == expected ? 0 : 1) }
  '; then
    pass "$label listens on $address with exactly $expected_count listener(s) on port $port"
  else
    fail "$label is not bound exactly as required at $address (expected $expected_count listener(s) on port $port); ss -ltn: $listeners"
  fi
}

# Read exactly one default gateway from Linux's /proc/net/route table.
default_gateway() {
  local container="$1" routes gateway
  routes=$(docker exec "$container" cat /proc/net/route 2>/dev/null) || return 1
  gateway=$(printf '%s\n' "$routes" | awk '
    $2 == "00000000" && $8 == "00000000" { count++; value = $3 }
    END { if (count != 1) exit 1; print value }
  ') || return 1
  case "$gateway" in *[!0123456789abcdefABCDEF]*|'') return 1 ;; esac
  [ "${#gateway}" -eq 8 ] || return 1
  printf '%d.%d.%d.%d' \
    "$((16#${gateway:6:2}))" "$((16#${gateway:4:2}))" \
    "$((16#${gateway:2:2}))" "$((16#${gateway:0:2}))"
}
check_gateway() {
  local container="$1" expected="$2" actual
  actual=$(default_gateway "$container") || actual=""
  if [ "$actual" = "$expected" ]; then
    pass "$container has exactly one default route via $expected"
  else
    fail "$container default route is $actual, expected exactly $expected"
  fi
}
eu_container_running() {
  local container="$1" status
  status=$(docker inspect -f '{{.State.Status}}' "$container" 2>/dev/null) || status=""
  [ "$status" = "running" ]
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
# New increment-4 services retain the same non-forwarding/loopback boundaries.
eu_inc4_boundaries() {
  local node field process mgmt container forwarding
  for node in 1 2 3; do
    case "$node" in
      1) container=rangerdanger-rmu-1; field=21; process=31; mgmt=24 ;;
      2) container=rangerdanger-rmu-2; field=22; process=32; mgmt=25 ;;
      3) container=rangerdanger-oltc-1; field=23; process=33; mgmt=26 ;;
    esac
    check_listener "$container" "10.40.40.$field:502" "$container Modbus"
    check_listener "$container" "10.50.50.$process:8080" "$container truth" 2
    check_listener "$container" "10.99.99.$mgmt:8080" "$container sim" 2
    check_gateway "$container" 10.40.40.2
  done
  check_listener rangerdanger-historian 10.70.70.40:8081 "historian ingest"
  check_listener rangerdanger-historian 10.70.70.40:8080 "historian DMZ read" 2
  check_listener rangerdanger-historian 10.99.99.40:8080 "historian mgmt read" 2
  check_gateway rangerdanger-historian 10.70.70.2
  check_gateway rangerdanger-openplc 10.30.30.2
  for container in rangerdanger-rmu-1 rangerdanger-rmu-2 rangerdanger-oltc-1 rangerdanger-historian rangerdanger-openplc; do
    forwarding=$(docker exec "$container" cat /proc/sys/net/ipv4/ip_forward) || forwarding=""
    if [ "$forwarding" = 0 ]; then pass "$container ip_forward=0"
    else fail "$container ip_forward=$forwarding (expected 0)"; fi
  done
}
