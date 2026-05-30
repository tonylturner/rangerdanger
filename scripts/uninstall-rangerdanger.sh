#!/usr/bin/env bash
#
# RangerDanger -- post-workshop cleanup, macOS / Linux.
#
# Brings the stack down, removes its persistent state, optionally
# removes the lab images, and reverts the amd64 emulation handler that
# setup.sh registers on arm64 Linux hosts for OpenPLC (only if setup
# installed it). Unlike Windows there is no custom-kernel install to
# undo -- Docker on macOS / Linux already ships CONFIG_NFT_QUEUE=y -- so
# apart from that emulation shim this script is strictly about the lab.
#
# Usage:
#   ./scripts/uninstall-rangerdanger.sh                     # interactive
#   ./scripts/uninstall-rangerdanger.sh --yes               # no prompt
#   ./scripts/uninstall-rangerdanger.sh --yes --remove-images
#   ./scripts/uninstall-rangerdanger.sh --yes --purge       # clean slate
#   ./scripts/uninstall-rangerdanger.sh --yes --keep-volumes
#
# Image categories this script knows about:
#   A. release  -- pulled ghcr.io/tonylturner/rangerdanger-*, containd
#   B. dev      -- locally-built rangerdanger-<service>:latest images
#   C. base     -- shared public images (alpine, nginx, fuxa, webtop)
#                  that OTHER projects on this host may also use
#
# Flags:
#   --yes / -y            skip the confirmation prompt
#   --remove-images       delete category A release images (~6 GB freed)
#   --remove-dev-images   delete category B locally-built dev images
#   --remove-base-images  delete category C shared base images; never
#                         forced -- any image in use by a running
#                         container is kept, not deleted
#   --purge               --remove-images + --remove-dev-images (a clean
#                         slate for redeploy testing; base images are
#                         left alone -- add --remove-base-images for those)
#   --keep-volumes        leave docker volumes alone (preserves lab DB
#                         and student progress for a later session)
#
# Exit codes:
#   0 = uninstall completed (or partial with warnings)
#   1 = user declined the confirmation prompt
#   2 = nothing to do (no rangerdanger state detected)

set -uo pipefail

YES=0
REMOVE_IMAGES=0
REMOVE_DEV_IMAGES=0
REMOVE_BASE_IMAGES=0
KEEP_VOLUMES=0
while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes)             YES=1; shift ;;
        --remove-images)      REMOVE_IMAGES=1; shift ;;
        --remove-dev-images)  REMOVE_DEV_IMAGES=1; shift ;;
        --remove-base-images) REMOVE_BASE_IMAGES=1; shift ;;
        --purge)              REMOVE_IMAGES=1; REMOVE_DEV_IMAGES=1; shift ;;
        --keep-volumes)       KEEP_VOLUMES=1; shift ;;
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

remove_image_list() {
    # Remove each repo:tag in the newline-separated list ($1). Never forces:
    # an image still in use by another container is kept, not deleted.
    local _img
    while IFS= read -r _img; do
        [ -z "$_img" ] && continue
        if docker image rm "$_img" >/dev/null 2>&1; then
            say "removed $_img"
        else
            warn "kept $_img (in use by another container, or already gone)"
        fi
    done <<EOF
$1
EOF
}

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"
OFFLINE_FILE="$ROOT_DIR/docker-compose.offline.yml"
ENV_FILE="$ROOT_DIR/.env"
BINFMT_MARKER="$ROOT_DIR/.setup-binfmt-amd64"
# Pinned to match setup.sh + the SSD-staged image so offline revert finds it
# locally with no pull. Keep in sync with setup.sh / stage-ssd*.sh.
BINFMT_REF="tonistiigi/binfmt:qemu-v10.2.1"
OS=$(uname -s)

COMPOSE_ARGS=("-f" "$COMPOSE_FILE")
if [ -f "$OFFLINE_FILE" ]; then COMPOSE_ARGS+=("-f" "$OFFLINE_FILE"); fi

banner "RangerDanger uninstall"

# --- inventory --------------------------------------------------------
CONTAINERS=$(docker ps -a --format '{{.Names}}' --filter "name=rangerdanger-" 2>/dev/null || true)
N_CONTAINERS=$(echo "$CONTAINERS" | grep -c . || true)
say "Containers found: $N_CONTAINERS"
[ -n "$CONTAINERS" ] && echo "$CONTAINERS" | sed 's/^/  /'

PRESENT_IMAGES=$(docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null || true)

# Category A -- pulled release images (ghcr.io/tonylturner/rangerdanger-*, containd).
ALL_IMAGES=$(echo "$PRESENT_IMAGES" \
    | grep -E '^ghcr\.io/tonylturner/(rangerdanger-|containd)' || true)
N_IMAGES=$(echo "$ALL_IMAGES" | grep -c . || true)

# Categories B + C are derived from the dev compose file as the single
# source of truth. `docker compose config --images` prints locally-built
# images WITHOUT a tag (e.g. "rangerdanger-backend") and pulled images
# WITH one (e.g. "nginx:1.27-alpine"), which lets us split them apart
# without hard-coding service lists.
DEV_COMPOSE="$ROOT_DIR/docker-compose.yml"
COMPOSE_IMAGES=""
[ -f "$DEV_COMPOSE" ] && COMPOSE_IMAGES=$(docker compose -f "$DEV_COMPOSE" config --images 2>/dev/null || true)

# Category B -- locally-built dev images (rangerdanger-<service>:latest).
DEV_REPOS=$(echo "$COMPOSE_IMAGES" | grep -v ':' | grep . || true)
[ -z "$DEV_REPOS" ] && DEV_REPOS=$(echo "$PRESENT_IMAGES" | sed 's/:.*//' \
    | grep -E '^rangerdanger-[a-z]' | grep -v '/' | sort -u || true)
DEV_IMAGES=""
while IFS= read -r _repo; do
    [ -z "$_repo" ] && continue
    _m=$(echo "$PRESENT_IMAGES" | grep -E "^${_repo}:" || true)
    [ -n "$_m" ] && DEV_IMAGES="${DEV_IMAGES}${_m}"$'\n'
done <<EOF
$DEV_REPOS
EOF
DEV_IMAGES=$(echo "$DEV_IMAGES" | grep . | sort -u || true)
N_DEV_IMAGES=$(echo "$DEV_IMAGES" | grep -c . || true)

# Category C -- shared third-party base images (tagged, non-ghcr, non-built).
BASE_REFS=$(echo "$COMPOSE_IMAGES" | grep ':' \
    | grep -vE '^ghcr\.io/tonylturner/' | grep -vE '^rangerdanger-' \
    | sed -E 's/@sha256:[0-9a-f]+//' | sort -u | grep . || true)
[ -z "$BASE_REFS" ] && BASE_REFS=$(printf '%s\n' \
    'alpine:3.21' 'nginx:1.27-alpine' 'frangoteam/fuxa:latest' 'linuxserver/webtop:ubuntu-mate')
BASE_IMAGES=""
while IFS= read -r _img; do
    [ -z "$_img" ] && continue
    echo "$PRESENT_IMAGES" | grep -qxF "$_img" && BASE_IMAGES="${BASE_IMAGES}${_img}"$'\n'
done <<EOF
$BASE_REFS
EOF
BASE_IMAGES=$(echo "$BASE_IMAGES" | grep . | sort -u || true)
N_BASE_IMAGES=$(echo "$BASE_IMAGES" | grep -c . || true)

say "Release images (ghcr):    $N_IMAGES"
say "Dev images (local build): $N_DEV_IMAGES"
say "Base images (shared):     $N_BASE_IMAGES"

VOLUMES=$(docker volume ls --format '{{.Name}}' 2>/dev/null | grep -E '^rangerdanger' || true)
N_VOLUMES=$(echo "$VOLUMES" | grep -c . || true)
say "Volumes: $N_VOLUMES"

ENV_FOUND=0
[ -f "$ENV_FILE" ] && { ENV_FOUND=1; say ".env file found at $ENV_FILE"; }

BINFMT_FOUND=0
[ -f "$BINFMT_MARKER" ] && { BINFMT_FOUND=1; say "amd64 emulation: registered by setup (will revert)"; }

# Nothing to do?
if [ "$N_CONTAINERS" = "0" ] && [ "$N_IMAGES" = "0" ] && [ "$N_DEV_IMAGES" = "0" ] && [ "$ENV_FOUND" = "0" ] && [ "$BINFMT_FOUND" = "0" ]; then
    banner "Nothing to uninstall"
    echo "  No RangerDanger state detected on this machine."
    exit 2
fi

# --- confirm ---------------------------------------------------------
banner "About to:"
echo "  - docker compose down (containers + networks)"
if [ "$KEEP_VOLUMES" = "0" ]; then echo "      with -v (removes lab DB, captures, sim state)"
else echo "      volumes kept (--keep-volumes)"; fi
if [ "$REMOVE_IMAGES" = "1" ] && [ "$N_IMAGES" != "0" ]; then
    echo "  - Remove $N_IMAGES release image(s) (~6 GB)"
fi
if [ "$REMOVE_DEV_IMAGES" = "1" ] && [ "$N_DEV_IMAGES" != "0" ]; then
    echo "  - Remove $N_DEV_IMAGES locally-built dev image(s)"
fi
if [ "$REMOVE_BASE_IMAGES" = "1" ] && [ "$N_BASE_IMAGES" != "0" ]; then
    echo "  - Remove $N_BASE_IMAGES shared base image(s) -- any in use are skipped"
fi
[ "$ENV_FOUND" = "1" ] && echo "  - Remove $ENV_FILE"
[ "$BINFMT_FOUND" = "1" ] && echo "  - Revert the amd64 emulation handler setup registered (qemu-x86_64)"
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

# --- 2. release images (optional) ------------------------------------
if [ "$REMOVE_IMAGES" = "1" ] && [ "$N_IMAGES" != "0" ]; then
    banner "Removing release images"
    remove_image_list "$ALL_IMAGES"
fi

# --- 2b. dev images (optional) ---------------------------------------
if [ "$REMOVE_DEV_IMAGES" = "1" ] && [ "$N_DEV_IMAGES" != "0" ]; then
    banner "Removing locally-built dev images"
    remove_image_list "$DEV_IMAGES"
fi

# --- 2c. base images (optional, shared) ------------------------------
if [ "$REMOVE_BASE_IMAGES" = "1" ] && [ "$N_BASE_IMAGES" != "0" ]; then
    banner "Removing shared base images"
    warn "These are public base images other projects may share;"
    warn "any image still used by a running container is kept."
    remove_image_list "$BASE_IMAGES"
fi

# --- 3. .env --------------------------------------------------------
if [ "$ENV_FOUND" = "1" ]; then
    banner "Removing setup-written .env"
    rm -f "$ENV_FILE"
    [ ! -f "$ENV_FILE" ] && say "Removed $ENV_FILE" || warn "Could not remove $ENV_FILE"
fi

# --- 4. amd64 emulation (revert only what setup installed) ----------
# setup.sh writes the marker only when IT registered the qemu-x86_64
# binfmt handler on an arm64 Linux host (for OpenPLC). Reverting returns
# the host to its pre-setup state. With no marker we do nothing, so a
# pre-existing or Docker-Desktop emulation handler is never touched.
if [ "$BINFMT_FOUND" = "1" ]; then
    banner "Reverting amd64 emulation"
    if [ "$OS" != "Linux" ]; then
        warn "Marker present but host is not Linux ($OS) — leaving binfmt alone; removing stale marker."
        rm -f "$BINFMT_MARKER"
    else
        docker run --privileged --rm "$BINFMT_REF" --uninstall qemu-x86_64 >/dev/null 2>&1 || true
        if [ ! -e /proc/sys/fs/binfmt_misc/qemu-x86_64 ]; then
            rm -f "$BINFMT_MARKER"
            docker image rm "$BINFMT_REF" >/dev/null 2>&1 || true
            say "amd64 emulation reverted (qemu-x86_64 handler removed)"
        else
            warn "Could not revert amd64 emulation automatically. To finish by hand:"
            warn "    docker run --privileged --rm $BINFMT_REF --uninstall qemu-x86_64"
            warn "Keeping $BINFMT_MARKER so a re-run can retry."
        fi
    fi
fi

# --- done ------------------------------------------------------------
banner "RangerDanger removed"
echo "  Containers + networks: stopped"
if [ "$KEEP_VOLUMES" = "0" ]; then echo "  Volumes:               removed"
else echo "  Volumes:               kept (--keep-volumes)"; fi
if [ "$REMOVE_IMAGES" = "1" ]; then echo "  Release images:        removed (~6 GB freed)"
else echo "  Release images:        kept (pass --remove-images to free disk)"; fi
if [ "$REMOVE_DEV_IMAGES" = "1" ]; then echo "  Dev images:            removed"
elif [ "$N_DEV_IMAGES" != "0" ]; then echo "  Dev images:            kept (pass --remove-dev-images)"; fi
if [ "$REMOVE_BASE_IMAGES" = "1" ]; then echo "  Base images:           removed where not in use"; fi
if [ "$BINFMT_FOUND" = "1" ]; then
    if [ ! -f "$BINFMT_MARKER" ]; then echo "  amd64 emulation:       reverted"
    else echo "  amd64 emulation:       revert incomplete (see warning above)"; fi
fi

# Always show how to reclaim the shared base images by hand, unless we just
# removed them. They are left alone by default because other projects on
# this host may depend on them.
if [ "$REMOVE_BASE_IMAGES" = "0" ] && [ "$N_BASE_IMAGES" != "0" ]; then
    echo ""
    warn "Shared base images left in place (other projects may use them):"
    echo "    docker image rm $(echo "$BASE_IMAGES" | tr '\n' ' ' | sed 's/ *$//')"
    echo "  or re-run with --remove-base-images (in-use images are skipped)."
fi
echo ""
echo "To reinstall later: ./setup.sh"
exit 0
