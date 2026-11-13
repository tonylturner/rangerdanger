#!/bin/sh
set -eu
if [ "$(id -u)" = 0 ]; then
    mkdir -p /recordings
    chown 1000:2172 /recordings || echo "sra: checking bind access without chown"
    chmod 2770 /recordings || echo "sra: checking bind access without chmod"
    exec setpriv --reuid=1000 --regid=1000 --groups=1000,2172 "$0"
fi
. /opt/sra/common/state.sh
sra_check
umask 0007
if [ ! -f /var/lib/sra/seed-complete ]; then
    find /recordings -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
    sra_begin
    printf 'sra-recordings-readable\n' > /recordings/.sra-access
    sra_done
fi
test -r /recordings/.sra-access
touch /recordings/.sra-write
rm /recordings/.sra-write
exec /opt/guacamole/entrypoint.sh
