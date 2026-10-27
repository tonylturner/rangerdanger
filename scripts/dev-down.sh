#!/usr/bin/env bash
#
# Stop RangerDanger, whatever mode installed it: the range (Compose
# project rangerdanger), then the platform (project rangerdanger-platform).
#
# Usage:
#   ./scripts/dev-down.sh               # range, then platform
#   ./scripts/dev-down.sh --range-only  # the range only (setup's migration)
#
# Teardown is label-only: `docker compose -p <project> down
# --remove-orphans` with no -f and no --project-directory, run from an
# empty directory. From the repo root Compose would discover
# docker-compose.yml and act on the platform model under the range's
# name. The range goes first because its containers sit on the
# platform's mgmt network.
#
# The range is taken down with -v. Range models have no named volumes
# (the package lint forbids them), so -v removes only the anonymous
# volumes its images declare (the webtops' /config); a new range start
# never reuses those. The platform is never taken down with -v.
#
# Each project is then verified by its com.docker.compose.project label:
# 0 containers and 0 networks, and for the range no volume its containers
# mounted, or exit 1. Allow up to ten one-second waits for asynchronous
# volume removal after a successful Compose down.
#
# Start again with ./setup.sh (or ./scripts/dev-up.sh).

set -euo pipefail
# Preserve the user's stderr for timeout guidance outside probe redirects.
exec 3>&2

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
# docker_bounded: readiness queries must not wait forever for a stalled CLI.
# shellcheck source=scripts/lib/docker-bounded.sh
# shellcheck disable=SC1091 # Default ShellCheck does not follow external sources; -x checks this helper.
. "$SCRIPT_DIR/lib/docker-bounded.sh"

RANGE_ONLY=0
while [ $# -gt 0 ]; do
    case "$1" in
        --range-only) RANGE_ONLY=1; shift ;;
        -h|--help)    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'; exit 0 ;;
        *) echo "[x] unknown argument: $1 (see --help)" >&2; exit 2 ;;
    esac
done

PROJECTS=(rangerdanger)
[ "$RANGE_ONLY" = 1 ] || PROJECTS+=(rangerdanger-platform)

WORKDIR=$(mktemp -d)
trap 'rmdir "$WORKDIR"' EXIT

project_containers() { docker_bounded 15 ps -aq --filter "label=com.docker.compose.project=$1"; }
project_networks()   { docker_bounded 15 network ls -q --filter "label=com.docker.compose.project=$1"; }
project_state() {
    containers=$(project_containers "$1") && networks=$(project_networks "$1")
}

# Volumes the project's containers mount, recorded before the down so the
# verification can prove that -v removed them.
project_volumes() {
    local ids
    ids=$(project_containers "$1") || return 1
    [ -n "$ids" ] || return 0
    # shellcheck disable=SC2086 # one container id per word
    docker_bounded 15 inspect -f '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}' $ids \
        | sed '/^$/d' | sort -u
}

# Listing must succeed: a failed inspect cannot distinguish "removed" from
# a daemon/permission error. Observe only; never remove or ignore a survivor.
wait_for_volumes() {
    local volumes=$1 remaining volume attempt
    left_v=0
    [ -n "$volumes" ] || return 0
    for ((attempt = 0; attempt <= 10; attempt++)); do
        remaining=$(docker_bounded 15 volume ls -q) || return 1
        left_v=0
        for volume in $volumes; do
            if grep -Fxq "$volume" <<< "$remaining"; then
                left_v=$((left_v + 1))
            fi
        done
        [ "$left_v" = 0 ] && return 0
        [ "$attempt" = 10 ] || sleep 1
    done
    return 0  # The caller reports the final survivor count.
}

failed=0
for project in "${PROJECTS[@]}"; do
    if ! project_state "$project"; then
        echo "[x] $project: cannot query containers/networks" >&2
        failed=1
        break
    fi
    if [ -z "$containers$networks" ]; then
        echo "[+] $project: nothing running"
        continue
    fi
    down_args=(down --remove-orphans)
    volumes=""
    if [ "$project" = rangerdanger ]; then
        down_args=(down -v --remove-orphans)
        if ! volumes=$(project_volumes "$project"); then
            echo "[x] $project: cannot record mounted volumes" >&2
            failed=1
            break
        fi
    fi
    echo "[+] Stopping $project"
    if ! (cd "$WORKDIR" && docker compose -p "$project" "${down_args[@]}"); then
        echo "[x] $project: Compose down failed" >&2
        failed=1
        break
    fi
    if ! project_state "$project"; then
        echo "[x] $project: cannot verify containers/networks" >&2
        failed=1
        break
    fi
    left_c=$(grep -c . <<< "$containers" || true)
    left_n=$(grep -c . <<< "$networks" || true)
    if ! wait_for_volumes "$volumes"; then
        echo "[x] $project: cannot verify volume removal" >&2
        failed=1
        break
    fi
    if [ "$left_c" != 0 ] || [ "$left_n" != 0 ] || [ "$left_v" != 0 ]; then
        echo "[x] $project left $left_c container(s), $left_n network(s) and $left_v volume(s) behind" >&2
        failed=1
        break
    fi
    if [ "$project" = rangerdanger ]; then
        echo "[+] $project: 0 containers, 0 networks, $(echo "$volumes" | grep -c . || true) anonymous volume(s) removed"
    else
        echo "[+] $project: 0 containers, 0 networks"
    fi
done
exit "$failed"
