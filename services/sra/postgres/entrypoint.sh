#!/bin/sh
set -eu
. /opt/sra/common/state.sh
sra_check
if [ ! -f /var/lib/sra/seed-complete ]; then sra_begin; fi
exec /usr/local/bin/docker-entrypoint.sh "$@"
