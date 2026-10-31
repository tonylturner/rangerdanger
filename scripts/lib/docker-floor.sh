# shellcheck shell=bash
# Docker version floor of the range Compose files. The firewall pins its
# NIC names with networks.<net>.interface_name: Compose accepts the key
# from 2.36.0 and creates it only on Engine 28.1 (API 1.49) or later.
# Older Compose rejects the file outright. Docker Desktop 4.42 is the
# first Desktop release that ships both.

DOCKER_FLOOR_COMPOSE=2.36.0
DOCKER_FLOOR_API=1.49

# version_at_least <have> <want>: dotted numeric comparison. A leading
# "v" and a pre-release or build suffix ("2.36.0-desktop.1") are ignored;
# anything else that is not numeric fails.
version_at_least() {
    local have="${1#v}" want="${2#v}" index h w
    local -a have_parts want_parts
    have="${have%%[-+]*}"
    [ -n "$have" ] || return 1
    want="${want%%[-+]*}"
    IFS=. read -r -a have_parts <<<"$have"
    IFS=. read -r -a want_parts <<<"$want"
    for index in 0 1 2; do
        h="${have_parts[index]:-0}"
        w="${want_parts[index]:-0}"
        [[ "$h" =~ ^[0-9]+$ && "$w" =~ ^[0-9]+$ ]] || return 1
        if [ "$((10#$h))" -gt "$((10#$w))" ]; then return 0; fi
        if [ "$((10#$h))" -lt "$((10#$w))" ]; then return 1; fi
    done
    return 0
}

# docker_floor_check <compose version> <engine API version>: silent and 0
# when both meet the floor; otherwise prints the refusal and returns 1.
docker_floor_check() {
    if version_at_least "$1" "$DOCKER_FLOOR_COMPOSE" \
        && version_at_least "$2" "$DOCKER_FLOOR_API"; then
        return 0
    fi
    printf '%s\n' "Docker is too old for RangerDanger: found Compose ${1:-unknown} on Engine API ${2:-unknown}. Need Compose >= $DOCKER_FLOOR_COMPOSE and Engine >= 28.1 (API >= $DOCKER_FLOOR_API): Docker Desktop >= 4.42, or Docker Engine >= 28.1 with the Compose plugin >= $DOCKER_FLOOR_COMPOSE. Update Docker, then re-run."
    return 1
}
