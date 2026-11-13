#!/bin/bash
set -euo pipefail
[[ -f /var/lib/sra/seed-complete ]]
exec setpriv --reuid=2171 --regid=2171 --init-groups ziti agent stats
