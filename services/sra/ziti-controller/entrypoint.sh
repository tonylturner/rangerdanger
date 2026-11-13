#!/bin/bash
set -euo pipefail
if [[ $(id -u) == 0 ]]; then
    /usr/local/bin/set-gateway.sh
    mkdir -p /run/sra/enrollment
    chown 2171:2171 /run/sra/enrollment || echo "sra: checking bind access without chown"
    chmod 2770 /run/sra/enrollment || echo "sra: checking bind access without chmod"
    exec setpriv --reuid=2171 --regid=2171 --init-groups "$0"
fi
source /opt/sra/common/state.sh
sra_check
export ZITI_HOME=/ziti-controller ZITI_CTRL_ADVERTISED_ADDRESS=${SRA_CONTROLLER_ADDRESS:-10.25.25.40}
export ZITI_CTRL_ADVERTISED_PORT=1280 ZITI_BOOTSTRAP=true ZITI_BOOTSTRAP_PKI=true ZITI_BOOTSTRAP_CONFIG=true ZITI_BOOTSTRAP_DATABASE=true ZITI_BOOTSTRAP_CLUSTER=true
export ZITI_CLUSTER_NODE_NAME=sra-controller ZITI_CLUSTER_TRUST_DOMAIN=sra.internal ZITI_USER=admin ZITI_PWD=lab-ziti-password ZITI_AUTO_RENEW_CERTS=false ZITI_BOOTSTRAP_CONSOLE=false
exec 3>&1
source /bootstrap.bash
if [[ -f /var/lib/sra/seed-complete ]]; then exec ziti controller run config.yml; fi
find /run/sra/enrollment -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
: > /run/sra/enrollment/.access
rm /run/sra/enrollment/.access
sra_begin
bootstrap config.yml
sed -i -E 's/duration: 180m/duration: 48h/g' config.yml
ziti controller run config.yml &
server=$!
( source /opt/sra/ziti-controller/seed.sh ) &
seed=$!
source /opt/sra/common/supervise.sh
sra_supervise "$server" "$seed"
