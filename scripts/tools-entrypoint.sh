#!/bin/bash
# Entrypoint script for lab tools containers
# Sets up routes through the firewall based on which network segment we're on

set -e

# Detect which network we're on based on our IP
MY_IP=$(hostname -I | awk '{print $1}')
SUBNET=$(echo "$MY_IP" | cut -d. -f1-3)

case "$SUBNET" in
  "10.10.10")
    # IT network - route to DMZ, OT Control, OT Safety via firewall
    GATEWAY="10.10.10.2"
    ip route add 10.20.20.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.30.30.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.40.40.0/24 via $GATEWAY 2>/dev/null || true
    echo "[entrypoint] Configured routes for IT network via $GATEWAY"
    ;;
  "10.20.20")
    # DMZ network - route to IT, OT Control, OT Safety via firewall
    GATEWAY="10.20.20.2"
    ip route add 10.10.10.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.30.30.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.40.40.0/24 via $GATEWAY 2>/dev/null || true
    echo "[entrypoint] Configured routes for DMZ network via $GATEWAY"
    ;;
  "10.30.30")
    # OT Control network - route to IT, DMZ, OT Safety via firewall
    GATEWAY="10.30.30.2"
    ip route add 10.10.10.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.20.20.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.40.40.0/24 via $GATEWAY 2>/dev/null || true
    echo "[entrypoint] Configured routes for OT Control network via $GATEWAY"
    ;;
  "10.40.40")
    # OT Safety network - route to IT, DMZ, OT Control via firewall
    GATEWAY="10.40.40.2"
    ip route add 10.10.10.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.20.20.0/24 via $GATEWAY 2>/dev/null || true
    ip route add 10.30.30.0/24 via $GATEWAY 2>/dev/null || true
    echo "[entrypoint] Configured routes for OT Safety network via $GATEWAY"
    ;;
  *)
    echo "[entrypoint] Unknown network subnet: $SUBNET - no routes configured"
    ;;
esac

# Execute the main command
exec "$@"
