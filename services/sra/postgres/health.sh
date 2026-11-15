#!/bin/sh
set -eu
test -f /var/lib/sra/seed-complete
export PGPASSWORD=lab-db-password
test "$(psql -h 127.0.0.1 -U guacamole -d guacamole -Atqc 'SELECT ready FROM sra_seed_status')" = 1
