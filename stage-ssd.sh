#!/usr/bin/env bash
#
# RangerDanger SSD/airgap stage helper.
#
# Pulls every release image for both linux/amd64 and linux/arm64 from
# GHCR, saves each architecture into a tarball, and copies the repo as
# a tar.gz alongside. The output directory is what students plug into
# setup.sh --from-tarballs <dir>.
#
# Usage:
#   ./stage-ssd.sh <output-dir> [version]
#
# Examples:
#   ./stage-ssd.sh /Volumes/WORKSHOP_SSD v0.1.0
#   ./stage-ssd.sh ./out latest
#
# Output:
#   <output-dir>/images-amd64.tar      (~6 GB)
#   <output-dir>/images-arm64.tar      (~6 GB)
#   <output-dir>/rangerdanger.tgz      (~1 MB — repo archive at HEAD)
#   <output-dir>/README.md             (instructions for the student)
#
# Runtime: ~25–45 min on a fast connection (pulls each image twice,
# once per architecture). Subsequent runs are cache-warm.

set -euo pipefail

if [ $# -lt 1 ] || [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
fi

OUT="$1"
VERSION="${2:-latest}"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"

if [ -t 1 ]; then
    GREEN=$'\e[32m'; YELLOW=$'\e[33m'; RED=$'\e[31m'; BOLD=$'\e[1m'; RESET=$'\e[0m'
else
    GREEN=""; YELLOW=""; RED=""; BOLD=""; RESET=""
fi
say()    { printf "%s[+]%s %s\n" "$GREEN" "$RESET" "$*"; }
warn()   { printf "%s[!]%s %s\n" "$YELLOW" "$RESET" "$*" >&2; }
die()    { printf "%s[✗]%s %s\n" "$RED" "$RESET" "$*" >&2; exit 1; }
banner() { printf "\n%s%s%s\n%s\n\n" "$BOLD" "$1" "$RESET" "$(printf '%.0s─' $(seq 1 ${#1}))"; }

[ -f "$COMPOSE_FILE" ] || die "$COMPOSE_FILE not found — run from repo root."
mkdir -p "$OUT" || die "Couldn't create $OUT"
OUT=$(cd "$OUT" && pwd)
say "Output:  $OUT"
say "Version: $VERSION"

# Image list comes from the release compose. Filter to what's actually
# pullable per architecture: openplc is amd64-only (upstream limit).
ALL_IMAGES=$(docker compose -f "$COMPOSE_FILE" config --images | sort -u)
[ -n "$ALL_IMAGES" ] || die "Couldn't enumerate images from $COMPOSE_FILE"

# Substitute :latest → :$VERSION for first-party rangerdanger-* images.
# Other images already carry their pinned tag/digest from compose.
RESOLVED=$(echo "$ALL_IMAGES" | sed -E "s|^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):latest\$|\\1:$VERSION|")

# openplc is amd64-only because tuttas/openplc_v3 ships only amd64.
ARM64_IMAGES=$(echo "$RESOLVED" | grep -v rangerdanger-openplc)
AMD64_IMAGES="$RESOLVED"

stage_arch() {
    local arch="$1" image_list="$2"
    local tarball="$OUT/images-$arch.tar"

    banner "Stage linux/$arch → $(basename "$tarball")"

    # docker keeps only one platform per image:tag locally, so pulling
    # arch B after arch A overwrites the saved version. The tar must
    # be written between pulls. We fail fast on any pull error rather
    # than warning-and-skipping — a tarball assembled from a partial
    # pull would either include a stale local copy of a missing image
    # or fail mid-`docker save` with a confusing "no such image"
    # error. Better to refuse to build the bundle so the operator
    # notices the missing image and re-runs after fixing the
    # upstream issue (network, GHCR auth, image rename).
    local count=0
    local pulled_csv=""
    while IFS= read -r img; do
        [ -z "$img" ] && continue
        count=$((count + 1))
        say "[$count] pull $arch  $img"
        docker pull --platform="linux/$arch" --quiet "$img" >/dev/null \
            || die "pull failed for $img on $arch — refusing to write a partial bundle. Fix the upstream issue (auth, network, image name), then re-run."
        pulled_csv="$pulled_csv $img"
    done <<< "$image_list"

    # Save the images we actually pulled this iteration. Using the
    # explicit pulled list (not the input list) means a stale local
    # cached image from a prior session can't sneak into the bundle.
    say "save $arch → $tarball"
    # shellcheck disable=SC2086
    docker save -o "$tarball" $pulled_csv \
        || die "docker save failed for $arch"

    local size
    size=$(du -h "$tarball" | awk '{print $1}')
    say "wrote $tarball ($size)"
}

stage_arch amd64 "$AMD64_IMAGES"
stage_arch arm64 "$ARM64_IMAGES"

banner "Stage repo archive → rangerdanger.tgz"
git -C "$ROOT_DIR" archive --format=tar HEAD | gzip > "$OUT/rangerdanger.tgz"
size=$(du -h "$OUT/rangerdanger.tgz" | awk '{print $1}')
say "wrote $OUT/rangerdanger.tgz ($size)"

banner "Write README"
cat > "$OUT/README.md" <<EOF
# RangerDanger — offline / SSD install

Staged $(date -u +%FT%TZ) for version \`$VERSION\`.

## Contents

- \`images-amd64.tar\` — Docker images for Intel / AMD64 hosts
- \`images-arm64.tar\` — Docker images for Apple Silicon / ARM64 hosts (openplc not included; that one is amd64-only)
- \`rangerdanger.tgz\` — Repo archive at $(git -C "$ROOT_DIR" rev-parse --short HEAD) ($(git -C "$ROOT_DIR" log -1 --format=%s | head -c 80))

## Use

Copy the four files to the student's laptop, then:

\`\`\`sh
tar xzf rangerdanger.tgz -C ~
cd ~/rangerdanger
./setup.sh --from-tarballs <dir-containing-the-tarballs>
\`\`\`

(or \`./setup.ps1 -FromTarballs <dir>\` on Windows).

\`setup.sh\` auto-detects the host architecture and loads the right
\`images-<arch>.tar\` before bringing the stack up.
EOF
say "wrote $OUT/README.md"

banner "Done"
echo
echo "  Output dir: $OUT"
echo "  $(ls -lh "$OUT" | awk 'NR>1 {print "  " $9 " " $5}')"
echo
echo "  Total: $(du -sh "$OUT" | awk '{print $1}')"
echo
