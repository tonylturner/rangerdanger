#!/bin/bash
# Seed FUXA HMI with RTAC Modbus device configuration and substation views.
# Run this after the lab is up: ./scripts/seed-fuxa.sh
#
# FUXA REST API: http://10.30.30.10:1881/api
# Default credentials: admin / admin (if auth enabled)

set -e

FUXA_URL="${FUXA_URL:-http://localhost:8088/fuxa}"
FUXA_API="$FUXA_URL/api"

echo "=== FUXA HMI Seed Script ==="
echo "Target: $FUXA_API"

# Wait for FUXA to be ready
echo "Waiting for FUXA..."
for i in $(seq 1 30); do
    if curl -s "$FUXA_API/project" >/dev/null 2>&1; then
        echo "FUXA is ready"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "FUXA not reachable at $FUXA_API after 30 attempts"
        exit 1
    fi
    sleep 2
done

# ──────────────────────────────────────────────────────────────
# 1. Configure RTAC Modbus TCP Device
# ──────────────────────────────────────────────────────────────
echo ""
echo "Configuring RTAC Modbus device..."

cat <<'DEVICE_JSON' | curl -s -X POST "$FUXA_API/device" \
  -H "Content-Type: application/json" \
  -d @- >/dev/null
{
  "id": "d_rtac",
  "name": "RTAC Supervisory Controller",
  "type": "ModbusClient",
  "enabled": true,
  "polling": 1000,
  "property": {
    "address": "10.30.30.20",
    "port": 502,
    "slaveid": 1,
    "type": "TCP"
  },
  "tags": {
    "breaker_closed": {
      "id": "t_breaker_closed",
      "name": "breaker_closed",
      "type": "Bool",
      "address": "000001",
      "memaddress": "0",
      "divisor": 1,
      "options": {}
    },
    "recloser_closed": {
      "id": "t_recloser_closed",
      "name": "recloser_closed",
      "type": "Bool",
      "address": "000002",
      "memaddress": "1",
      "divisor": 1,
      "options": {}
    },
    "general_load_on": {
      "id": "t_general_load_on",
      "name": "general_load_on",
      "type": "Bool",
      "address": "000003",
      "memaddress": "2",
      "divisor": 1,
      "options": {}
    },
    "critical_load_on": {
      "id": "t_critical_load_on",
      "name": "critical_load_on",
      "type": "Bool",
      "address": "000004",
      "memaddress": "3",
      "divisor": 1,
      "options": {}
    },
    "relay_comms_ok": {
      "id": "t_relay_comms",
      "name": "relay_comms_ok",
      "type": "Bool",
      "address": "000009",
      "memaddress": "8",
      "divisor": 1,
      "options": {}
    },
    "recloser_comms_ok": {
      "id": "t_recloser_comms",
      "name": "recloser_comms_ok",
      "type": "Bool",
      "address": "000010",
      "memaddress": "9",
      "divisor": 1,
      "options": {}
    },
    "regulator_comms_ok": {
      "id": "t_regulator_comms",
      "name": "regulator_comms_ok",
      "type": "Bool",
      "address": "000011",
      "memaddress": "10",
      "divisor": 1,
      "options": {}
    },
    "alarm_comm_loss": {
      "id": "t_alarm_comm_loss",
      "name": "alarm_comm_loss",
      "type": "Bool",
      "address": "100001",
      "memaddress": "0",
      "divisor": 1,
      "options": {}
    },
    "alarm_breaker_open": {
      "id": "t_alarm_breaker_open",
      "name": "alarm_breaker_open",
      "type": "Bool",
      "address": "100002",
      "memaddress": "1",
      "divisor": 1,
      "options": {}
    },
    "alarm_reclose_disabled": {
      "id": "t_alarm_reclose_disabled",
      "name": "alarm_reclose_disabled",
      "type": "Bool",
      "address": "100003",
      "memaddress": "2",
      "divisor": 1,
      "options": {}
    },
    "alarm_low_voltage": {
      "id": "t_alarm_low_voltage",
      "name": "alarm_low_voltage",
      "type": "Bool",
      "address": "100004",
      "memaddress": "3",
      "divisor": 1,
      "options": {}
    },
    "downstream_voltage": {
      "id": "t_downstream_v",
      "name": "downstream_voltage",
      "type": "Int16",
      "address": "300001",
      "memaddress": "0",
      "divisor": 10,
      "options": { "min": 0, "max": 150 }
    },
    "critical_voltage": {
      "id": "t_critical_v",
      "name": "critical_voltage",
      "type": "Int16",
      "address": "300002",
      "memaddress": "1",
      "divisor": 10,
      "options": { "min": 0, "max": 150 }
    },
    "feeder_current": {
      "id": "t_feeder_current",
      "name": "feeder_current",
      "type": "Int16",
      "address": "300003",
      "memaddress": "2",
      "divisor": 10,
      "options": { "min": 0, "max": 100 }
    },
    "general_load_kw": {
      "id": "t_gen_kw",
      "name": "general_load_kw",
      "type": "Int16",
      "address": "300004",
      "memaddress": "3",
      "divisor": 1,
      "options": { "min": 0, "max": 1000 }
    },
    "critical_load_kw": {
      "id": "t_crit_kw",
      "name": "critical_load_kw",
      "type": "Int16",
      "address": "300005",
      "memaddress": "4",
      "divisor": 1,
      "options": { "min": 0, "max": 500 }
    },
    "regulator_tap": {
      "id": "t_reg_tap",
      "name": "regulator_tap",
      "type": "Int16",
      "address": "400001",
      "memaddress": "0",
      "divisor": 1,
      "options": { "min": -16, "max": 16 }
    }
  }
}
DEVICE_JSON

echo "RTAC Modbus device configured"

# ──────────────────────────────────────────────────────────────
# 2. Configure Alarms
# ──────────────────────────────────────────────────────────────
echo ""
echo "Configuring alarms..."

for alarm_json in \
  '{"id":"a_comm_loss","name":"Communication Loss","property":{"variableId":"d_rtac:t_alarm_comm_loss","type":"HIGH","highhigh":1,"text":"Device communication lost — field device unreachable","group":"Protection","priority":"high","ackmode":"float"}}' \
  '{"id":"a_breaker_open","name":"Unexpected Breaker Open","property":{"variableId":"d_rtac:t_alarm_breaker_open","type":"HIGH","highhigh":1,"text":"Feeder breaker opened — all downstream customers without power","group":"Protection","priority":"urgent","ackmode":"float"}}' \
  '{"id":"a_reclose_off","name":"Auto-Reclose Disabled","property":{"variableId":"d_rtac:t_alarm_reclose_disabled","type":"HIGH","highhigh":1,"text":"Auto-reclose disabled — next fault will cause sustained outage","group":"Protection","priority":"high","ackmode":"float"}}' \
  '{"id":"a_low_voltage","name":"Low Voltage - Critical Load","property":{"variableId":"d_rtac:t_alarm_low_voltage","type":"HIGH","highhigh":1,"text":"Critical load voltage below ANSI C84.1 Range A — hospital/fire station affected","group":"Power Quality","priority":"high","ackmode":"float"}}' \
; do
  echo "$alarm_json" | curl -s -X POST "$FUXA_API/alarm" \
    -H "Content-Type: application/json" \
    -d @- >/dev/null
done

echo "Alarms configured"

# ──────────────────────────────────────────────────────────────
# Done
# ──────────────────────────────────────────────────────────────
echo ""
echo "=== FUXA seed complete ==="
echo ""
echo "Next steps:"
echo "  1. Open FUXA editor at $FUXA_URL"
echo "  2. Verify RTAC device shows 'Connected' in Devices panel"
echo "  3. Use the built-in editor to create one-line diagram views:"
echo "     - Add shapes for breaker, recloser, regulator, loads"
echo "     - Bind tag values to shape properties (color, text)"
echo "     - Add alarm display widget"
echo ""
echo "RTAC Modbus register map:"
echo "  Coils (FC1):     breaker_closed(0), recloser_closed(1), loads(2-3), comms(8-10)"
echo "  Disc Inputs (FC2): alarms — comm_loss(0), breaker_open(1), reclose_off(2), low_v(3)"
echo "  Input Regs (FC4):  voltage(0-1), current(2), power(3-4) — all scaled x10"
echo "  Hold Regs (FC3):   regulator_tap(0)"
