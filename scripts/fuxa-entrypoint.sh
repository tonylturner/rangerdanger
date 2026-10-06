#!/bin/sh
set -e

/usr/local/bin/set-gateway.sh
exec /usr/local/bin/docker-entrypoint.sh "$@"
