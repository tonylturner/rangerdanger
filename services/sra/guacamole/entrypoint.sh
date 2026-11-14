#!/bin/bash
set -euo pipefail
if [[ $(id -u) == 0 ]]; then
    /usr/local/bin/set-gateway.sh
    exec setpriv --reuid=1001 --regid=1001 --init-groups "$0"
fi
source /opt/sra/common/state.sh
sra_check
export GUACD_HOSTNAME=${SRA_GUACD_ADDRESS:-10.25.25.30} GUACD_PORT=4822
export POSTGRESQL_ENABLED=true POSTGRESQL_HOSTNAME=${SRA_POSTGRES_ADDRESS:-10.25.25.31} POSTGRESQL_PORT=5432
export POSTGRESQL_DATABASE=guacamole POSTGRESQL_USERNAME=guacamole POSTGRESQL_PASSWORD=lab-db-password
export EXTENSION_PRIORITY=saml,postgresql SAML_ENABLED=true SAML_STRICT=true SAML_GROUP_ATTRIBUTE=groups
export SAML_IDP_METADATA_URL="http://${SRA_KEYCLOAK_ADDRESS:-10.25.25.10}:8080/realms/sra/protocol/saml/descriptor"
export SAML_ENTITY_ID="http://${SRA_GUACAMOLE_ADDRESS:-10.25.25.20}:8080/guacamole/" SAML_CALLBACK_URL="http://${SRA_GUACAMOLE_ADDRESS:-10.25.25.20}:8080/guacamole/"
export RECORDING_ENABLED=true RECORDING_SEARCH_PATH=/recordings WEBAPP_CONTEXT=guacamole
# The reset also ends guacd sessions. Close history left behind by an interrupted JVM.
PGPASSWORD=$POSTGRESQL_PASSWORD psql -h "$POSTGRESQL_HOSTNAME" -U guacamole -d guacamole -v ON_ERROR_STOP=1 -c 'UPDATE guacamole_connection_history SET end_date=CURRENT_TIMESTAMP WHERE end_date IS NULL; UPDATE guacamole_user_history SET end_date=CURRENT_TIMESTAMP WHERE end_date IS NULL;'
if [[ ! -f /var/lib/sra/seed-complete ]]; then
    sra_begin
    test -r /recordings/.sra-access
    sra_done
fi
exec /opt/guacamole/bin/entrypoint.sh
