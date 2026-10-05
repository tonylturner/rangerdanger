#!/bin/sh
# Set the containd firewall as default gateway for inter-zone routing.
# Called by containers that need to reach other zones.
# Usage: GATEWAY=10.10.10.2 /scripts/set-gateway.sh
#
# This replaces any Docker-assigned default routes with the containd
# firewall address, so all cross-zone traffic is inspected by DPI.
#
# Containers may be attached to multiple Docker networks (zone network
# plus mgmt_net for out-of-band management access). Docker installs a
# default route for each, so we flush all defaults before installing
# the single zone-firewall default. Per-subnet routes (e.g. for
# mgmt_net's 10.99.99.0/24) are preserved, so management traffic to
# the proxy/backend still works while all other traffic egresses via
# the zone firewall.

GATEWAY="${GATEWAY:-}"
if [ -z "$GATEWAY" ]; then
    echo "set-gateway: GATEWAY not set, skipping"
    exit 0
fi

# BusyBox ip lacks 'route flush', so loop until no default remains.
while ip route del default 2>/dev/null; do :; done

if route_error=$(ip route add default via "$GATEWAY" 2>&1 >/dev/null); then
    echo "set-gateway: default gateway set to $GATEWAY"
else
    printf 'set-gateway: failed to add default via %s: %s\n' "$GATEWAY" "$route_error" >&2
    exit 1
fi
