#!/bin/bash
# Apply an already-checked-out repo-only hotfix to an installed release range.
# Usage: ./scripts/apply-repo-update.sh [--from-tarballs <SSD directory>]
# Like setup.sh, --from-tarballs selects the offline Compose overlay.
# No install-mode marker is persisted by setup; SSD users must pass this flag.
set -euo pipefail

die() { printf 'Repo update refused: %s\n' "$*" >&2; exit 1; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FLAGS=(--project-directory "$ROOT_DIR" -f "$ROOT_DIR/docker-compose.release.yml")

if [ "$#" -gt 0 ]; then
    [ "$#" -eq 2 ] && [ "$1" = "--from-tarballs" ] || \
        die "Usage: $0 [--from-tarballs <SSD directory>]"
    [ -d "$2" ] || die "SSD directory not found: $2"
    [ -f "$ROOT_DIR/docker-compose.offline.yml" ] || die "Offline overlay is missing."
    COMPOSE_FLAGS+=(-f "$ROOT_DIR/docker-compose.offline.yml")
fi

# Ignore untracked/ignored files, including .env and student evidence/data.
# A .git file also counts (linked worktrees). Archive installs cannot check edits.
if [ -e "$ROOT_DIR/.git" ]; then
    git_root=$(git -C "$ROOT_DIR" rev-parse --show-toplevel) || die "A Git checkout is required."
    [ "$git_root" = "$ROOT_DIR" ] || die "Run this helper from its own Git checkout."
    edits=$(git -C "$ROOT_DIR" status --porcelain --untracked-files=no)
    if [ -n "$edits" ]; then
        printf 'Tracked files have local edits; back them up before updating:\n%s\n' "$edits" >&2
        exit 1
    fi
else
    printf 'No .git metadata; skipped Git tracked-file guard. Back up local source edits before extracting an update.\n'
fi

# setup.sh only checks for the Compose subcommand, not a numeric minimum.
# Fail before any restart if this CLI cannot enforce the no-network policy.
up_help=$(docker compose "${COMPOSE_FLAGS[@]}" up --help)
if ! grep -q -- '--pull ' <<< "$up_help"; then
    die "Compose lacks up --pull never. Update Docker Compose before applying this hotfix."
fi

images=$(docker compose "${COMPOSE_FLAGS[@]}" config --images)
[ -n "$images" ] || die "Release Compose resolved no images."
missing=0
while IFS= read -r image; do
    [ -n "$image" ] || continue
    if ! docker image inspect "$image" >/dev/null 2>&1; then
        printf 'Required image is not available locally: %s\n' "$image" >&2
        missing=1
    fi
done <<< "$images"
[ "$missing" -eq 0 ] || die "Restore the installed v0.1.35 images from your SSD/instructor and check Docker is running. No images were downloaded; no services changed."

docker compose "${COMPOSE_FLAGS[@]}" config -q
docker compose "${COMPOSE_FLAGS[@]}" restart backend proxy
docker compose "${COMPOSE_FLAGS[@]}" up -d --no-deps --no-build --pull never --force-recreate vendor_jump

printf 'Repo update applied; .env and data kept. Open http://localhost:8088 (vendor desktop: http://localhost:8088/apps/vendor-jump/). Allow about 20 seconds for the desktop.\n'
