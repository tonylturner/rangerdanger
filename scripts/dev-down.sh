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
# mounted, or exit 1.
#
# Start again with ./setup.sh (or ./scripts/dev-up.sh).

set -euo pipefail

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

project_containers() { docker ps -aq --filter "label=com.docker.compose.project=$1"; }
project_networks()   { docker network ls -q --filter "label=com.docker.compose.project=$1"; }

# Volumes the project's containers mount, recorded before the down so the
# verification can prove that -v removed them.
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
    down_args=(down --remove-orphans)
    volumes=""
    if [ "$project" = rangerdanger ]; then
        down_args=(down -v --remove-orphans)
        volumes=$(project_volumes "$project")
    fi
    echo "[+] Stopping $project"
    (cd "$WORKDIR" && docker compose -p "$project" "${down_args[@]}") || true
    left_c=$(project_containers "$project" | grep -c . || true)
    left_n=$(project_networks "$project" | grep -c . || true)
    left_v=0
    for volume in $volumes; do
        if docker volume inspect "$volume" >/dev/null 2>&1; then left_v=$((left_v + 1)); fi
    done
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
