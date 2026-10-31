#!/usr/bin/env bash

# Readiness queries must not hang when Docker Desktop stops responding.
# macOS has no timeout(1). Kill only the CLI child owned by this call;
# image acquisition and the backend's lifecycle are not cancelled here.
docker_bounded() { # <seconds> <docker args...>
    local limit="$1" child watchdog rc=0
    shift
    docker "$@" &
    child=$!
    # The watchdog's sleep outlives the kill below; without the redirect it
    # holds a caller's $(...) pipe open for the whole limit.
    ( sleep "$limit"; kill -KILL "$child" 2>/dev/null ) >/dev/null 2>&1 &
    watchdog=$!
    wait "$child" 2>/dev/null || rc=$?
    kill "$watchdog" 2>/dev/null || true
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
