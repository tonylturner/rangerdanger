#!/bin/bash
set -euo pipefail
exec 3<>/dev/tcp/127.0.0.1/8080
printf 'GET %s HTTP/1.0\r\nHost: localhost\r\n\r\n' "$1" >&3
IFS= read -r response <&3
[[ "$response" == *' 200 '* ]]
