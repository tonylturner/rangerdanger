#!/usr/bin/env bash
# ============================================================================
# Scenario 1: Enterprise to Breaker Attack
# ============================================================================
#
# An attacker on the enterprise network (10.10.10.0/24) sends a trip command
# directly to the feeder breaker relay (10.40.40.20) in the field device zone.
#
# With the weak baseline firewall config, this succeeds — the breaker opens
# and all downstream loads lose power.
#
# Students must use Containd to block enterprise→field traffic, then retest.
#
# Usage:
#   ./scenario1-enterprise-to-breaker.sh          # Interactive (pauses between steps)
#   AUTO_RUN=1 ./scenario1-enterprise-to-breaker.sh  # Fast run (no pauses)
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

echo -e "${BOLD}${RED}"
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║  SCENARIO 1: Enterprise to Breaker Attack                ║"
echo "║                                                          ║"
echo "║  Attacker: Kali (10.10.10.50) — Enterprise Zone          ║"
echo "║  Target:   Relay-Sim (10.40.40.20) — Field Device Zone   ║"
echo "║  Impact:   Feeder breaker trip → total load loss          ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# ── Step 1: Observe baseline ────────────────────────────────────────────────

step 1 "Observe baseline — verify feeder is energized"

# Set containd as gateway on Kali so cross-zone traffic goes through firewall
setup_gateway rangerdanger-kali "$GW_ENTERPRISE"

reset_devices

info "Checking feeder state..."
BREAKER=$(check_breaker)
LOADS=$(check_loads)
echo -e "  Breaker closed: ${GREEN}${BREAKER}${NC}"
echo -e "  Loads: ${GREEN}${LOADS}${NC}"

if [ "$BREAKER" = "True" ]; then
    ok "Feeder is energized, all loads served"
else
    warn "Breaker is not closed — resetting..."
    reset_devices
fi

pause

# ── Step 2: Launch attack from enterprise zone ──────────────────────────────

step 2 "Launch attack from enterprise zone"

attack "Sending trip command from Kali (10.10.10.50) to relay (10.40.40.20)"
echo ""
echo -e "  ${YELLOW}Command:${NC}"
echo '  curl -X POST http://10.40.40.20:8080/api/command \'
echo '    -d '\''{"command":"trip","source":"10.10.10.50-kali"}'\'''
echo ""

RESULT=$(send_direct_command "$RELAY_URL" \
    '{"command":"trip","source":"10.10.10.50-kali"}' \
    "Trip feeder breaker")

# Check if the command succeeded
if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack SUCCEEDED — breaker tripped from enterprise zone!"
    echo ""
    echo -e "  ${RED}This should not be possible with proper segmentation.${NC}"
    echo -e "  ${RED}Enterprise zone should have NO direct access to field devices.${NC}"
else
    ok "Attack BLOCKED — firewall prevented enterprise→field access"
    echo ""
    echo -e "  ${GREEN}Segmentation is working correctly.${NC}"
    echo ""
    echo -e "${BOLD}Scenario complete — segmentation already in place.${NC}"
    exit 0
fi

pause

# ── Step 3: Observe operational consequence ─────────────────────────────────

step 3 "Observe operational consequence"

sleep 2  # Let RTAC poll pick up the change

info "Checking feeder state after attack..."
BREAKER=$(check_breaker)
LOADS=$(check_loads)
echo -e "  Breaker closed: ${RED}${BREAKER}${NC}"
echo -e "  Loads: ${RED}${LOADS}${NC}"

if [ "$BREAKER" = "False" ]; then
    fail "IMPACT: Breaker is OPEN — all downstream loads de-energized"
    fail "General load: 0 kW (was 500 kW)"
    fail "Critical load: 0 kW (was 200 kW)"
else
    warn "Breaker still shows closed — RTAC may not have polled yet"
fi

echo ""
echo -e "${BOLD}${YELLOW}QUESTION FOR STUDENTS:${NC}"
echo "  What firewall rule would prevent this attack?"
echo "  Hint: Block enterprise_net (wan) → field_net (lan2) traffic"

pause

# ── Step 4: Improve segmentation ────────────────────────────────────────────

step 4 "Improve segmentation"

echo -e "${BOLD}OPTION A:${NC} Import the improved config via Containd CLI"
echo ""
echo "  ssh -p 2222 containd@localhost"
echo "  # Then from the Containd shell:"
echo "  # import-config /path/to/substation-improved.json"
echo ""
echo -e "${BOLD}OPTION B:${NC} Import via API"
echo ""
echo "  curl -X POST http://localhost:9080/api/v1/config/import \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d @lab-definitions/firewall/substation-improved.json"
echo ""
echo -e "${BOLD}OPTION C:${NC} Add rules manually in Containd UI"
echo ""
echo "  1. Open http://localhost:9080"
echo "  2. Navigate to Firewall → Rules"
echo "  3. Add rule: DENY wan → lan2 (all traffic)"
echo "  4. This blocks enterprise zone from reaching field devices"

pause

# ── Step 5: Re-test attack ──────────────────────────────────────────────────

step 5 "Re-test attack (after applying improved segmentation)"

info "Resetting devices first..."
reset_devices
sleep 2

attack "Repeating trip command from Kali to relay..."
echo ""

RESULT=$(send_direct_command "$RELAY_URL" \
    '{"command":"trip","source":"10.10.10.50-kali"}' \
    "Trip feeder breaker (should be BLOCKED)")

if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack still succeeded — segmentation not yet applied"
    echo ""
    echo "  Apply the improved firewall config and try again."
else
    ok "Attack BLOCKED — segmentation is working!"
fi

pause

# ── Step 6: Validate operations ─────────────────────────────────────────────

step 6 "Validate legitimate operations still work"

info "Checking that RTAC can still read field device state..."
TAGS=$(get_substation_tags)
COMMS_OK=$(echo "$TAGS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
tags = d.get('tags', {})
relay_ok = tags.get('comms.relay.ok', False)
recloser_ok = tags.get('comms.recloser.ok', False)
regulator_ok = tags.get('comms.regulator.ok', False)
print(f'relay={relay_ok} recloser={recloser_ok} regulator={regulator_ok}')
" 2>/dev/null || echo "error")

echo -e "  Device comms: ${COMMS_OK}"

BREAKER=$(check_breaker)
LOADS=$(check_loads)
echo -e "  Breaker closed: ${BREAKER}"
echo -e "  Loads: ${LOADS}"

echo ""
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${GREEN}  SCENARIO 1 COMPLETE${NC}"
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo "Key takeaway: Enterprise networks should NEVER have direct"
echo "access to field device zones. Zone-based firewall rules at"
echo "the IT/OT boundary are critical for substation security."
