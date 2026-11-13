#!/bin/sh
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -v router="${SRA_ROUTER_ADDRESS:-10.25.25.50}" -f /opt/sra/postgres/seed.sql
touch /var/lib/sra/seed-complete
