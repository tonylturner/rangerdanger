#!/usr/bin/env bash

# Readiness queries must not hang when Docker Desktop stops responding.
# macOS has no timeout(1). Kill only the CLI child owned by this call;
# image acquisition and the backend's lifecycle are not cancelled here.
docker_bounded() { # <seconds> <docker args...>
    local limit="$1" child watchdog rc=0
    shift
    docker "$@" &
    child=$!
    # Never signal the watchdog shell: that can orphan its sleep child.
    # Poll in tenths of a second so a completed CLI is joined promptly, and
    # redirect output so the watchdog cannot hold a caller's $(...) pipe open.
    (
        remaining=$((limit * 10))
        while kill -0 "$child" 2>/dev/null; do
            if [ "$remaining" -le 0 ]; then
                kill -KILL "$child" 2>/dev/null
                break
            fi
            sleep 0.1
            remaining=$((remaining - 1))
        done
    ) >/dev/null 2>&1 &
    watchdog=$!
    wait "$child" 2>/dev/null || rc=$?
    wait "$watchdog" 2>/dev/null || true
    if [ "$rc" -eq 137 ]; then
        # The scripts reserve fd 3 for the user's original stderr, outside
        # per-probe redirects and pipelines; sourced callers may not have it.
        if { : >&3; } 2>/dev/null; then
            printf '[!] Docker CLI timed out after %s seconds. Start or restart Docker Desktop or dockerd, then re-run.\n' "$limit" >&3
        else
            printf '[!] Docker CLI timed out after %s seconds. Start or restart Docker Desktop or dockerd, then re-run.\n' "$limit" >&2
        fi
        rc=124
    fi
    return "$rc"
}
