#!/bin/bash
# Smoke test for opendss-sim Python service.
# Run from repo root after docker compose up.
set -euo pipefail

BASE="http://localhost:8088/api"
DIRECT="http://localhost:8080"  # if testing container directly
PASS=0
FAIL=0

green() { printf "\033[32m✓ %s\033[0m\n" "$1"; PASS=$((PASS+1)); }
red()   { printf "\033[31m✗ %s\033[0m\n" "$1"; FAIL=$((FAIL+1)); }
bold()  { printf "\033[1m%s\033[0m\n" "$1"; }

# Detect endpoint: try direct container first, then proxy
if docker compose exec opendss_sim curl -sf http://localhost:8080/api/health >/dev/null 2>&1; then
    SIM="docker compose exec opendss_sim curl -sf http://localhost:8080"
    bold "Testing opendss-sim directly inside container"
else
    echo "Cannot reach opendss-sim container directly, testing via proxy"
    SIM="curl -sf http://localhost:8088"
fi

# ─── Test 1: Health check ───────────────────────────────────────────
bold ""
bold "=== Test 1: Health Check ==="
HEALTH=$(docker compose exec opendss_sim curl -sf http://localhost:8080/api/health 2>&1) || true
if echo "$HEALTH" | grep -q '"ok"'; then
    green "Health check passed: $HEALTH"
else
    red "Health check failed: $HEALTH"
fi

# ─── Test 2: Normal state (breaker+recloser closed, tap 0) ─────────
bold ""
bold "=== Test 2: Normal Operating State ==="
PAYLOAD='{
  "relay": {"breaker_closed": true, "lockout": false, "fault_seen": false, "measured_current_a": 120.0, "measured_voltage_kv": 12.47, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "recloser": {"closed": true, "reclose_enabled": true, "fault_seen": false, "shot_count": 0, "lockout": false, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "regulator": {"tap_position": 0, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""}
}'

RESULT=$(docker compose exec opendss_sim curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" \
    http://localhost:8080/api/update-state 2>&1) || true

echo "Response: $RESULT"

if echo "$RESULT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
v = d.get('downstream_voltage_v', 0)
c = d.get('feeder_current_a', 0)
if v > 100 and c > 0:
    print(f'downstream={v}V, current={c}A')
    sys.exit(0)
else:
    print(f'UNEXPECTED: downstream={v}V, current={c}A')
    sys.exit(1)
" 2>&1; then
    green "Normal state: non-zero voltage and current"
else
    red "Normal state: got zero or missing values"
fi

# ─── Test 3: Breaker open → 0V downstream ──────────────────────────
bold ""
bold "=== Test 3: Breaker Open ==="
PAYLOAD_OPEN='{
  "relay": {"breaker_closed": false, "lockout": false, "fault_seen": false, "measured_current_a": 0, "measured_voltage_kv": 12.47, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "recloser": {"closed": true, "reclose_enabled": true, "fault_seen": false, "shot_count": 0, "lockout": false, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "regulator": {"tap_position": 0, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""}
}'

RESULT=$(docker compose exec opendss_sim curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD_OPEN" \
    http://localhost:8080/api/update-state 2>&1) || true

echo "Response: $RESULT"

if echo "$RESULT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
v = d.get('downstream_voltage_v', -1)
e = d.get('general_load_energized', True)
if v == 0 and not e:
    print(f'downstream={v}V, energized={e}')
    sys.exit(0)
else:
    print(f'UNEXPECTED: downstream={v}V, energized={e}')
    sys.exit(1)
" 2>&1; then
    green "Breaker open: downstream de-energized"
else
    red "Breaker open: expected 0V downstream"
fi

# ─── Test 4: Recloser open → 0V downstream ─────────────────────────
bold ""
bold "=== Test 4: Recloser Open ==="
PAYLOAD_RECL='{
  "relay": {"breaker_closed": true, "lockout": false, "fault_seen": false, "measured_current_a": 120.0, "measured_voltage_kv": 12.47, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "recloser": {"closed": false, "reclose_enabled": true, "fault_seen": false, "shot_count": 0, "lockout": false, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "regulator": {"tap_position": 0, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""}
}'

RESULT=$(docker compose exec opendss_sim curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD_RECL" \
    http://localhost:8080/api/update-state 2>&1) || true

echo "Response: $RESULT"

if echo "$RESULT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
v = d.get('downstream_voltage_v', -1)
if v == 0:
    print(f'downstream={v}V')
    sys.exit(0)
else:
    print(f'UNEXPECTED: downstream={v}V')
    sys.exit(1)
" 2>&1; then
    green "Recloser open: downstream de-energized"
else
    red "Recloser open: expected 0V downstream"
fi

# ─── Test 5: Tap +5 → voltage increase ─────────────────────────────
bold ""
bold "=== Test 5: Regulator Tap +5 ==="
PAYLOAD_TAP='{
  "relay": {"breaker_closed": true, "lockout": false, "fault_seen": false, "measured_current_a": 120.0, "measured_voltage_kv": 12.47, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "recloser": {"closed": true, "reclose_enabled": true, "fault_seen": false, "shot_count": 0, "lockout": false, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "regulator": {"tap_position": 5, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""}
}'

RESULT=$(docker compose exec opendss_sim curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD_TAP" \
    http://localhost:8080/api/update-state 2>&1) || true

echo "Response: $RESULT"

if echo "$RESULT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
cv = d.get('critical_load_voltage_v', 0)
dv = d.get('downstream_voltage_v', 0)
if cv > dv and cv > 0:
    print(f'critical={cv}V > downstream={dv}V (tap boost working)')
    sys.exit(0)
else:
    print(f'UNEXPECTED: critical={cv}V, downstream={dv}V')
    sys.exit(1)
" 2>&1; then
    green "Tap +5: critical load voltage boosted"
else
    red "Tap +5: expected voltage boost"
fi

# ─── Test 6: Fault injection ───────────────────────────────────────
bold ""
bold "=== Test 6: Fault Injection ==="
PAYLOAD_FAULT='{
  "relay": {"breaker_closed": true, "lockout": false, "fault_seen": false, "measured_current_a": 120.0, "measured_voltage_kv": 12.47, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "recloser": {"closed": true, "reclose_enabled": true, "fault_seen": true, "shot_count": 1, "lockout": false, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""},
  "regulator": {"tap_position": 0, "comms_ok": true, "remote_control_enabled": true, "last_command_source": ""}
}'

RESULT=$(docker compose exec opendss_sim curl -sf -X POST \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD_FAULT" \
    http://localhost:8080/api/update-state 2>&1) || true

echo "Response: $RESULT"

if echo "$RESULT" | python3 -c "
import sys, json
d = json.load(sys.stdin)
fc = d.get('fault_current_a', 0)
if fc > 0:
    print(f'fault_current={fc}A')
    sys.exit(0)
else:
    print(f'UNEXPECTED: fault_current={fc}A')
    sys.exit(1)
" 2>&1; then
    green "Fault injection: non-zero fault current"
else
    red "Fault injection: expected non-zero fault current"
fi

# ─── Test 7: GET /api/electrical returns cached state ───────────────
bold ""
bold "=== Test 7: GET /api/electrical (cached state) ==="
ELEC=$(docker compose exec opendss_sim curl -sf http://localhost:8080/api/electrical 2>&1) || true
echo "Response: $ELEC"

if echo "$ELEC" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if 'downstream_voltage_v' in d and 'feeder_current_a' in d:
    sys.exit(0)
else:
    sys.exit(1)
" 2>&1; then
    green "GET /api/electrical returns valid cached state"
else
    red "GET /api/electrical missing expected fields"
fi

# ─── Test 8: RTAC aggregated state (end-to-end) ────────────────────
bold ""
bold "=== Test 8: End-to-End via RTAC ==="
E2E=$(curl -sf http://localhost:8088/api/substation/state 2>&1) || true
echo "Response (truncated): $(echo "$E2E" | head -c 500)"

if echo "$E2E" | python3 -c "
import sys, json
d = json.load(sys.stdin)
elec = d.get('electrical', {})
v = elec.get('downstream_voltage_v', 0)
if v > 0:
    print(f'RTAC electrical downstream={v}V')
    sys.exit(0)
else:
    print(f'RTAC electrical downstream={v}V (zero or missing)')
    sys.exit(1)
" 2>&1; then
    green "End-to-end: RTAC returns non-zero electrical values"
else
    red "End-to-end: RTAC electrical values are zero or missing"
fi

# ─── Summary ────────────────────────────────────────────────────────
bold ""
bold "==============================="
bold "  Results: $PASS passed, $FAIL failed"
bold "==============================="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
