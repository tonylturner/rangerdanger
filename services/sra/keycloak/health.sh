#!/bin/bash
set -euo pipefail
/opt/sra/common/http-health.sh /realms/sra/protocol/saml/descriptor
# Realm availability proves the first import committed, without wrapping Java.
touch /var/lib/sra/seed-complete
