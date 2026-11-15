#!/bin/bash
# The successful seed exits; an unexpected child exit terminates the container.
sra_supervise() {
    local server=$1 seed=$2 result=0
    trap 'kill -TERM "$server" "$seed" 2>/dev/null || :; wait "$server" "$seed" 2>/dev/null || :' TERM INT
    wait -n "$server" "$seed" || result=$?
    if (( result != 0 )) || [[ ! -f /var/lib/sra/seed-complete ]] || ! kill -0 "$server" 2>/dev/null; then
        kill -TERM "$server" "$seed" 2>/dev/null || :
        wait "$server" "$seed" 2>/dev/null || :
        return 1
    fi
    wait "$server"
}
