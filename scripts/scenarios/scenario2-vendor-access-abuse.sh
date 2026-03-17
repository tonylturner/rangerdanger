#!/usr/bin/env bash
# ============================================================================
# Scenario 2: Vendor Access Abuse
# ============================================================================
#
# A compromised vendor remote-access path is used to disable auto-reclose
# on the feeder recloser. When a fault occurs, the recloser trips but cannot
# automatically restore service — turning a momentary fault into a sustained
# outage affecting all downstream loads.
#
# Usage:
#   ./scenario2-vendor-access-abuse.sh
#   AUTO_RUN=1 ./scenario2-vendor-access-abuse.sh
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

echo -e "${BOLD}${RED}"
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║  SCENARIO 2: Vendor Access Abuse                         ║"
echo "║                                                          ║"
echo "║  Attacker: Vendor Jump (10.20.20.10) — Vendor Zone       ║"
echo "║  Target:   Recloser-Sim (10.40.40.21) — Field Zone       ║"
echo "║  Impact:   Disabled auto-reclose → sustained outage      ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# ── Step 1: Observe normal fault recovery ───────────────────────────────────

step 1 "Observe normal fault recovery (auto-reclose working)"

# Set containd as gateway on vendor-jump so cross-zone traffic goes through firewall
ATTACK_CONTAINER="rangerdanger-vendor-jump"
setup_gateway rangerdanger-vendor-jump "$GW_VENDOR"

reset_devices

info "Verifying recloser is closed with auto-reclose enabled..."
RECLOSER=$(check_recloser)
echo -e "  Recloser closed: ${GREEN}${RECLOSER}${NC}"

info "Injecting a temporary fault on the recloser..."
send_direct_command "$RECLOSER_URL" \
    '{"command":"inject_fault","source":"scenario-script"}' \
    "Inject temporary fault"

sleep 2
info "Recloser should trip on fault..."
RECLOSER=$(check_recloser)
echo -e "  Recloser closed: ${RECLOSER}"

info "Clearing fault (simulating temporary fault clearing)..."
send_direct_command "$RECLOSER_URL" \
    '{"command":"clear_fault","source":"scenario-script"}' \
    "Clear fault"

sleep 3
info "With auto-reclose enabled, recloser should automatically restore..."
RECLOSER=$(check_recloser)
LOADS=$(check_loads)
echo -e "  Recloser closed: ${GREEN}${RECLOSER}${NC}"
echo -e "  Loads: ${GREEN}${LOADS}${NC}"

if [ "$RECLOSER" = "True" ]; then
    ok "Auto-reclose WORKED — service restored automatically after fault"
else
    warn "Recloser still open — may need lockout reset"
    send_direct_command "$RECLOSER_URL" \
        '{"command":"reset_lockout","source":"scenario-script"}' \
        "Reset lockout"
    send_direct_command "$RECLOSER_URL" \
        '{"command":"close","source":"scenario-script"}' \
        "Close recloser"
fi

pause

# ── Step 2: Attack from vendor zone ─────────────────────────────────────────

step 2 "Attack from vendor zone — disable auto-reclose"

attack "Compromised vendor session disabling auto-reclose on recloser"
echo ""
echo -e "  ${YELLOW}Command (from 10.20.20.10):${NC}"
echo '  curl -X POST http://10.40.40.21:8080/api/command \'
echo '    -d '\''{"command":"disable_reclose","source":"10.20.20.10-vendor-jump"}'\'''
echo ""

# Reset to clean state first
reset_devices
sleep 1

RESULT=$(send_direct_command "$RECLOSER_URL" \
    '{"command":"disable_reclose","source":"10.20.20.10-vendor-jump"}' \
    "Disable auto-reclose")

if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack SUCCEEDED — auto-reclose disabled from vendor zone!"
    echo ""
    echo -e "  ${RED}A vendor should not have direct control access to field devices.${NC}"
else
    ok "Attack BLOCKED — vendor cannot reach field devices"
    echo ""
    echo -e "${BOLD}Scenario complete — segmentation already in place.${NC}"
    exit 0
fi

pause

# ── Step 3: Inject fault after disabling reclose ────────────────────────────

step 3 "Inject fault — observe sustained outage"

attack "Injecting fault with auto-reclose disabled..."
send_direct_command "$RECLOSER_URL" \
    '{"command":"inject_fault","source":"10.20.20.10-vendor-jump"}' \
    "Inject fault on feeder"

sleep 2

info "Recloser trips on fault..."
RECLOSER=$(check_recloser)
echo -e "  Recloser closed: ${RED}${RECLOSER}${NC}"

info "Clearing fault..."
send_direct_command "$RECLOSER_URL" \
    '{"command":"clear_fault","source":"scenario-script"}' \
    "Clear fault (simulating transient)"

sleep 4

info "Checking if recloser restored (it should NOT with reclose disabled)..."
RECLOSER=$(check_recloser)
LOADS=$(check_loads)

if [ "$RECLOSER" = "False" ]; then
    fail "IMPACT: Recloser LOCKED OUT — sustained outage!"
    fail "Auto-reclose was disabled, so transient fault → permanent outage"
    echo -e "  Loads: ${RED}${LOADS}${NC}"
    echo ""
    echo -e "  ${RED}A momentary fault that would normally self-clear in seconds${NC}"
    echo -e "  ${RED}has become a sustained outage requiring manual intervention.${NC}"
else
    warn "Recloser somehow restored — may need to retry"
fi

echo ""
echo -e "${BOLD}${YELLOW}QUESTION FOR STUDENTS:${NC}"
echo "  Why is vendor→field device access dangerous?"
echo "  What's the minimum access a vendor needs for remote support?"
echo "  Hint: Vendors typically need SSH/HTTPS to OT ops servers only"

pause

# ── Step 4: Improve segmentation ────────────────────────────────────────────

step 4 "Improve segmentation"

echo -e "${BOLD}Rules to add:${NC}"
echo ""
echo "  1. DENY vendor_net (dmz) → field_net (lan2)"
echo "     Vendors should never directly reach field devices"
echo ""
echo "  2. ALLOW vendor_net → ot_ops_net (SSH, HTTPS only)"
echo "     Vendors can access EWS/HMI for maintenance via approved protocols"
echo ""
echo -e "${BOLD}Apply via Containd:${NC}"
echo "  - Import substation-improved.json, or"
echo "  - Add rules manually in the Containd UI at http://localhost:9080"

pause

# ── Step 5: Re-test ─────────────────────────────────────────────────────────

step 5 "Re-test attack (after applying improved segmentation)"

reset_devices
sleep 2

attack "Repeating disable_reclose from vendor zone..."
RESULT=$(send_direct_command "$RECLOSER_URL" \
    '{"command":"disable_reclose","source":"10.20.20.10-vendor-jump"}' \
    "Disable auto-reclose (should be BLOCKED)")

if echo "$RESULT" | grep -q '"result"' 2>/dev/null && ! echo "$RESULT" | grep -q '"error"' 2>/dev/null; then
    fail "Attack still succeeded — apply firewall rules and retry"
else
    ok "Attack BLOCKED — vendor cannot reach field devices"
fi

echo ""
info "Testing that fault recovery still works..."
send_direct_command "$RECLOSER_URL" \
    '{"command":"inject_fault","source":"scenario-script"}' \
    "Inject fault"

sleep 2
send_direct_command "$RECLOSER_URL" \
    '{"command":"clear_fault","source":"scenario-script"}' \
    "Clear fault"

sleep 3
RECLOSER=$(check_recloser)
if [ "$RECLOSER" = "True" ]; then
    ok "Auto-reclose working — fault recovery successful"
else
    warn "Recloser may need manual reset (lockout from repeated faults)"
fi

echo ""
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${GREEN}  SCENARIO 2 COMPLETE${NC}"
echo -e "${BOLD}${GREEN}═══════════════════════════════════════════════════════${NC}"
echo ""
echo "Key takeaway: Vendor/contractor access must be tightly scoped."
echo "Direct access from vendor zones to field devices enables attacks"
echo "that convert transient faults into sustained outages."
echo "Restrict vendors to approved services via SSH/HTTPS only."
