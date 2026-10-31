#!/bin/sh
set -e

/usr/local/bin/set-gateway.sh
node /opt/rangerdanger/fuxa/install.js
exec /usr/local/bin/docker-entrypoint.sh "$@"
