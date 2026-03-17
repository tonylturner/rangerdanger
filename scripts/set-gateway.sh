#!/bin/sh
# Set the containd firewall as default gateway for inter-zone routing.
# Called by containers that need to reach other zones.
# Usage: GATEWAY=10.10.10.2 /scripts/set-gateway.sh
#
# This replaces the Docker bridge default gateway with the containd
# firewall address, so all cross-zone traffic is inspected by DPI.

GATEWAY="${GATEWAY:-}"
if [ -z "$GATEWAY" ]; then
    echo "set-gateway: GATEWAY not set, skipping"
    exit 0
fi

# Get current default gateway
CURRENT_GW=$(ip route show default | awk '{print $3}' | head -1)

if [ "$CURRENT_GW" = "$GATEWAY" ]; then
    echo "set-gateway: already using $GATEWAY"
    exit 0
fi

# Replace default route
ip route del default 2>/dev/null || true
ip route add default via "$GATEWAY"
echo "set-gateway: default gateway set to $GATEWAY (was $CURRENT_GW)"
