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

# tonistiigi/binfmt is added to the arm64 bundle ONLY: it provides the
# qemu-x86_64 handler that lets the amd64-only openplc image run on arm64
# LINUX (macOS arm64 uses Docker Desktop/Rosetta instead). Without it on
# the SSD, setup.sh's auto-register step has nothing to pull on an
# air-gapped arm64 Linux laptop and openplc won't start. amd64 hosts run
# openplc natively and never need it, so it stays out of images-amd64.tar.
BINFMT_IMAGE="tonistiigi/binfmt:qemu-v10.2.1"  # pinned; keep in sync with setup.sh

# Inspect once and parse both Linux architectures from the same JSON
# response. For indexes .Manifest contains the platform descriptors; for
# single images .Manifest is only a descriptor, so .Image supplies the
# platform. Unknown-platform entries are attestations, not runnable images.
parse_manifest() {
    local img="$1"
    python3 -c '
import json
import sys

image = sys.argv[1]

def field(value, name):
    if not isinstance(value, dict):
        return None
    return value.get(name, value.get(name[0].upper() + name[1:]))

def fail(message):
    print(message, file=sys.stderr)
    raise SystemExit(1)

def image_repository(reference):
    base = reference.split("@", 1)[0]
    final = base.rsplit("/", 1)[-1]
    if ":" in final:
        base = base.rsplit(":", 1)[0]
    return base

try:
    inspection = json.load(sys.stdin)
except (ValueError, TypeError) as error:
    fail("invalid manifest JSON: " + str(error))
if not isinstance(inspection, dict):
    fail("inspection JSON is not an object")
manifest = field(inspection, "manifest")
image_config = field(inspection, "image")
if not isinstance(manifest, dict):
    fail("inspection has no readable manifest")

root_digest = field(manifest, "digest")
if isinstance(root_digest, str) and root_digest and root_digest != "<no value>":
    print("digest\t" + root_digest)

entries = field(manifest, "manifests")
if entries is not None:
    if not isinstance(entries, list):
        fail("manifest index has no readable manifests list")
    print("kind\tindex")
    refs = {}
    for entry in entries:
        platform = field(entry, "platform")
        operating_system = field(platform, "os")
        architecture = field(platform, "architecture")
        if operating_system != "linux" or architecture not in ("amd64", "arm64"):
            continue
        digest = field(entry, "digest")
        if not isinstance(digest, str) or not digest or digest == "<no value>":
            fail("Linux image manifest entry has no digest")
        if architecture not in refs:
            refs[architecture] = image_repository(image) + "@" + digest
    for architecture, reference in refs.items():
        print(architecture + "\t" + reference)
else:
    config = field(manifest, "config")
    if isinstance(config, dict):
        platform = field(config, "platform") or field(manifest, "platform")
        architecture = field(platform, "architecture")
        operating_system = field(platform, "os")
        if architecture is None:
            architecture = field(config, "architecture")
            operating_system = field(config, "os")
    else:
        architecture = field(image_config, "architecture")
        operating_system = field(image_config, "os")
    if not isinstance(architecture, str) or not architecture:
        fail("single-image manifest has no readable platform architecture")
    if not isinstance(operating_system, str) or not operating_system:
        fail("single-image manifest has no readable platform operating system")
    print("kind\tsingle")
    if operating_system == "linux" and architecture in ("amd64", "arm64"):
        print(architecture + "\t" + image)
' "$img" <<< "$2"
}

inspect_platform_manifest() {
    local img="$1" error_file manifest_json
    error_file=$(mktemp "${TMPDIR:-/tmp}/ssd-manifest.XXXXXX") || return 2
    if manifest_json=$(docker buildx imagetools inspect "$img" \
        --format '{{json .}}' 2>"$error_file"); then
        rm -f "$error_file"
    else
        if grep -Eiq '(^|[^[:alnum:]])429[[:space:]]+too[[:space:]]+many[[:space:]]+requests([^[:alnum:]]|$)|toomanyrequests|too[[:space:]]+many[[:space:]]+requests' "$error_file"; then
            rm -f "$error_file"
            return 3
        fi
        rm -f "$error_file"
        return 2
    fi
    parse_manifest "$img" "$manifest_json"
}

rate_limit_abort() {
    local img="$1" first_component="${1%%/*}" registry="docker.io"
    if [[ "$img" == */* ]] && {
        [[ "$first_component" == *.* ]] ||
        [[ "$first_component" == *:* ]] ||
        [[ "$first_component" == localhost ]]
    }; then
        registry="$first_component"
    fi

    case "$registry" in
        docker.io|index.docker.io)
            die "Docker Hub anonymous pull limit reached while inspecting $img. The anonymous pull budget resets within the hour; wait or run 'docker login' and retry."
            ;;
        *)
            die "$registry rate-limited the request while inspecting $img. Wait for its limit to reset or authenticate to $registry with 'docker login $registry', then retry."
            ;;
    esac
}

PREFLIGHT_IMAGES=()
PREFLIGHT_AMD64_REFS=()
PREFLIGHT_ARM64_REFS=()
PREFLIGHT_ARM64_CROSS=()

preflight_image() {
    local img="$1" required_arches="$2" parsed status
    local amd64_ref="" arm64_ref="" arch ref
    say "preflight $img"
    if parsed=$(inspect_platform_manifest "$img"); then
        :
    else
        status=$?
        if [ "$status" -eq 3 ]; then
            rate_limit_abort "$img"
        fi
        die "could not inspect the registry manifest for $img; this is an inspection error, not evidence that a platform is absent."
    fi
    while IFS=$'\t' read -r arch ref; do
        case "$arch" in
            amd64) amd64_ref="$ref" ;;
            arm64) arm64_ref="$ref" ;;
        esac
    done <<< "$parsed"

    if [[ "$required_arches" == *amd64* ]] && [ -z "$amd64_ref" ]; then
        die "readable manifest for $img has no linux/amd64 image."
    fi
    if [[ "$required_arches" == *arm64* ]] && [ -z "$arm64_ref" ]; then
        if [[ "$img" == *rangerdanger-openplc* ]] && [ -n "$amd64_ref" ]; then
            arm64_ref="$amd64_ref"
            PREFLIGHT_ARM64_CROSS+=(yes)
            say "    openplc amd64 image will be cross-included for arm64"
        else
            die "readable manifest for $img has no linux/arm64 image; openplc is the only image allowed to cross-include another architecture."
        fi
    else
        PREFLIGHT_ARM64_CROSS+=(no)
    fi
    PREFLIGHT_IMAGES+=("$img")
    PREFLIGHT_AMD64_REFS+=("$amd64_ref")
    PREFLIGHT_ARM64_REFS+=("$arm64_ref")
}

while IFS= read -r img; do
    [ -z "$img" ] && continue
    preflight_image "$img" "amd64 arm64"
done <<< "$ALL_IMAGES"
preflight_image "$BINFMT_IMAGE" "arm64"

mkdir -p "$OUT" || die "Couldn't create $OUT"
OUT=$(cd "$OUT" && pwd)
say "Output:  $OUT"
say "Version: $VERSION"

AMD64_TAGS=()
ARM64_TAGS=()

stage_arch() {
    local arch="$1" image_list="$2"
    local tarball="$OUT/images-$arch.tar"

    banner "Stage linux/$arch → $(basename "$tarball")"

    local count=0
    local i preflight_index ref cross_arch
    local -a pulled_tags=()
    while IFS= read -r img; do
        [ -z "$img" ] && continue
        count=$((count + 1))
        say "[$count] stage $arch  $img"
        preflight_index=""
        for i in "${!PREFLIGHT_IMAGES[@]}"; do
            if [ "${PREFLIGHT_IMAGES[$i]}" = "$img" ]; then
                preflight_index="$i"
                break
            fi
        done
        [ -n "$preflight_index" ] || die "no preflight result for $img on linux/$arch"
        if [ "$arch" = "amd64" ]; then
            ref="${PREFLIGHT_AMD64_REFS[$preflight_index]}"
        else
            ref="${PREFLIGHT_ARM64_REFS[$preflight_index]}"
        fi
        [ -n "$ref" ] || die "no resolved manifest for $img on linux/$arch"
        cross_arch="${PREFLIGHT_ARM64_CROSS[$preflight_index]}"
        if [ "$arch" = "arm64" ] && [ "$cross_arch" = "yes" ]; then
            say "    cross-arch (amd64 image, runs on arm64 via emulation): $ref"
        elif [ "$ref" != "$img" ]; then
            say "    -> $ref"
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
            || die "docker pull failed for $ref on $arch — see Docker's error above for the cause; refusing to write a partial bundle."
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
