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
# Requires python3 to verify the saved Docker archive manifests.

set -euo pipefail

# Suppress macOS AppleDouble sidecar files (._README.md, ._images-amd64.tar,
# etc.) that macOS otherwise writes next to every file on a non-Apple
# filesystem (exFAT/FAT/NTFS — the usual Mac↔Windows SSD format). Those
# `._*` files are binary, hidden on macOS but VISIBLE on Windows, and make
# every staged file look like it has a "_" twin that opens as garbage. A
# final dot_clean sweep below catches any the OS wrote before this point.
export COPYFILE_DISABLE=1

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
command -v python3 >/dev/null 2>&1 || die "python3 is required to verify the saved Docker archive manifests."

# Image list comes from the release compose; Compose substitutes the
# requested version into every first-party image reference.
# Set VERSION for Compose itself so an existing repo .env cannot select
# a different first-party release than the positional argument.
if ! ALL_IMAGES=$(VERSION="$VERSION" docker compose -f "$COMPOSE_FILE" config --images | sort -u); then
    die "Couldn't enumerate images from $COMPOSE_FILE"
fi
[ -n "$ALL_IMAGES" ] || die "Couldn't enumerate images from $COMPOSE_FILE"

# The Compose interpolation above is the only version-selection
# mechanism. Refuse an environment/configuration mismatch before
# creating the output directory or writing any bundle files.
while IFS= read -r img; do
    case "$img" in
        ghcr.io/tonylturner/rangerdanger-*)
            image_version="${img##*:}"
            [ "$image_version" = "$VERSION" ] \
                || die "Compose resolved first-party image $img, not requested version $VERSION (check Compose interpolation/environment)."
            ;;
    esac
done <<< "$ALL_IMAGES"

mkdir -p "$OUT" || die "Couldn't create $OUT"
OUT=$(cd "$OUT" && pwd)
say "Output:  $OUT"
say "Version: $VERSION"

# tonistiigi/binfmt is added to the arm64 bundle ONLY: it provides the
# qemu-x86_64 handler that lets the amd64-only openplc image run on arm64
# LINUX (macOS arm64 uses Docker Desktop/Rosetta instead). Without it on
# the SSD, setup.sh's auto-register step has nothing to pull on an
# air-gapped arm64 Linux laptop and openplc won't start. amd64 hosts run
# openplc natively and never need it, so it stays out of images-amd64.tar.
BINFMT_IMAGE="tonistiigi/binfmt:qemu-v10.2.1"  # pinned; keep in sync with setup.sh

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
# Returns the resolved single-platform reference on stdout. Exit 1 means
# the readable manifest genuinely has no linux/$arch image; exit 2 means
# the manifest/platform could not be resolved and staging must abort.
resolve_platform_ref() {
    local img="$1" arch="$2"
    local manifest_arches digest
    if ! manifest_arches=$(docker buildx imagetools inspect "$img" \
        --format '{{range .Manifest.Manifests}}{{if eq .Platform.OS "linux"}}{{.Platform.Architecture}} {{end}}{{end}}' \
        2>/dev/null); then
        return 2
    fi
    if [ -n "$manifest_arches" ]; then
        case " $manifest_arches " in
            *" $arch "*) ;;
            *) return 1 ;;
        esac
        if ! digest=$(docker buildx imagetools inspect "$img" \
            --format '{{range .Manifest.Manifests}}{{if and (eq .Platform.OS "linux") (eq .Platform.Architecture "'"$arch"'")}}{{.Digest}}{{end}}{{end}}' \
            2>/dev/null); then
            return 2
        fi
        [ -n "$digest" ] && [ "$digest" != "<no value>" ] || return 2
        local base="${img%@*}"      # strip @sha256:... if present
        local repo="${base%:*}"     # strip :tag
        printf '%s@%s\n' "$repo" "$digest"
        return 0
    fi

    # No manifest list — single-arch image. Verify the platform matches.
    local single_arch
    if ! single_arch=$(docker buildx imagetools inspect "$img" \
        --format '{{.Manifest.Config.Platform.Architecture}}' 2>/dev/null); then
        return 2
    fi
    if [ -z "$single_arch" ] || [ "$single_arch" = "<no value>" ]; then
        if ! single_arch=$(docker buildx imagetools inspect "$img" \
            --format '{{.Image.architecture}}' 2>/dev/null); then
            return 2
        fi
    fi
    [ -n "$single_arch" ] && [ "$single_arch" != "<no value>" ] || return 2
    if [ "$single_arch" = "$arch" ]; then
        printf '%s\n' "$img"
        return 0
    fi
    return 1
}

AMD64_TAGS=()
ARM64_TAGS=()

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
    local -a pulled_tags=()
    while IFS= read -r img; do
        [ -z "$img" ] && continue
        count=$((count + 1))
        say "[$count] resolve $arch  $img"
        local ref
        local resolve_status
        if ref=$(resolve_platform_ref "$img" "$arch"); then
            if [ "$ref" != "$img" ]; then
                say "    -> $ref"
            fi
        else
            resolve_status=$?
            if [ "$resolve_status" -eq 1 ] \
                && [ "$arch" = "arm64" ] \
                && [[ "$img" == *rangerdanger-openplc* ]]; then
                # Apple Silicon students need amd64 openplc - upstream
                # tuttas/openplc_v3 ships only amd64. Cross-include it in
                # the arm64 bundle so it can run under Rosetta on macOS or
                # registered qemu emulation on arm64 Linux.
                if ref=$(resolve_platform_ref "$img" "amd64"); then
                    say "    cross-arch (amd64 image, runs on arm64 via emulation): $ref"
                else
                    resolve_status=$?
                    [ "$resolve_status" -eq 1 ] \
                        && die "openplc manifest has no linux/amd64 image: $img"
                    die "could not resolve the openplc linux/amd64 manifest: $img"
                fi
            elif [ "$resolve_status" -eq 1 ]; then
                die "manifest for $img has no linux/$arch image; openplc is the only image allowed to cross-include another architecture."
            else
                die "could not resolve the manifest for $img on linux/$arch (registry, authentication, or manifest error)."
            fi
        fi
        # Heads-up on the large images so a multi-minute pull doesn't look
        # like a hang (issue #81). The webtop desktop images (corp-ws,
        # eng-ws, vendor-jump) are multi-GB; openplc is also a large pull.
        # Everything else is quick.
        case "$img" in
            *rangerdanger-corp-ws*|*rangerdanger-eng-ws*|*rangerdanger-vendor-jump*)
                say "    large image (~2-3 GB desktop) — a few minutes is normal" ;;
            *rangerdanger-openplc*)
                say "    large image (~1 GB) — give it a minute" ;;
        esac
        # Show Docker's native layer progress (no --quiet) so the operator can
        # see bytes moving on the big pulls instead of staring at a silent line.
        docker pull "$ref" \
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
        pulled_tags+=("$target_tag")
    done <<< "$image_list"

    say "save $arch → $tarball"
    docker save -o "$tarball" "${pulled_tags[@]}" \
        || die "docker save failed for $arch"
    if [ "$arch" = "amd64" ]; then
        AMD64_TAGS=("${pulled_tags[@]}")
    else
        ARM64_TAGS=("${pulled_tags[@]}")
    fi

    local size
    size=$(du -h "$tarball" | awk '{print $1}')
    say "wrote $tarball ($size)"
}

stage_arch amd64 "$ALL_IMAGES"
stage_arch arm64 "$(printf '%s\n%s\n' "$ALL_IMAGES" "$BINFMT_IMAGE")"

banner "Stage repo archive → rangerdanger.tgz"
# --prefix=rangerdanger/ so `tar xzf rangerdanger.tgz -C ~` creates
# ~/rangerdanger/ instead of scattering ~80 repo files into the target
# dir. Without it, the generated README's `cd ~/rangerdanger` lands the
# student in a dir that was never created — a real "wrong directory"
# trap. Every extract instruction in the READMEs/docs assumes this prefix.
git -C "$ROOT_DIR" archive --prefix=rangerdanger/ --format=tar HEAD | gzip > "$OUT/rangerdanger.tgz"
size=$(du -h "$OUT/rangerdanger.tgz" | awk '{print $1}')
say "wrote $OUT/rangerdanger.tgz ($size)"

# Write a .version file so setup.sh --from-tarballs can auto-pick the
# right tag without the student having to pass --version. Avoids the
# "No such image: ...:latest" failure mode when the script's default
# VERSION (latest) doesn't match the staged tarball's actual tag.
echo "$VERSION" > "$OUT/.version"
say "wrote $OUT/.version ($VERSION)"

# docker save archives expose their RepoTags in manifest.json. Verify the
# complete requested tag set, including the version marker, before the
# bundle is reported as complete. Python is used only to parse Docker's
# archive manifest; it never contacts Docker or the registry.
verify_archive() {
    local arch="$1"
    shift
    python3 - "$OUT/images-$arch.tar" "$OUT/.version" "$@" <<'PY'
import json
import sys
import tarfile

archive_path, version_path, *expected = sys.argv[1:]
with open(version_path, encoding="utf-8") as version_file:
    version = version_file.read().strip()
if not version:
    raise SystemExit(f"{version_path} is empty")

try:
    with tarfile.open(archive_path, "r:*") as archive:
        member = archive.extractfile("manifest.json")
        if member is None:
            raise KeyError("manifest.json")
        manifest = json.load(member)
except (OSError, KeyError, tarfile.TarError, json.JSONDecodeError) as exc:
    raise SystemExit(f"cannot verify {archive_path}: invalid Docker save manifest: {exc}")

actual = {
    tag
    for image in manifest
    for tag in (image.get("RepoTags") or [])
}
missing = sorted(set(expected) - actual)
first_party = sorted(
    tag for tag in actual
    if tag.startswith("ghcr.io/tonylturner/rangerdanger-")
)
wrong_version = sorted(
    tag for tag in first_party
    if tag.rpartition(":")[2] != version
)
if missing or wrong_version:
    if missing:
        print("missing saved image tags: " + ", ".join(missing), file=sys.stderr)
    if wrong_version:
        print(
            f"first-party image tags do not match .version={version}: "
            + ", ".join(wrong_version),
            file=sys.stderr,
        )
    raise SystemExit(1)
print(
    f"verified {archive_path}: all {len(expected)} enumerated image tags are present; "
    f"first-party tags match .version={version}"
)
PY
}

verify_archive amd64 "${AMD64_TAGS[@]}" || die "amd64 archive verification failed"
verify_archive arm64 "${ARM64_TAGS[@]}" || die "arm64 archive verification failed"

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
- \`images-arm64.tar\` — Docker images for Apple Silicon / ARM64 hosts. Includes the amd64 openplc image (cross-arch; macOS runs it under Rosetta 2) plus \`tonistiigi/binfmt\`, which \`setup.sh\` uses to register amd64 emulation on arm64 *Linux* so openplc runs fully offline.
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

# Belt-and-suspenders: sweep any macOS AppleDouble (._*) sidecar files the OS
# may have written to the volume before COPYFILE_DISABLE took effect (or via
# Finder/other tooling). dot_clean is macOS-only; harmless to skip elsewhere.
# Without this, Windows shows a binary "._<file>" twin next to every staged
# file, which reads as garbage in editors.
if command -v dot_clean >/dev/null 2>&1; then
    dot_clean -m "$OUT" 2>/dev/null || true
    say "swept macOS AppleDouble (._*) sidecar files from $OUT"
fi

banner "Done"
echo
echo "  Output dir: $OUT"
for file in "$OUT"/*; do
    [ -f "$file" ] || continue
    size=$(du -h "$file" | awk '{print $1}')
    printf "  %-30s %s\n" "${file##*/}" "$size"
done
echo
echo "  Total: $(du -sh "$OUT" | awk '{print $1}')"
echo
