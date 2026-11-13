#!/bin/bash
set -euo pipefail
waitForAgent "" 30 1
clusterInit ""
ca=/ziti-controller/pki/root/certs/root.cert
for attempt in {1..30}; do
    if ziti edge login "https://${ZITI_CTRL_ADVERTISED_ADDRESS}:1280" -u admin -p "$ZITI_PWD" --ca "$ca"; then break; fi
    if (( attempt == 30 )); then exit 1; fi
    sleep 2
done
ziti edge create edge-router sra-router --tunneler-enabled --role-attributes sra-routers --jwt-output-file /run/sra/enrollment/router.jwt
ziti edge update identity sra-router --role-attributes sra-dialers
ziti edge create identity ot-host --role-attributes sra-hosts --jwt-output-file /run/sra/enrollment/ot-host.jwt
ziti edge create config rtac-ssh-proxy proxy.v1 '{"binding":"0.0.0.0","port":2222,"protocols":["tcp"]}'
ziti edge create config rtac-ssh-host host.v1 "{\"protocol\":\"tcp\",\"address\":\"${SRA_RTAC_ADDRESS:-10.30.30.20}\",\"port\":22}"
ziti edge create service rtac-ssh --role-attributes sra-services --configs rtac-ssh-proxy,rtac-ssh-host
ziti edge create service-policy sra-dial Dial --semantic AnyOf --identity-roles '#sra-dialers' --service-roles '#sra-services'
ziti edge create service-policy sra-bind Bind --semantic AnyOf --identity-roles '#sra-hosts' --service-roles '#sra-services'
ziti edge create edge-router-policy sra-identities --semantic AnyOf --identity-roles '#sra-dialers,#sra-hosts' --edge-router-roles '#sra-routers'
ziti edge create service-edge-router-policy sra-service-routers --semantic AnyOf --service-roles '#sra-services' --edge-router-roles '#sra-routers'
chmod 640 /run/sra/enrollment/*.jwt || echo "sra: bind chmod unavailable"
cp "$ca" /run/sra/enrollment/root.cert
sra_done
