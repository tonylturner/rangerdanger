#!/bin/bash
set -euo pipefail
if [[ $(id -u) == 0 ]]; then
    /usr/local/bin/set-gateway.sh
    exec setpriv --reuid=65534 --regid=2171 --clear-groups "$0"
fi
# Enrollment may need firewall policy installed only after up --wait completes.
identity=/ziti-edge-tunnel/ot-host.json
if [[ ! -s "$identity" ]]; then
    echo enrolling > /var/lib/sra/phase
    delay=1
    while [[ ! -s "$identity" ]]; do
        date +%s > /var/lib/sra/attempt-time
        if [[ -s /run/sra/enrollment/ot-host.jwt ]]; then
            # Bound each operation: an unreachable peer must not stale local health.
            timeout 20 ziti-edge-tunnel enroll --jwt /run/sra/enrollment/ot-host.jwt --identity "$identity" && break
        fi
        sleep "$delay"
        (( delay < 30 )) && delay=$((delay * 2))
        (( delay > 30 )) && delay=30
    done
fi
echo running > /var/lib/sra/phase
exec ziti-edge-tunnel run-host --identity "$identity"
