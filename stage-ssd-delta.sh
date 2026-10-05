#!/usr/bin/env bash
#
# RangerDanger SSD delta-stage helper.
#
# Compares two release versions and saves only the images whose
# content changed between them. Lets you push a mid-workshop fix as
# a tens-of-MB tarball instead of re-shipping the full ~6 GB bundle.
#
# Usage:
#   ./stage-ssd-delta.sh <output-dir> <since-version> <new-version> [options]
#
# Options:
#   --include image1,image2   force-include images even if their digest matches
#   --all                     ignore digest comparison; save every image at <new-version>
#   --include-upstream        also delta-check non-rangerdanger upstream images
#                             (containd/nginx/fuxa/webtop/alpine — usually pinned by digest already)
#
# Examples:
#   ./stage-ssd-delta.sh /Volumes/WORKSHOP_SSD/delta-v0.1.7 v0.1.6 v0.1.7
#   ./stage-ssd-delta.sh ./out v0.1.6 v0.1.7-rc1 --include backend,frontend
#   ./stage-ssd-delta.sh ./out v0.1.5 v0.1.7 --all
#
# Output:
#   <output-dir>/delta-amd64.tar       changed images only, amd64
#   <output-dir>/delta-arm64.tar       changed images only, arm64
#   <output-dir>/rangerdanger.tgz      repo archive at HEAD (always included)
#   <output-dir>/DELTA-README.md       per-stage student-facing apply instructions
#
# Runtime: ~5-15 min depending on how many images changed and how
# fresh the layer cache is.
# Compatible with stock macOS Bash 3.2; requires python3 for Compose
# service mapping and registry-manifest fallback.
#
# See docs/workshop-ssd.md for the full operator runbook including
# when to use this vs full stage-ssd.sh.

set -euo pipefail

# Suppress macOS AppleDouble sidecar files (._*) that macOS writes next to
# every file on a non-Apple filesystem (exFAT/FAT/NTFS — the usual Mac↔Windows
# SSD format). Those `._*` files are binary, hidden on macOS but VISIBLE on
# Windows, and make every staged file look like it has a "_" twin that opens
# as garbage. A dot_clean sweep at the end catches any written earlier.
export COPYFILE_DISABLE=1

if [ $# -lt 3 ] || [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    sed -n '2,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
fi

OUT="$1"
SINCE="$2"
NEW="$3"
shift 3

INCLUDE_LIST=""
SAVE_ALL=0
INCLUDE_UPSTREAM=0
while [ $# -gt 0 ]; do
    case "$1" in
        --include)            INCLUDE_LIST="$2"; shift 2 ;;
        --include=*)          INCLUDE_LIST="${1#*=}"; shift ;;
        --all)                SAVE_ALL=1; shift ;;
        --include-upstream)   INCLUDE_UPSTREAM=1; shift ;;
        *) echo "Unknown argument: $1 (see --help)"; exit 1 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
COMPOSE_FILE="$ROOT_DIR/docker-compose.release.yml"

# Cross-included into the arm64 delta (see stage_arch): the qemu-x86_64
# binfmt helper that runs amd64-only OpenPLC on arm64 Linux.
BINFMT_IMAGE="tonistiigi/binfmt:qemu-v10.2.1"  # pinned; keep in sync with setup.sh

if [ -t 1 ]; then
    GREEN=$'\e[32m'; YELLOW=$'\e[33m'; RED=$'\e[31m'; BOLD=$'\e[1m'; RESET=$'\e[0m'
else
    GREEN=""; YELLOW=""; RED=""; BOLD=""; RESET=""
fi
say()    { printf "%s[+]%s %s\n" "$GREEN" "$RESET" "$*"; }
warn()   { printf "%s[!]%s %s\n" "$YELLOW" "$RESET" "$*" >&2; }
die()    { printf "%s[x]%s %s\n" "$RED" "$RESET" "$*" >&2; exit 1; }
banner() { printf "\n%s%s%s\n%s\n\n" "$BOLD" "$1" "$RESET" "$(printf '%.0s-' $(seq 1 ${#1}))"; }

[ -f "$COMPOSE_FILE" ] || die "$COMPOSE_FILE not found - run from repo root."
command -v python3 >/dev/null 2>&1 || die "python3 is required for exact Compose image-to-service mapping."
mkdir -p "$OUT" || die "Couldn't create $OUT"
OUT=$(cd "$OUT" && pwd)

say "Output:           $OUT"
say "Since version:    $SINCE"
say "New version:      $NEW"
[ "$SAVE_ALL" -eq 1 ]          && say "Mode:             --all (skip digest comparison)"
[ -n "$INCLUDE_LIST" ]         && say "Force-include:    $INCLUDE_LIST"
[ "$INCLUDE_UPSTREAM" -eq 1 ]  && say "Upstream images:  included in delta check"

# Enumerate images from compose. Filter to first-party rangerdanger-*
# unless --include-upstream is set, since the upstream images are
# already pinned by sha256 digest in compose and almost never need
# to ship in a delta.
ALL_IMAGES=$(docker compose -f "$COMPOSE_FILE" config --images | sort -u)
[ -n "$ALL_IMAGES" ] || die "Couldn't enumerate images from $COMPOSE_FILE"

if [ "$INCLUDE_UPSTREAM" -eq 0 ]; then
    CANDIDATE_IMAGES=$(echo "$ALL_IMAGES" | grep -E 'ghcr\.io/tonylturner/rangerdanger-' || true)
else
    CANDIDATE_IMAGES="$ALL_IMAGES"
fi

# Substitute the release tag only for first-party RangerDanger images.
# containd deliberately remains :latest; its release cadence is independent.
# Other upstream images keep their pinned tag/digest as-is.
resolve_version() {
    local images="$1" version="$2"
    echo "$images" | sed -E "s|^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):latest\$|\\1:$version|" \
                  | sed -E "s|^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):[^@]+\$|\\1:$version|"
}

SINCE_REF=$(resolve_version "$CANDIDATE_IMAGES" "$SINCE")
NEW_REF=$(resolve_version "$CANDIDATE_IMAGES" "$NEW")

# Get manifest digest for an image:tag without pulling. buildx
# imagetools is reliable across modern Docker; falls back to
# `docker manifest inspect` if buildx isn't available.
remote_digest() {
    local ref="$1"
    if docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$ref" 2>/dev/null; then
        return 0
    fi
    docker manifest inspect "$ref" 2>/dev/null | python3 -c '
import json, sys, hashlib
m = json.load(sys.stdin)
# manifest list: pick the linux/amd64 entry as the canonical digest reference
if m.get("mediaType","").endswith("manifest.list.v2+json") or m.get("manifests"):
    for entry in m.get("manifests", []):
        if entry.get("platform", {}).get("architecture") == "amd64":
            print(entry["digest"]); sys.exit(0)
    sys.exit(1)
print(m.get("config", {}).get("digest", ""))
' 2>/dev/null || echo ""
}

INCLUDE_SET=" ${INCLUDE_LIST//,/ } "

banner "Comparing $SINCE -> $NEW across $(echo "$CANDIDATE_IMAGES" | wc -l | tr -d ' ') candidate image(s)"

CHANGED=()
UNCHANGED=()
FORCED=()
MISSING_SINCE=()

# Read the resolved lists in lockstep with a here-string loop supported
# by stock macOS Bash 3.2.
SINCE_ARR=()
NEW_ARR=()
while IFS= read -r ref; do
    SINCE_ARR+=("$ref")
done <<< "$SINCE_REF"
while IFS= read -r ref; do
    NEW_ARR+=("$ref")
done <<< "$NEW_REF"

UNCHANGED_SINCE_REFS=()
UNCHANGED_NEW_REFS=()

for i in "${!NEW_ARR[@]}"; do
    new="${NEW_ARR[$i]}"
    since="${SINCE_ARR[$i]}"
    [ -z "$new" ] && continue

    # A delta must never be built from an absent/local-only new image.
    # Check every requested new ref even in --all and --include modes.
    new_digest=$(remote_digest "$new" || echo "")
    [ -n "$new_digest" ] \
        || die "new version image is missing from the registry or its manifest cannot be read: $new"

    # Match against include list (compare against short image name).
    short=$(echo "$new" | sed -E 's|.*/||; s|:.*||')
    if [[ "$INCLUDE_SET" == *" $short "* ]]; then
        FORCED+=("$short")
        CHANGED+=("$new")
        continue
    fi

    if [ "$SAVE_ALL" -eq 1 ]; then
        CHANGED+=("$new")
        continue
    fi

    since_digest=$(remote_digest "$since" || echo "")

    if [ -z "$since_digest" ]; then
        warn "  $short: couldn't read $since (not pulled?) - including in delta to be safe"
        MISSING_SINCE+=("$short")
        CHANGED+=("$new")
        continue
    fi

    if [ "$new_digest" = "$since_digest" ]; then
        UNCHANGED+=("$short")
        UNCHANGED_SINCE_REFS+=("$since")
        UNCHANGED_NEW_REFS+=("$new")
    else
        CHANGED+=("$new")
    fi
done

echo
say "  Changed (will be in delta):"
if [ "${#CHANGED[@]}" -eq 0 ]; then
    echo "    (none)"
else
    for c in "${CHANGED[@]}"; do
        short=$(echo "$c" | sed -E 's|.*/||; s|:.*||')
        echo "    $short  ($c)"
    done
fi
echo
[ "${#FORCED[@]}" -gt 0 ]        && say "  Forced via --include: ${FORCED[*]}"
[ "${#MISSING_SINCE[@]}" -gt 0 ] && warn "  Couldn't read since digests for: ${MISSING_SINCE[*]}"
say "  Unchanged (skipped from delta):"
if [ "${#UNCHANGED[@]}" -eq 0 ]; then
    echo "    (none)"
else
    printf '    %s\n' "${UNCHANGED[@]}"
fi
echo

if [ "${#CHANGED[@]}" -eq 0 ]; then
    warn "No image changes detected. Use --all or --include to force, or just"
    warn "ship a new rangerdanger.tgz alone if only repo content changed."
    # Still write the repo archive + readme even if no image deltas.
fi

# Resolve a tagged or digest-pinned image reference to a SINGLE-PLATFORM
# manifest digest reference for the requested arch. See stage-ssd.sh
# for the full rationale; short version: pulling --platform=linux/amd64
# on an arm64 host stores a manifest LIST locally (with pointers to
# both platforms' sub-manifests), and `docker save` then walks the
# list and errors on the missing cross-platform sub-manifest. Pulling
# by the platform-specific manifest digest stores ONLY the single-arch
# manifest, so save walks only that platform.
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
        local base="${img%@*}"
        local repo="${base%:*}"
        printf '%s@%s\n' "$repo" "$digest"
        return
    fi

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
        return
    fi
    return 1
}

stage_arch() {
    local arch="$1"
    local tarball="$OUT/delta-$arch.tar"

    # Images to stage = the changed set, plus tonistiigi/binfmt on arm64.
    # binfmt isn't a rangerdanger image and never shows up in the digest
    # comparison, so — like the WSL2 kernel — we bundle it unconditionally
    # into the arm64 delta. That way a student who only ever applies deltas
    # (e.g. from an SSD staged before binfmt was added) still ends up with
    # the qemu-x86_64 helper OpenPLC needs on arm64 Linux.
    local to_stage=("${CHANGED[@]}")
    [ "$arch" = "arm64" ] && to_stage+=("$BINFMT_IMAGE")

    if [ "${#to_stage[@]}" -eq 0 ]; then
        return 0
    fi

    banner "Stage linux/$arch -> $(basename "$tarball")"

    local -a pulled_tags=()
    for img in "${to_stage[@]}"; do
        say "resolve $arch  $img"
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
                # See stage-ssd.sh for rationale: openplc is amd64-only
                # upstream and runs under emulation on arm64 hosts.
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
        # like a hang (issue #81); show native layer progress (no --quiet).
        case "$img" in
            *rangerdanger-corp-ws*|*rangerdanger-eng-ws*|*rangerdanger-vendor-jump*)
                say "    large image (~2-3 GB desktop) — a few minutes is normal" ;;
            *rangerdanger-openplc*)
                say "    large image (~1 GB) — give it a minute" ;;
        esac
        docker pull "$ref" \
            || die "pull failed for new-version image $ref on $arch (registry entry may be missing or changed)."
        local target_tag="${img%@*}"
        if [ "$ref" != "$target_tag" ]; then
            docker tag "$ref" "$target_tag" \
                || die "docker tag $ref -> $target_tag failed"
        fi
        pulled_tags+=("$target_tag")
    done

    if [ "${#pulled_tags[@]}" -eq 0 ]; then
        say "Nothing to save for $arch (no images compatible with this arch)"
        return 0
    fi

    say "save $arch -> $tarball"
    docker save -o "$tarball" "${pulled_tags[@]}" \
        || die "docker save failed for $arch"

    local size
    size=$(du -h "$tarball" | awk '{print $1}')
    say "wrote $tarball ($size)"
}

if [ "${#CHANGED[@]}" -gt 0 ]; then
    stage_arch amd64
    stage_arch arm64
fi

banner "Stage repo archive -> rangerdanger.tgz"
# --prefix=rangerdanger/ so extraction creates a self-contained
# rangerdanger/ folder (see stage-ssd.sh for the full rationale). The
# delta apply instructions below extract over the student's existing
# ~/rangerdanger in place, which relies on this prefix.
git -C "$ROOT_DIR" archive --prefix=rangerdanger/ --format=tar HEAD | gzip > "$OUT/rangerdanger.tgz"
TGZ_SIZE=$(du -h "$OUT/rangerdanger.tgz" | awk '{print $1}')
say "wrote $OUT/rangerdanger.tgz ($TGZ_SIZE)"

# Bundle the WSL2 kernel asset for the $NEW release. Deltas almost
# always include the kernel because (a) it is small (~25 MB) compared
# to image tarballs and (b) students applying a delta on Windows may
# have applied an older delta that never had the kernel. Bundling
# unconditionally avoids that miss. Graceful skip if the asset
# isn't published yet for $NEW.
KERNEL_README_ROW=""
banner "Bundle WSL2 kernel asset for $NEW (Windows offline support)"
GH_OWNER_REPO="${GH_OWNER_REPO:-tonylturner/rangerdanger}"
KERNEL_URL="https://github.com/${GH_OWNER_REPO}/releases/download/${NEW}/rangerdanger-wsl2-kernel"
KERNEL_SHA_URL="${KERNEL_URL}.sha256"
if curl -fsSL -o /dev/null --head "$KERNEL_URL" 2>/dev/null; then
    say "Downloading $KERNEL_URL"
    curl -fsSL "$KERNEL_URL" -o "$OUT/rangerdanger-wsl2-kernel" \
        || die "kernel download failed mid-stream - refusing to write a partial bundle. Re-run."
    curl -fsSL "$KERNEL_SHA_URL" -o "$OUT/rangerdanger-wsl2-kernel.sha256" \
        || warn "kernel sha256 download failed; on-install verification will be skipped."
    kernel_size=$(du -h "$OUT/rangerdanger-wsl2-kernel" | awk '{print $1}')
    say "wrote $OUT/rangerdanger-wsl2-kernel ($kernel_size)"
    KERNEL_README_ROW="- \`rangerdanger-wsl2-kernel\` + \`.sha256\` -- custom WSL2 kernel for Windows ICS DPI labs (\`setup.ps1 -FromTarballs\` picks it up automatically)."
else
    warn "rangerdanger-wsl2-kernel not yet published for release $NEW."
    warn "  (.github/workflows/build-wsl-kernel.yml builds the kernel on tag push."
    warn "   Re-run this delta after the kernel asset publishes, OR drop the file into $OUT manually.)"
fi

banner "Write DELTA-README.md"

# Derive image-to-service mappings from the release Compose model. This
# avoids guessing names from image names (e.g. eng-ws is eng_workstation).
# `config --images SERVICE` also includes transitive dependencies, so
# read the service's direct image from Compose's resolved JSON model and
# use the per-service image output only to confirm that reference exists.
COMPOSE_SERVICES=$(docker compose -f "$COMPOSE_FILE" config --services) \
    || die "Couldn't enumerate services from $COMPOSE_FILE"
COMPOSE_MODEL=$(docker compose -f "$COMPOSE_FILE" config --format json) \
    || die "Couldn't read the resolved service model from $COMPOSE_FILE"
SERVICE_NAMES=()
SERVICE_REPOS=()
image_repository() {
    local ref="${1%@*}"
    local final_component="${ref##*/}"
    case "$final_component" in
        *:*) ref="${ref%:*}" ;;
    esac
    printf '%s\n' "$ref"
}
while IFS= read -r service; do
    [ -z "$service" ] && continue
    service_images=$(docker compose -f "$COMPOSE_FILE" config --images "$service") \
        || die "Couldn't enumerate images for Compose service $service"
    service_image=$(printf '%s\n' "$COMPOSE_MODEL" | python3 -c '
import json, sys
service = sys.argv[1]
print(json.load(sys.stdin)["services"][service].get("image", ""))
' "$service") || die "Couldn't read the resolved image for Compose service $service"
    [ -n "$service_image" ] || continue
    printf '%s\n' "$service_images" | grep -Fqx "$service_image" \
        || die "Compose did not list direct image $service_image for service $service"
    SERVICE_NAMES+=("$service")
    SERVICE_REPOS+=("$(image_repository "$service_image")")
done <<< "$COMPOSE_SERVICES"

APPLY_TABLE=""
for img in "${CHANGED[@]}"; do
    image_repo=$(image_repository "$img")
    short="${image_repo##*/}"
    svc=""
    for i in "${!SERVICE_NAMES[@]}"; do
        if [ "${SERVICE_REPOS[$i]}" = "$image_repo" ]; then
            if [ -z "$svc" ]; then
                svc="${SERVICE_NAMES[$i]}"
            else
                svc="$svc, ${SERVICE_NAMES[$i]}"
            fi
        fi
    done
    [ -n "$svc" ] || die "Changed image $img does not map to a service in $COMPOSE_FILE"
    APPLY_TABLE="$APPLY_TABLE| \`$short\` | \`$svc\` |
"
done

APPLY_LOAD_COMMAND="# No image archive was created; no image load is needed."
if [ "${#CHANGED[@]}" -gt 0 ]; then
    # Preserve the command substitutions for the generated student recipe.
    # shellcheck disable=SC2016
    APPLY_LOAD_COMMAND='ARCH=$(uname -m | sed '\''s/x86_64/amd64/;s/aarch64/arm64/'\'')
docker load -i "$DELTA_DIR/delta-$ARCH.tar"'
fi

APPLY_RETAG_COMMANDS=""
for i in "${!UNCHANGED_NEW_REFS[@]}"; do
    case "${UNCHANGED_NEW_REFS[$i]}" in
        ghcr.io/tonylturner/rangerdanger-*)
            APPLY_RETAG_COMMANDS="$APPLY_RETAG_COMMANDS"'docker image tag '"${UNCHANGED_SINCE_REFS[$i]}"' '"${UNCHANGED_NEW_REFS[$i]}"'
' ;;
    esac
done
if [ -z "$APPLY_RETAG_COMMANDS" ]; then
    APPLY_RETAG_COMMANDS="# No unchanged first-party image tags need to be created."
fi

cat > "$OUT/DELTA-README.md" <<EOF
# RangerDanger - delta patch

Staged $(date -u +%FT%TZ) for upgrade from \`$SINCE\` -> \`$NEW\`.

## Changed

| Image | Compose service |
|---|---|
$APPLY_TABLE
$KERNEL_README_ROW
$([ "${#UNCHANGED[@]}" -gt 0 ] && echo "## Unchanged (kept from prior install)" && printf -- '- %s\n' "${UNCHANGED[@]}")

## Apply

Run from the student's existing \`~/rangerdanger\` directory:

\`\`\`sh
# Set this to the directory containing this delta bundle.
DELTA_DIR="/path/to/delta-$NEW"
cd ~/rangerdanger

# Keep the installed version/settings for rollback, then stop the release stack.
test -f .env || { echo "Expected .env from setup.sh; cannot preserve the prior version." >&2; exit 1; }
if [ ! -f ".env.before-$NEW" ]; then cp .env ".env.before-$NEW"; fi
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml down

# Update the repo, then load the changed images (if any).
tar xzf "\$DELTA_DIR/rangerdanger.tgz" -C ~
$APPLY_LOAD_COMMAND

# Re-tag unchanged first-party images so every required :$NEW tag exists.
$APPLY_RETAG_COMMANDS

# Select the new release while preserving other .env settings.
NEW_VERSION=$NEW awk '
  BEGIN { version = ENVIRON["NEW_VERSION"]; replaced = 0 }
  /^VERSION=/ {
    if (!replaced) print "VERSION=" version
    replaced = 1
    next
  }
  { print }
  END { if (!replaced) print "VERSION=" version }
' .env > .env.delta.tmp && mv .env.delta.tmp .env

# Start the complete stack from the new version without contacting GHCR.
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
\`\`\`

**ARM64 Linux only:** OpenPLC needs amd64 emulation. \`delta-arm64.tar\`
ships \`tonistiigi/binfmt\` for this; if OpenPLC isn't running after the
restart (\`docker ps | grep openplc\`), register it once with
\`docker run --privileged --rm tonistiigi/binfmt:qemu-v10.2.1 --install amd64\`.
(setup.sh does this automatically on a fresh install; the registration
does not persist across a host reboot.)

If \`docker load\` fails with "no space left on device", free space
without removing the prior \`$SINCE\` image tags; deleting those tags
removes the offline rollback path.

## Rollback

The apply recipe keeps the complete pre-upgrade \`.env\` (including
\`VERSION=$SINCE\`) in \`.env.before-$NEW\`. The old image tags remain
installed, so rollback does not need the network or another bundle:

\`\`\`sh
cd ~/rangerdanger
cp ".env.before-$NEW" .env
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
\`\`\`
EOF
say "wrote $OUT/DELTA-README.md"

# Sweep any macOS AppleDouble (._*) sidecar files written before
# COPYFILE_DISABLE took effect (or via Finder). dot_clean is macOS-only;
# harmless to skip elsewhere. Prevents the binary "._<file>" twins that
# show up next to every staged file on Windows.
if command -v dot_clean >/dev/null 2>&1; then
    dot_clean -m "$OUT" 2>/dev/null || true
    say "swept macOS AppleDouble (._*) sidecar files from $OUT"
fi

banner "Done"
echo
echo "  Output dir:       $OUT"
echo "  Changed images:   ${#CHANGED[@]}"
echo "  Unchanged:        ${#UNCHANGED[@]}"
echo
for file in "$OUT"/*; do
    [ -f "$file" ] || continue
    size=$(du -h "$file" | awk '{print $1}')
    printf "  %-30s %s\n" "${file##*/}" "$size"
done
echo
echo "  Total: $(du -sh "$OUT" | awk '{print $1}')"
echo
echo "  Distribute the files in $OUT to students. The README in"
echo "  that directory contains the exact apply commands."
