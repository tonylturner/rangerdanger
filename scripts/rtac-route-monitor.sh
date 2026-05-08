#!/bin/sh
# rtac-route-monitor.sh — keep RTAC's hardening routes installed.
#
# rtac-harden.sh installs the canonical RTAC routing at boot:
#   1. default via OT Ops firewall (so RTAC can answer cross-zone
#      requests from wan / dmz over the firewall)
#   2. 10.40.40.0/24 indirect via OT Ops firewall (so RTAC -> field
#      traffic transits the containd firewall instead of bypassing
#      it via the directly-attached field_net interface — this is
#      for visibility, not access control; access control is the
#      firewall policy's job)
#
# Both are vulnerable to docker network reconciliation: when a peer
# container is recreated or networks are torn down/up, docker can
# flush container routes. Without the default route in particular,
# RTAC silently can't reply to wan or dmz, so cross-zone probes time
# out with no obvious symptom.
#
# This monitor runs in background after harden and re-installs either
# route as soon as it's missing. Idempotent and quiet on the happy
# path; logs only when it had to restore something.

set -u

OTOPS_FW="${OTOPS_FW:-10.30.30.2}"
FIELD_CIDR="${FIELD_CIDR:-10.40.40.0/24}"
OTOPS_IFACE="${OTOPS_IFACE:-}"
POLL_SECS="${ROUTE_MONITOR_POLL_SECS:-5}"

log() { echo "rtac-route-monitor: $*"; }

if [ -z "$OTOPS_IFACE" ]; then
  OTOPS_IFACE=$(ip -o addr show 2>/dev/null \
                | awk '/inet 10\.30\.30\./ {print $2; exit}')
fi
if [ -z "$OTOPS_IFACE" ]; then
  log "no OT Ops interface found — exiting (not the RTAC?)"
  exit 0
fi

ensure_routes() {
  if ! ip route show default 2>/dev/null | grep -q .; then
    if ip route add default via "$OTOPS_FW" dev "$OTOPS_IFACE" 2>/dev/null; then
      log "restored default route via $OTOPS_FW dev $OTOPS_IFACE"
    fi
  fi
  if ! ip route show "$FIELD_CIDR" 2>/dev/null | grep -q "via $OTOPS_FW"; then
    # Either the indirect route was wiped, or docker re-installed the
    # directly-connected route. Replace forces the indirect one.
    if ip route replace "$FIELD_CIDR" via "$OTOPS_FW" dev "$OTOPS_IFACE" 2>/dev/null; then
      log "restored indirect $FIELD_CIDR via $OTOPS_FW dev $OTOPS_IFACE"
    fi
  fi
}

# Reapply once on startup to close any race between harden finishing
# and the first reconciliation event.
ensure_routes

log "polling routes every ${POLL_SECS}s (iface=$OTOPS_IFACE, gw=$OTOPS_FW)"
while :; do
  sleep "$POLL_SECS"
  ensure_routes
done
