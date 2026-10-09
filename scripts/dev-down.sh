#!/usr/bin/env bash
#
# Stop RangerDanger, whatever mode installed it: the range (Compose
# project rangerdanger), then the platform (project rangerdanger-platform).
#
# Usage:
#   ./scripts/dev-down.sh               # range, then platform
#   ./scripts/dev-down.sh --range-only  # the range only (setup's migration)
#   ./scripts/dev-down.sh --volumes     # also remove the anonymous volumes
#                                       # the removed containers mounted
#
# Teardown is label-only: `docker compose -p <project> down
# --remove-orphans` with no -f and no --project-directory, run from an
# empty directory. From the repo root Compose would discover
# docker-compose.yml and act on the platform model under the range's
# name. Each project is then verified by its com.docker.compose.project
# label: 0 containers and 0 networks, or exit 1. The range goes first
# because its containers sit on the platform's mgmt network.
#
# Start again with ./setup.sh (or ./scripts/dev-up.sh).

set -euo pipefail

RANGE_ONLY=0
VOLUMES=0
while [ $# -gt 0 ]; do
    case "$1" in
        --range-only) RANGE_ONLY=1; shift ;;
        --volumes)    VOLUMES=1; shift ;;
        -h|--help)    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'; exit 0 ;;
        *) echo "[x] unknown argument: $1 (see --help)" >&2; exit 2 ;;
    esac
done

PROJECTS=(rangerdanger)
[ "$RANGE_ONLY" = 1 ] || PROJECTS+=(rangerdanger-platform)

WORKDIR=$(mktemp -d)
trap 'rmdir "$WORKDIR"' EXIT

project_containers() { docker ps -aq --filter "label=com.docker.compose.project=$1"; }
project_networks()   { docker network ls -q --filter "label=com.docker.compose.project=$1"; }

# The lab has no named volumes; every volume mount is an image VOLUME
# (the webtops' /config), which `down` without -v leaves behind.
project_volumes() {
    local ids
    ids=$(project_containers "$1")
    [ -n "$ids" ] || return 0
    # shellcheck disable=SC2086 # one container id per word
    docker inspect -f '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}' $ids \
        | sed '/^$/d'
}

failed=0
for project in "${PROJECTS[@]}"; do
    if [ -z "$(project_containers "$project")$(project_networks "$project")" ]; then
        echo "[+] $project: nothing running"
        continue
    fi
    volumes=""
    [ "$VOLUMES" = 1 ] && volumes=$(project_volumes "$project")
    echo "[+] Stopping $project"
    (cd "$WORKDIR" && docker compose -p "$project" down --remove-orphans) || true
    left_c=$(project_containers "$project" | grep -c . || true)
    left_n=$(project_networks "$project" | grep -c . || true)
    if [ "$left_c" != 0 ] || [ "$left_n" != 0 ]; then
        echo "[x] $project left $left_c container(s) and $left_n network(s) behind" >&2
        failed=1
        break
    fi
    echo "[+] $project: 0 containers, 0 networks"
    if [ -n "$volumes" ]; then
        # shellcheck disable=SC2086 # one volume name per word
        if docker volume rm $volumes >/dev/null; then
            echo "[+] $project: removed $(echo "$volumes" | grep -c .) anonymous volume(s)"
        else
            echo "[!] $project: could not remove every anonymous volume" >&2
        fi
    fi
done
exit "$failed"
