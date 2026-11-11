#!/usr/bin/env bash

# Readiness queries must not hang when Docker Desktop stops responding.
# macOS has no timeout(1). Kill only the CLI child owned by this call;
# image acquisition and the backend's lifecycle are not cancelled here.
docker_bounded() { # <seconds> <docker args...>
    local limit="$1" child watchdog rc=0 call message arg skip=0 parts=0
    shift
    # Opt-in sticky failure for gates: substitutions and negative probes must
    # not turn a timed-out CLI into success on a later poll. Cleanup may keep
    # trying other owned resources, but the timeout log still fails the run.
    if [ -n "${DOCKER_BOUNDED_TIMEOUT_FILE:-}" ] \
        && [ -s "$DOCKER_BOUNDED_TIMEOUT_FILE" ] \
        && [ "${DOCKER_BOUNDED_TIMEOUT_CONTINUE:-0}" -ne 1 ]; then
        return 124
    fi
    call="docker $1"
    if [ "$1" = exec ]; then
        # Name the target and executable, never HTTP bodies or auth headers.
        for arg in "${@:2}"; do
            if [ "$skip" -eq 1 ]; then skip=0; continue; fi
            case "$arg" in
                -e|--env|-w|--workdir|-u|--user) skip=1 ;;
                -*) ;;
                *)
                    call="$call $arg"
                    parts=$((parts + 1))
                    [ "$parts" -lt 2 ] || break
                    ;;
            esac
        done
    elif [ "$1" = inspect ]; then
        call="$call ${!#}"
    elif [ "$1" = compose ]; then
        for arg in "${@:2}"; do
            case "$arg" in
                version|config|up|down|ps|exec|logs|pull|build|restart|stop|start)
                    call="$call $arg"; break ;;
            esac
        done
    elif [ "$#" -gt 1 ]; then
        call="$call $2"
    fi
    # Explicitly preserve stdin for exec -i policy uploads; an asynchronous
    # command without this redirection inherits /dev/null in noninteractive Bash.
    docker "$@" <&0 &
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
        message="[!] Docker CLI timed out after $limit seconds: $call (caller ${FUNCNAME[1]:-main}). Start or restart Docker Desktop or dockerd, then re-run."
        if [ -n "${DOCKER_BOUNDED_TIMEOUT_FILE:-}" ]; then
            printf '%s\n' "$message" >>"$DOCKER_BOUNDED_TIMEOUT_FILE"
        fi
        # The scripts reserve fd 3 for the user's original stderr, outside
        # per-probe redirects and pipelines; sourced callers may not have it.
        if { : >&3; } 2>/dev/null; then
            printf '%s\n' "$message" >&3
        else
            printf '%s\n' "$message" >&2
        fi
        rc=124
    fi
    return "$rc"
}
