#!/bin/bash
set -euo pipefail
if [[ $(id -u) == 0 ]]; then
    /usr/local/bin/set-gateway.sh
    exec setpriv --reuid=2171 --regid=2171 --init-groups "$0"
fi
source /opt/sra/common/state.sh
sra_check
export ZITI_HOME=/ziti-router ZITI_ROUTER_NAME=sra-router ZITI_CTRL_ADVERTISED_ADDRESS=${SRA_CONTROLLER_ADDRESS:-10.25.25.40} ZITI_CTRL_ADVERTISED_PORT=1280 ZITI_ROUTER_PORT=3022
export ZITI_ROUTER_ADVERTISED_ADDRESS=${SRA_ROUTER_ADDRESS:-10.25.25.50}
export ZITI_ROUTER_IP_OVERRIDE=$ZITI_ROUTER_ADVERTISED_ADDRESS
export ZITI_ROUTER_MODE=proxy ZITI_ROUTER_TYPE=edge ZITI_BOOTSTRAP=true ZITI_BOOTSTRAP_CONFIG=true ZITI_BOOTSTRAP_ENROLLMENT=true ZITI_ENROLL_TOKEN=/run/sra/enrollment/router.jwt
exec 3>&1
source /bootstrap.bash
if [[ ! -f /var/lib/sra/seed-complete ]]; then
    sra_begin
    bootstrap config.yml
    sra_done
fi
exec ziti router run config.yml
