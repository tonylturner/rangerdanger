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
# We don't filter the image list anymore — resolve_platform_ref below
# returns empty for arch-incompatible images and the loop skips them.
AMD64_IMAGES="$RESOLVED"
ARM64_IMAGES="$RESOLVED"

# Resolve a tagged or digest-pinned image reference to a SINGLE-PLATFORM
# manifest digest reference for the requested arch. This is the
# load-bearing piece that makes cross-arch staging work on Apple Silicon
# (and any arm64 host) without `docker save` failing with
# "unable to create manifests file: NotFound: content digest ... not found".
#
# Background: when you `docker pull --platform=linux/amd64 nginx:1.27-alpine`
# on an arm64 host with the containerd snapshotter, Docker fetches the
# manifest LIST (which references both amd64 and arm64 sub-manifests)
# plus only the amd64 layers. A subsequent `docker save nginx:1.27-alpine`
# walks the manifest list and tries to bundle BOTH platforms' manifests,
# fails to find the arm64 sub-manifest's content (we never pulled it),
# and errors out. Pulling by the platform-specific manifest digest
# instead of by tag stores ONLY the single-arch manifest locally, with
# no manifest-list to walk during save.
#
# Returns the resolved single-platform reference on stdout, or exits
# nonzero if the image isn't available for the requested arch (e.g.
# openplc on arm64 — upstream tuttas/openplc_v3 is amd64-only).
resolve_platform_ref() {
    local img="$1" arch="$2"
    local digest
    digest=$(docker buildx imagetools inspect "$img" \
        --format '{{range .Manifest.Manifests}}{{if and (eq .Platform.OS "linux") (eq .Platform.Architecture "'"$arch"'")}}{{.Digest}}{{end}}{{end}}' \
        2>/dev/null | head -1)
    if [ -n "$digest" ] && [ "$digest" != "<no value>" ]; then
        local base="${img%@*}"      # strip @sha256:... if present
        local repo="${base%:*}"     # strip :tag
        printf '%s@%s\n' "$repo" "$digest"
        return 0
    fi
    # No manifest list — single-arch image. Verify the platform matches.
    local single_arch
    single_arch=$(docker buildx imagetools inspect "$img" \
        --format '{{.Manifest.Config.Platform.Architecture}}' 2>/dev/null)
    if [ -z "$single_arch" ]; then
        single_arch=$(docker buildx imagetools inspect "$img" \
            --format '{{.Image.architecture}}' 2>/dev/null)
    fi
    if [ "$single_arch" = "$arch" ]; then
        printf '%s\n' "$img"
        return 0
    fi
    return 1
}

stage_arch() {
    local arch="$1" image_list="$2"
    local tarball="$OUT/images-$arch.tar"

    banner "Stage linux/$arch → $(basename "$tarball")"

    # Per-image: resolve to single-platform manifest digest, pull by
    # digest, then re-tag locally to the user-friendly tag so the saved
    # tar carries it. Students load and `docker compose up` finds the
    # tag-keyed image they expect. Without the re-tag, compose would
    # still try to pull from GHCR because it can't see the digest-only
    # local image.
    local count=0
    local pulled_tags=""
    while IFS= read -r img; do
        [ -z "$img" ] && continue
        count=$((count + 1))
        say "[$count] resolve $arch  $img"
        local ref
        if ! ref=$(resolve_platform_ref "$img" "$arch"); then
            case "$img" in
                *rangerdanger-openplc*)
                    # Apple Silicon students need amd64 openplc - upstream
                    # tuttas/openplc_v3 ships only amd64, and
                    # docker-compose.release.yml pins
                    # `platform: linux/amd64` on the openplc service so
                    # Docker Desktop runs it under Rosetta 2 emulation.
                    # Cross-include the amd64 image in the arm64 bundle
                    # so the SSD is self-sufficient on either arch.
                    ref=$(resolve_platform_ref "$img" "amd64") \
                        || die "openplc: amd64 fallback resolution also failed"
                    say "    cross-arch (amd64 image, runs on arm64 via Rosetta): $ref"
                    ;;
                *)
                    say "    skip — not available for linux/$arch"
                    continue
                    ;;
            esac
        elif [ "$ref" != "$img" ]; then
            say "    -> $ref"
        fi
        docker pull --quiet "$ref" >/dev/null \
            || die "pull failed for $ref on $arch — refusing to write a partial bundle. Fix the upstream issue (auth, network, image name), then re-run."
        # Apply the user-friendly tag locally. The tag-target form
        # cannot include @digest (docker rejects it), so strip any
        # @sha256:... suffix from the original compose reference.
        # After this, the tag points at the single-arch manifest we
        # just pulled, so `docker save` walks only one platform's
        # manifest and can't error on missing cross-platform content.
        local target_tag="${img%@*}"
        if [ "$ref" != "$target_tag" ]; then
            docker tag "$ref" "$target_tag" \
                || die "docker tag $ref → $target_tag failed"
        fi
        pulled_tags="$pulled_tags $target_tag"
    done <<< "$image_list"

    say "save $arch → $tarball"
    # shellcheck disable=SC2086
    docker save -o "$tarball" $pulled_tags \
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

# Write a .version file so setup.sh --from-tarballs can auto-pick the
# right tag without the student having to pass --version. Avoids the
# "No such image: ...:latest" failure mode when the script's default
# VERSION (latest) doesn't match the staged tarball's actual tag.
echo "$VERSION" > "$OUT/.version"
say "wrote $OUT/.version ($VERSION)"

# Bundle the WSL2 kernel asset for Windows students on offline /
# air-gapped laptops. setup.ps1 -FromTarballs looks here for
# rangerdanger-wsl2-kernel + .sha256 and skips its kernel download
# step if found. This step is additive: if the asset isn't built yet
# for $VERSION (rare; CI builds it on tag push), we skip with a
# warning and the SSD still works for everything except ICS DPI on
# Windows.
KERNEL_README_ROW=""
banner "Bundle WSL2 kernel asset (Windows offline support)"
GH_OWNER_REPO="${GH_OWNER_REPO:-tonylturner/rangerdanger}"
if [ "$VERSION" = "latest" ]; then
    KERNEL_URL="https://github.com/${GH_OWNER_REPO}/releases/latest/download/rangerdanger-wsl2-kernel"
    KERNEL_SHA_URL="https://github.com/${GH_OWNER_REPO}/releases/latest/download/rangerdanger-wsl2-kernel.sha256"
else
    KERNEL_URL="https://github.com/${GH_OWNER_REPO}/releases/download/${VERSION}/rangerdanger-wsl2-kernel"
    KERNEL_SHA_URL="https://github.com/${GH_OWNER_REPO}/releases/download/${VERSION}/rangerdanger-wsl2-kernel.sha256"
fi
if curl -fsSL -o /dev/null --head "$KERNEL_URL" 2>/dev/null; then
    say "Downloading $KERNEL_URL"
    curl -fsSL "$KERNEL_URL" -o "$OUT/rangerdanger-wsl2-kernel" \
        || die "kernel download failed mid-stream — refusing to write a partial bundle. Re-run."
    curl -fsSL "$KERNEL_SHA_URL" -o "$OUT/rangerdanger-wsl2-kernel.sha256" \
        || warn "kernel sha256 download failed; on-install verification will be skipped."
    kernel_size=$(du -h "$OUT/rangerdanger-wsl2-kernel" | awk '{print $1}')
    say "wrote $OUT/rangerdanger-wsl2-kernel ($kernel_size)"
    KERNEL_README_ROW="- \`rangerdanger-wsl2-kernel\` + \`.sha256\` — custom WSL2 kernel with CONFIG_NFT_QUEUE=y for Windows ICS DPI labs (see wsl-kernel/README.md). \`setup.ps1 -FromTarballs\` picks it up automatically."
else
    warn "rangerdanger-wsl2-kernel not yet published for release $VERSION."
    warn "  (.github/workflows/build-wsl-kernel.yml builds the kernel on tag push."
    warn "   If you are staging before that workflow has run, re-run stage-ssd.sh after the kernel"
    warn "   asset attaches to the release, OR manually drop rangerdanger-wsl2-kernel + .sha256"
    warn "   into $OUT.)"
    warn "  Without the kernel, Windows students on this SSD lose ICS DPI on Labs 2.3 / 2.3-bonus."
fi

banner "Write README"
cat > "$OUT/README.md" <<EOF
# RangerDanger — offline / SSD install

Staged $(date -u +%FT%TZ) for version \`$VERSION\`.

## Contents

- \`images-amd64.tar\` — Docker images for Intel / AMD64 hosts
- \`images-arm64.tar\` — Docker images for Apple Silicon / ARM64 hosts (openplc is cross-included as the amd64 image and runs under Rosetta 2)
- \`rangerdanger.tgz\` — Repo archive at $(git -C "$ROOT_DIR" rev-parse --short HEAD) ($(git -C "$ROOT_DIR" log -1 --format=%s | head -c 80))
$KERNEL_README_ROW

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
