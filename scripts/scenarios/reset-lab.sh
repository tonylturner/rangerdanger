#!/usr/bin/env bash
# Reset all field devices to default state
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

echo -e "${BOLD}Resetting lab to default state...${NC}"
reset_devices
echo ""

info "Current state:"
BREAKER=$(check_breaker)
RECLOSER=$(check_recloser)
VOLTAGE=$(check_critical_voltage)
LOADS=$(check_loads)

echo -e "  Breaker:   ${BREAKER}"
echo -e "  Recloser:  ${RECLOSER}"
echo -e "  Voltage:   ${VOLTAGE}V"
echo -e "  Loads:     ${LOADS}"
echo ""
ok "Lab reset complete"
