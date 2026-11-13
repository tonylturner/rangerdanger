#!/bin/bash
set -euo pipefail
[[ -f /var/lib/sra/seed-complete ]]
setpriv --reuid=1001 --regid=1001 --init-groups test -r /recordings/.sra-access
exec /opt/sra/common/http-health.sh /guacamole/
