#!/usr/bin/env bash
# Common functions for scenario scripts
# Source this at the top of each scenario script

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Backend API (from host via proxy)
API_URL="${API_URL:-http://localhost:8088/api}"

# Containd firewall
CONTAIND_URL="${CONTAIND_URL:-http://localhost:9080}"

# Device addresses — these go through docker exec since field devices
# are on an internal network not exposed to the host.
# The ATTACK_CONTAINER variable selects which container runs the attack curl.
ATTACK_CONTAINER="${ATTACK_CONTAINER:-rangerdanger-kali}"
RELAY_URL="http://10.40.40.20:8080"
RECLOSER_URL="http://10.40.40.21:8080"
REGULATOR_URL="http://10.40.40.22:8080"
RTAC_URL="http://10.30.30.20:8080"

# Gateway addresses (containd firewall) per zone
GW_ENTERPRISE="10.10.10.2"
GW_VENDOR="10.20.20.2"
GW_OT_OPS="10.30.30.2"
GW_FIELD="10.40.40.2"

# Set containd as default gateway on a container so cross-zone traffic
# flows through the firewall for DPI inspection and policy enforcement.
setup_gateway() {
    local container=$1
    local gateway=$2
    info "Setting default gateway on ${container} → ${gateway}"
    # Try ip route first, fall back to route command
    docker exec "$container" sh -c "
        if command -v ip >/dev/null 2>&1; then
            ip route del default 2>/dev/null
            ip route add default via ${gateway}
        elif command -v route >/dev/null 2>&1; then
            route del default 2>/dev/null
            route add default gw ${gateway}
        else
            echo 'WARNING: no routing command available'
        fi
    " 2>/dev/null || true
}

step() {
    local num=$1
    shift
    echo ""
    echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${CYAN}  STEP ${num}: $*${NC}"
    echo -e "${BOLD}${CYAN}═══════════════════════════════════════════════════════${NC}"
    echo ""
}

info() {
    echo -e "${CYAN}[INFO]${NC} $*"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

ok() {
    echo -e "${GREEN}[OK]${NC} $*"
}

fail() {
    echo -e "${RED}[FAIL]${NC} $*"
}

attack() {
    echo -e "${RED}[ATTACK]${NC} $*"
}

# Send a command to a field device via direct access (attack path)
# Runs curl inside a docker container to reach the internal network.
send_direct_command() {
    local url=$1
    local payload=$2
    local label=${3:-""}

    if [ -n "$label" ]; then
        echo -e "  ${YELLOW}→${NC} $label"
    fi
    echo -e "  ${YELLOW}→${NC} POST ${url}/api/command (via ${ATTACK_CONTAINER})"

    local body
    body=$(docker exec "$ATTACK_CONTAINER" \
        curl -s -X POST "${url}/api/command" \
        -H "Content-Type: application/json" \
        -d "$payload" 2>/dev/null) || body='{"error":"container unreachable"}'

    if echo "$body" | grep -q '"result"' 2>/dev/null; then
        echo -e "  ${GREEN}←${NC} ${body}"
    else
        echo -e "  ${RED}←${NC} ${body}"
    fi

    echo "$body"
}

# Get substation state from RTAC via backend API
get_substation_state() {
    curl -s "${API_URL}/substation/state" 2>/dev/null || echo '{"error":"backend unreachable"}'
}

# Get substation tags from RTAC via backend API
get_substation_tags() {
    curl -s "${API_URL}/substation/tags" 2>/dev/null || echo '{"error":"backend unreachable"}'
}

# Check if breaker is closed
check_breaker() {
    local state
    state=$(get_substation_state)
    local closed
    closed=$(echo "$state" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('electrical',{}).get('breaker_closed', 'unknown'))" 2>/dev/null || echo "unknown")
    echo "$closed"
}

# Check if recloser is closed
check_recloser() {
    local state
    state=$(get_substation_state)
    echo "$state" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('electrical',{}).get('recloser_closed', 'unknown'))" 2>/dev/null || echo "unknown"
}

# Check critical load voltage
check_critical_voltage() {
    local state
    state=$(get_substation_state)
    echo "$state" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('electrical',{}).get('critical_load_voltage_v', 0))" 2>/dev/null || echo "0"
}

# Check if loads are energized
check_loads() {
    local state
    state=$(get_substation_state)
    echo "$state" | python3 -c "
import sys, json
d = json.load(sys.stdin)
e = d.get('electrical', {})
gen = e.get('general_load_energized', False)
crit = e.get('critical_load_energized', False)
print(f'general={gen} critical={crit}')
" 2>/dev/null || echo "error"
}

# Wait and poll for state change
wait_for() {
    local description=$1
    local check_cmd=$2
    local expected=$3
    local max_wait=${4:-10}

    echo -n "  Waiting for ${description}..."
    for i in $(seq 1 "$max_wait"); do
        local result
        result=$(eval "$check_cmd")
        if [ "$result" = "$expected" ]; then
            echo -e " ${GREEN}done${NC}"
            return 0
        fi
        sleep 1
        echo -n "."
    done
    echo -e " ${RED}timeout${NC} (got: $(eval "$check_cmd"))"
    return 1
}

pause() {
    if [ "${AUTO_RUN:-0}" = "1" ]; then
        sleep 2
    else
        echo ""
        echo -e "${BOLD}Press Enter to continue...${NC}"
        read -r
    fi
}

# Send a reset command to a device (uses docker exec to reach internal network)
_reset_cmd() {
    local url=$1
    local payload=$2
    docker exec rangerdanger-rtac-sim wget -q -O/dev/null --post-data="$payload" \
        --header="Content-Type: application/json" "${url}/api/command" 2>/dev/null || true
}

# Reset all devices to default state
reset_devices() {
    info "Resetting all devices to default state..."

    # Close breaker
    _reset_cmd "$RELAY_URL" '{"command":"clear_fault","source":"reset-script"}'
    _reset_cmd "$RELAY_URL" '{"command":"unlock","source":"reset-script"}'
    _reset_cmd "$RELAY_URL" '{"command":"close","source":"reset-script"}'

    # Close recloser and enable reclose
    _reset_cmd "$RECLOSER_URL" '{"command":"clear_fault","source":"reset-script"}'
    _reset_cmd "$RECLOSER_URL" '{"command":"reset_lockout","source":"reset-script"}'
    _reset_cmd "$RECLOSER_URL" '{"command":"enable_reclose","source":"reset-script"}'
    _reset_cmd "$RECLOSER_URL" '{"command":"close","source":"reset-script"}'

    # Reset regulator to tap 0 auto mode
    _reset_cmd "$REGULATOR_URL" '{"command":"set_auto","source":"reset-script"}'
    _reset_cmd "$REGULATOR_URL" '{"command":"set_tap","value":0,"source":"reset-script"}'

    sleep 2
    ok "Devices reset to normal operating state"
}
