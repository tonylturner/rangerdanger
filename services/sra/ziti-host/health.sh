#!/bin/bash
set -euo pipefail
phase=$(cat /var/lib/sra/phase)
if [[ "$phase" == enrolling ]]; then
    last=$(cat /var/lib/sra/attempt-time)
    [[ "$last" =~ ^[0-9]+$ ]] && (( $(date +%s) - last < 60 ))
else
    [[ "$phase" == running && -s /ziti-edge-tunnel/ot-host.json &&
       $(cat /proc/1/comm) == ziti-edge-tunne* ]]
fi
