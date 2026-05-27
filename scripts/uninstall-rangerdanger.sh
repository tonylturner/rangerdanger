#!/usr/bin/env bash
#
# RangerDanger -- post-workshop cleanup, macOS / Linux.
#
# Brings the stack down, removes its persistent state, optionally
# removes the lab images. There is no equivalent of the Windows
# custom-kernel install on macOS / Linux (Docker on those hosts
# already has CONFIG_NFT_QUEUE=y in its kernel), so this script is
# strictly about the lab itself.
#
# Usage:
#   ./scripts/uninstall-rangerdanger.sh                     # interactive
#   ./scripts/uninstall-rangerdanger.sh --yes               # no prompt
#   ./scripts/uninstall-rangerdanger.sh --yes --remove-images
#   ./scripts/uninstall-rangerdanger.sh --yes --keep-volumes
#
# Flags:
#   --yes / -y           skip the confirmation prompt
#   --remove-images      delete every ghcr.io/tonylturner/rangerdanger-*
#                        + ghcr.io/tonylturner/containd* image (~6 GB freed)
#   --keep-volumes       leave docker volumes alone (preserves lab DB
#                        and student progress for a later session)
#
# Exit codes:
#   0 = uninstall completed (or partial with warnings)
#   1 = user declined the confirmation prompt
#   2 = nothing to do (no rangerdanger state detected)

set -uo pipefail

YES=0
REMOVE_IMAGES=0
KEEP_VOLUMES=0
while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes)            YES=1; shift ;;
        --remove-images)     REMOVE_IMAGES=1; shift ;;
        --keep-volumes)      KEEP_VOLUMES=1; shift ;;
        -h|--help)
            sed -n '2,/^set/p' "$0" | sed 's/^# \?//;/^set/d'
            exit 0 ;;
        *)
            echo "unknown arg: $1 (see --help)" >&2; exit 1 ;;
    esac
done

if [ -t 1 ]; then
    GREEN=$'\e[32m'; YELLOW=$'\e[33m'; BOLD=$'\e[1m'; RESET=$'\e[0m'
else
    GREEN=""; YELLOW=""; BOLD=""; RESET=""
fi
say()    { printf "%s[+]%s %s\n" "$GREEN" "$RESET" "$*"; }
warn()   { printf "%s[!]%s %s\n" "$YELLOW" "$RESET" "$*" >&2; }
banner() { printf "\n%s%s%s\n%s\n\n" "$BOLD" "$1" "$RESET" "$(printf -- '-%.0s' $(seq 1 ${#1}))"; }

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"
OFFLINE_FILE="$ROOT_DIR/docker-compose.offline.yml"
ENV_FILE="$ROOT_DIR/.env"

COMPOSE_ARGS=("-f" "$COMPOSE_FILE")
if [ -f "$OFFLINE_FILE" ]; then COMPOSE_ARGS+=("-f" "$OFFLINE_FILE"); fi

banner "RangerDanger uninstall"

# --- inventory --------------------------------------------------------
CONTAINERS=$(docker ps -a --format '{{.Names}}' --filter "name=rangerdanger-" 2>/dev/null || true)
N_CONTAINERS=$(echo "$CONTAINERS" | grep -c . || true)
say "Containers found: $N_CONTAINERS"
[ -n "$CONTAINERS" ] && echo "$CONTAINERS" | sed 's/^/  /'

ALL_IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null \
    | grep -E '^ghcr\.io/tonylturner/(rangerdanger-|containd)' || true)
N_IMAGES=$(echo "$ALL_IMAGES" | grep -c . || true)
say "Lab images found: $N_IMAGES"

VOLUMES=$(docker volume ls --format '{{.Name}}' 2>/dev/null | grep -E '^rangerdanger' || true)
N_VOLUMES=$(echo "$VOLUMES" | grep -c . || true)
say "Volumes: $N_VOLUMES"

ENV_FOUND=0
[ -f "$ENV_FILE" ] && { ENV_FOUND=1; say ".env file found at $ENV_FILE"; }

# Nothing to do?
if [ "$N_CONTAINERS" = "0" ] && [ "$N_IMAGES" = "0" ] && [ "$ENV_FOUND" = "0" ]; then
    banner "Nothing to uninstall"
    echo "  No RangerDanger state detected on this machine."
    exit 2
fi

# --- confirm ---------------------------------------------------------
banner "About to:"
echo "  1. docker compose down (containers + networks)"
if [ "$KEEP_VOLUMES" = "0" ]; then echo "     -- with -v (removes lab DB, captures, sim state)"
else echo "     -- volumes kept (--keep-volumes)"; fi
if [ "$REMOVE_IMAGES" = "1" ] && [ "$N_IMAGES" != "0" ]; then
    echo "  2. Remove $N_IMAGES Docker image(s) (~6 GB)"
fi
[ "$ENV_FOUND" = "1" ] && echo "  3. Remove $ENV_FILE"
echo ""

if [ "$YES" = "0" ]; then
    printf "Continue? [y/N] "
    read -r ans
    case "$ans" in
        y|Y|yes|YES) ;;
        *) say "Aborted by user. No changes made."; exit 1 ;;
    esac
fi

# --- 1. compose down -------------------------------------------------
banner "Stopping the stack"
if [ "$KEEP_VOLUMES" = "0" ]; then
    docker compose "${COMPOSE_ARGS[@]}" down -v 2>&1 | sed 's/^/  /' || true
else
    docker compose "${COMPOSE_ARGS[@]}" down 2>&1 | sed 's/^/  /' || true
fi
say "compose down complete"

# --- 2. images (optional) --------------------------------------------
if [ "$REMOVE_IMAGES" = "1" ] && [ "$N_IMAGES" != "0" ]; then
    banner "Removing lab images"
    echo "$ALL_IMAGES" | while read -r img; do
        [ -z "$img" ] && continue
        if docker image rm "$img" >/dev/null 2>&1; then
            say "removed $img"
        else
            warn "could not remove $img (in use, missing, or already gone)"
        fi
    done
fi

# --- 3. .env --------------------------------------------------------
if [ "$ENV_FOUND" = "1" ]; then
    banner "Removing setup-written .env"
    rm -f "$ENV_FILE"
    [ ! -f "$ENV_FILE" ] && say "Removed $ENV_FILE" || warn "Could not remove $ENV_FILE"
fi

# --- done ------------------------------------------------------------
banner "RangerDanger removed"
echo "  Containers + networks: stopped"
if [ "$KEEP_VOLUMES" = "0" ]; then echo "  Volumes:               removed"
else echo "  Volumes:               kept (--keep-volumes)"; fi
if [ "$REMOVE_IMAGES" = "1" ]; then echo "  Images:                removed (~6 GB freed)"
else echo "  Images:                kept (pass --remove-images to free disk)"; fi
echo ""
echo "To reinstall later: ./setup.sh"
exit 0
