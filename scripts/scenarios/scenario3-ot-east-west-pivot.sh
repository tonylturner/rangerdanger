#!/usr/bin/env bash
# ============================================================================
# Scenario 3: OT East-West Pivot (Lateral Movement)
# ============================================================================
#
# A compromised node in the OT operations zone (10.30.30.0/24) moves laterally
# to manipulate the voltage regulator (10.40.40.22) in the field device zone.
# By setting an extreme tap position, the attacker causes voltage quality
# issues at critical loads (hospitals, data centers, water treatment).
#
# Usage:
#   ./scenario3-ot-east-west-pivot.sh
#   AUTO_RUN=1 ./scenario3-ot-east-west-pivot.sh
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

echo -e "${BOLD}${RED}"
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║  SCENARIO 3: OT East-West Pivot                         ║"
echo "║                                                          ║"
echo "║  Attacker: Compromised OT node (10.30.30.x)              ║"
echo "║  Target:   Regulator-Sim (10.40.40.22) — Field Zone      ║"
echo "║  Impact:   Extreme tap → voltage out of range             ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# ── Step 1: Observe normal voltage ──────────────────────────────────────────

step 1 "Observe normal voltage on critical loads"

# Scenario uses an OT ops node (e.g., OpenPLC) as the compromised host.
# OpenPLC has curl available and is on ot_ops_net.
ATTACK_CONTAINER="rangerdanger-openplc"
setup_gateway rangerdanger-openplc "$GW_OT_OPS"

reset_devices

info "Checking critical load voltage..."
sleep 2
VOLTAGE=$(check_critical_voltage)
echo -e "  Critical load voltage: ${GREEN}${VOLTAGE}V${NC}"
echo ""
echo "  Normal range (ANSI C84.1 Range A): 114V – 126V"
echo "  Acceptable range (Range B):        108V – 132V"
echo "  Below 108V or above 132V:          Equipment damage risk"

if python3 -c "v=${VOLTAGE}; exit(0 if 114 <= v <= 126 else 1)" 2>/dev/null; then
    ok "Voltage is within normal Range A"
elif python3 -c "v=${VOLTAGE}; exit(0 if 108 <= v <= 132 else 1)" 2>/dev/null; then
    warn "Voltage is in Range B (acceptable but not ideal)"
else
    fail "Voltage is already outside acceptable range!"
fi

pause

# ── Step 2: Simulate OT compromise ─────────────────────────────────────────

step 2 "Simulate OT compromise — manipulate voltage regulator"

attack "Compromised OT operations node sending extreme tap command"
echo ""
echo -e "  ${YELLOW}Scenario:${NC} An attacker has compromised a node in the OT"
echo "  operations zone (e.g., through phishing, supply chain,"
echo "  or pivot from the vendor zone). They now have direct"
echo "  network access to field devices."
echo ""
echo -e "  ${YELLOW}Command (from compromised 10.30.30.x):${NC}"
echo '  curl -X POST http://10.40.40.22:8080/api/command \'
echo '    -d '\''{"command":"set_tap","value":-16,"source":"compromised-ot-node"}'\'''
echo ""

RESULT=$(send_direct_command "$REGULATOR_URL" \
    '{"command":"set_tap","value":-16,"source":"10.30.30.x-compromised"}' \
    "Set regulator tap to -16 (extreme low)")

if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack SUCCEEDED — regulator tap set to extreme value!"
else
    ok "Attack BLOCKED — only RTAC can control field devices"
    echo ""
    echo -e "${BOLD}Scenario complete — segmentation already in place.${NC}"
    exit 0
fi

pause

# ── Step 3: Observe voltage degradation ─────────────────────────────────────

step 3 "Observe voltage degradation at critical loads"

sleep 2
info "Checking critical load voltage after attack..."
VOLTAGE=$(check_critical_voltage)

echo -e "  Critical load voltage: ${RED}${VOLTAGE}V${NC}"
echo ""

if python3 -c "v=${VOLTAGE}; exit(0 if v < 108 else 1)" 2>/dev/null; then
    fail "IMPACT: Voltage BELOW 108V — outside ANSI C84.1 Range B!"
    echo ""
    echo -e "  ${RED}At this voltage level:${NC}"
    echo -e "  ${RED}  - Motors overheat and fail${NC}"
    echo -e "  ${RED}  - Electronic equipment malfunctions${NC}"
    echo -e "  ${RED}  - Hospital/critical facility power quality degraded${NC}"
    echo -e "  ${RED}  - Protective relays may trip additional equipment${NC}"
elif python3 -c "v=${VOLTAGE}; exit(0 if v < 114 else 1)" 2>/dev/null; then
    warn "Voltage below normal range (Range B) — service quality degraded"
else
    info "Voltage still in normal range — tap change may not have been extreme enough"
fi

# Also try setting to extreme high
echo ""
attack "Now trying extreme high tap..."
send_direct_command "$REGULATOR_URL" \
    '{"command":"set_tap","value":16,"source":"10.30.30.x-compromised"}' \
    "Set regulator tap to +16 (extreme high)"

sleep 2
VOLTAGE=$(check_critical_voltage)
echo -e "  Critical load voltage: ${RED}${VOLTAGE}V${NC}"

if python3 -c "v=${VOLTAGE}; exit(0 if v > 132 else 1)" 2>/dev/null; then
    fail "IMPACT: Voltage ABOVE 132V — equipment damage risk!"
fi

echo ""
echo -e "${BOLD}${YELLOW}QUESTION FOR STUDENTS:${NC}"
echo "  Which OT nodes should have direct access to field devices?"
echo "  Hint: Only the RTAC (supervisory controller) needs to send"
echo "  commands to field devices. Other OT nodes should access"
echo "  field data through the RTAC, not directly."

pause

# ── Step 4: Improve segmentation ────────────────────────────────────────────

step 4 "Improve segmentation — restrict OT→field access"

echo -e "${BOLD}The key rule:${NC}"
echo ""
echo "  ALLOW only RTAC (10.40.40.10) → field_net (lan2)"
echo "  DENY all other ot_ops_net (lan1) → field_net (lan2)"
echo ""
echo "  This means:"
echo "  - The RTAC can still poll and command field devices ✓"
echo "  - The HMI reads data through the RTAC, not directly ✓"
echo "  - A compromised OT node cannot reach field devices ✓"
echo ""
echo -e "${BOLD}Implementation:${NC}"
echo "  The improved config (substation-improved.json) includes"
echo "  a source restriction on the lan1→lan2 rule:"
echo "    source: 10.40.40.10/32 (RTAC's field_net address)"

pause

# ── Step 5: Re-test ─────────────────────────────────────────────────────────

step 5 "Re-test attack (after applying improved segmentation)"

reset_devices
sleep 2

attack "Repeating tap manipulation from compromised OT node..."
RESULT=$(send_direct_command "$REGULATOR_URL" \
    '{"command":"set_tap","value":-16,"source":"10.30.30.x-compromised"}' \
    "Set extreme tap (should be BLOCKED)")

if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack still succeeded — apply improved firewall rules"
else
    ok "Attack BLOCKED — only RTAC can command field devices"
fi

echo ""
info "Verifying RTAC can still control field devices..."
sleep 2
VOLTAGE=$(check_critical_voltage)
echo -e "  Critical load voltage: ${VOLTAGE}V"

LOADS=$(check_loads)
echo -e "  Loads: ${LOADS}"

echo ""
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${GREEN}  SCENARIO 3 COMPLETE${NC}"
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo "Key takeaway: East-west lateral movement within OT zones is"
echo "a critical threat. Not every OT node needs direct field device"
echo "access. Enforce least-privilege: only the RTAC/gateway should"
echo "communicate with field devices. All other OT nodes access"
echo "process data through the supervisory controller."
