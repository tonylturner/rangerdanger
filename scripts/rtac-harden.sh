#!/bin/sh
# rtac-harden.sh — prevent the RTAC from acting as a firewall bypass.
#
# The RTAC is intentionally multi-homed on multiple Docker networks
# (ot_ops_net, field_net, physics_net, mgmt_net) because real substation
# communication processors often have legs on multiple process networks.
# What we do NOT want is for the RTAC to act as a router or bridge between
# zones, or for its application traffic to field devices to bypass the
# firewall by using the directly-connected field_net interface.
#
# This script enforces the following constraints:
#   1. IP forwarding is disabled globally and per-interface
#   2. Proxy ARP and ICMP redirects are disabled
#   3. Strict reverse path filtering is enabled
#   4. netfilter FORWARD policy is DROP
#   5. The directly-connected route for the field subnet is REPLACED
#      with an indirect route via the OT Ops firewall, so that all
#      RTAC -> field traffic transits the containd firewall and is
#      visible in its capture pipeline.
#   6. Inbound L3 traffic on the field_net interface is dropped
#      (the interface still answers ARP for its IP, but nothing on
#      field_net can initiate a connection TO the RTAC directly).
#
# All of this runs before the rtac-sim binary starts so the Go service
# never sees the directly-connected field route.

set -e

log() { echo "rtac-harden: $*"; }

# ── 1. Sysctls ───────────────────────────────────────────────────
#
# Sysctls are set via docker-compose sysctls: at container creation
# time (required because /proc/sys is read-only inside the container).
# This block is defensive — if the container is run without the
# compose sysctls config (e.g. manual docker run), we try to set them
# anyway. Writes will silently fail on read-only /proc/sys; that's
# fine because the compose path is the canonical source of truth.

log "ensuring forwarding and redirect controls are disabled (sysctls)"

# Use subshells to fully suppress "can't create" errors from read-only /proc.
# In POSIX sh, redirection-failure messages are produced by the shell itself
# before the command runs, so a subshell is needed to cleanly capture them.
try_write() { ( echo "$1" > "$2" ) 2>/dev/null || true; }

try_write 0 /proc/sys/net/ipv4/ip_forward
for f in /proc/sys/net/ipv4/conf/*/forwarding; do try_write 0 "$f"; done
for f in /proc/sys/net/ipv4/conf/*/proxy_arp; do try_write 0 "$f"; done
for f in /proc/sys/net/ipv4/conf/*/send_redirects; do try_write 0 "$f"; done
for f in /proc/sys/net/ipv4/conf/*/accept_redirects; do try_write 0 "$f"; done
for f in /proc/sys/net/ipv4/conf/*/rp_filter; do try_write 1 "$f"; done

# Report the effective values so the harden log is self-documenting
log "  ip_forward=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo ?)"
log "  conf.all.forwarding=$(cat /proc/sys/net/ipv4/conf/all/forwarding 2>/dev/null || echo ?)"
log "  conf.all.rp_filter=$(cat /proc/sys/net/ipv4/conf/all/rp_filter 2>/dev/null || echo ?)"

# ── 2. netfilter FORWARD DROP ────────────────────────────────────

if command -v iptables >/dev/null 2>&1; then
  log "setting iptables FORWARD policy to DROP"
  iptables -P FORWARD DROP 2>/dev/null || log "iptables -P FORWARD DROP failed (non-fatal)"
  iptables -F FORWARD 2>/dev/null || true
else
  log "iptables not present — skipping FORWARD DROP (ip_forward=0 is the primary control)"
fi

# ── 3. Identify interfaces by their IPs ──────────────────────────

FIELD_IFACE=$(ip -o addr show 2>/dev/null | awk '/inet 10\.40\.40\./ {print $2; exit}')
OTOPS_IFACE=$(ip -o addr show 2>/dev/null | awk '/inet 10\.30\.30\./ {print $2; exit}')
FIELD_FW="10.40.40.2"
OTOPS_FW="10.30.30.2"
FIELD_CIDR="10.40.40.0/24"

if [ -z "$FIELD_IFACE" ]; then
  log "no field_net interface detected (container may not be the RTAC) — skipping route override"
  exit 0
fi

if [ -z "$OTOPS_IFACE" ]; then
  log "ERROR: field_net is attached but ot_ops_net is not — cannot route field traffic through firewall"
  exit 1
fi

log "field_net iface=$FIELD_IFACE, ot_ops_net iface=$OTOPS_IFACE"

# ── 4. Route override ────────────────────────────────────────────
#
# Replace the directly-connected field route with an indirect route
# via the OT Ops firewall. The field_net interface itself remains up
# and keeps its IP (10.40.40.10) for realism and ARP responsiveness.

log "removing directly-connected route $FIELD_CIDR dev $FIELD_IFACE"
ip route del "$FIELD_CIDR" dev "$FIELD_IFACE" 2>/dev/null || \
  log "  (route already absent)"

log "installing indirect route $FIELD_CIDR via $OTOPS_FW dev $OTOPS_IFACE"
ip route add "$FIELD_CIDR" via "$OTOPS_FW" dev "$OTOPS_IFACE" 2>/dev/null || {
  log "  failed to add indirect route — attempting replace"
  ip route replace "$FIELD_CIDR" via "$OTOPS_FW" dev "$OTOPS_IFACE"
}

# ── 5. Drop inbound L3 on field_net ──────────────────────────────
#
# The field_net interface still answers ARP for 10.40.40.10 at L2,
# but we reject any L3 packet arriving on it. Nothing on field_net
# should initiate a connection toward the RTAC.

if command -v iptables >/dev/null 2>&1; then
  log "dropping inbound L3 traffic on $FIELD_IFACE"
  iptables -A INPUT -i "$FIELD_IFACE" -j DROP 2>/dev/null || \
    log "  iptables INPUT DROP failed (non-fatal)"
fi

# ── 6. Verify default route is still in place ───────────────────
#
# Belt-and-suspenders: set-gateway.sh should have installed the
# default route before this script ran, but if anything cleaned it
# up we want to restore it. Without a default route, the RTAC cannot
# reply to cross-zone requests (e.g. from eng-ws in vendor_net), and
# TCP handshakes silently fail with no obvious symptom.

if ! ip route show default | grep -q .; then
  log "default route missing — restoring via $OTOPS_FW"
  ip route add default via "$OTOPS_FW" dev "$OTOPS_IFACE" 2>/dev/null || true
fi

# ── 7. Summary ───────────────────────────────────────────────────

log "harden complete — routing table:"
ip route 2>&1 | sed 's/^/  /'

log "ip_forward = $(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo unknown)"
