#!/bin/bash
set -euo pipefail
if [[ $(id -u) == 0 ]]; then
    /usr/local/bin/set-gateway.sh
    exec setpriv --reuid=1000 --regid=0 --clear-groups "$0"
fi
source /opt/sra/common/state.sh
sra_check
export KC_HOSTNAME="http://${SRA_KEYCLOAK_ADDRESS:-10.25.25.10}:8080"
if [[ ! -f /var/lib/sra/seed-complete ]]; then
    sra_begin
    mkdir -p /opt/keycloak/data/import
    sed "s|10.25.25.20|${SRA_GUACAMOLE_ADDRESS:-10.25.25.20}|g" /opt/sra/keycloak/realm.json > /opt/keycloak/data/import/sra-realm.json
fi
exec /opt/keycloak/bin/kc.sh start --optimized --import-realm
