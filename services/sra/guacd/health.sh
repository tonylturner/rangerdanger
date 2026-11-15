#!/bin/sh
set -eu
test -f /var/lib/sra/seed-complete
setpriv --reuid=1000 --regid=1000 --groups=1000,2172 sh -ec 'test -r /recordings/.sra-access; touch /recordings/.sra-health; rm /recordings/.sra-health'
nc -z 127.0.0.1 4822
