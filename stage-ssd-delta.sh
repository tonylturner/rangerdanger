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
# service mapping and registry-manifest parsing.
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

INCLUDE_SET=" ${INCLUDE_LIST//,/ } "

# Inspect each versioned image once and parse both Linux architectures
# from the same JSON response. For indexes .Manifest contains the platform
# descriptors; for single images .Manifest is only a descriptor, so .Image
# supplies the platform. Unknown-platform attestations are ignored.
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

manifest_value() {
    local key="$1" data="$2"
    printf '%s\n' "$data" | awk -F '\t' -v key="$key" '$1 == key { print $2; exit }'
}

banner "Comparing $SINCE -> $NEW across $(echo "$CANDIDATE_IMAGES" | wc -l | tr -d ' ') candidate image(s)"

CHANGED=()
CHANGED_MANIFESTS=()
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

    # This response provides both the comparison digest and the platform
    # references later consumed by preflight. Do not inspect it again per
    # architecture.
    if new_manifest=$(inspect_platform_manifest "$new"); then
        new_digest=$(manifest_value digest "$new_manifest")
    else
        inspect_status=$?
        if [ "$inspect_status" -eq 3 ]; then
            rate_limit_abort "$new"
        fi
        die "new-version registry manifest could not be inspected for $new; this is not a platform-availability result."
    fi
    [ -n "$new_digest" ] \
        || die "new version image is missing from the registry or its manifest digest cannot be read: $new"

    # Match against include list (compare against short image name).
    short=$(echo "$new" | sed -E 's|.*/||; s|:.*||')
    if [[ "$INCLUDE_SET" == *" $short "* ]]; then
        FORCED+=("$short")
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        continue
    fi

    if [ "$SAVE_ALL" -eq 1 ]; then
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        continue
    fi

    if [ "$since" = "$new" ]; then
        since_manifest="$new_manifest"
        since_digest="$new_digest"
    elif since_manifest=$(inspect_platform_manifest "$since"); then
        since_digest=$(manifest_value digest "$since_manifest")
    else
        inspect_status=$?
        if [ "$inspect_status" -eq 3 ]; then
            rate_limit_abort "$since"
        fi
        since_digest=""
    fi

    if [ -z "$since_digest" ]; then
        warn "  $short: couldn't read the since-version manifest for $since (not a platform-availability result) - including in delta to be safe"
        MISSING_SINCE+=("$short")
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        continue
    fi

    if [ "$new_digest" = "$since_digest" ]; then
        UNCHANGED+=("$short")
        UNCHANGED_SINCE_REFS+=("$since")
        UNCHANGED_NEW_REFS+=("$new")
    else
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
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

PREFLIGHT_IMAGES=()
PREFLIGHT_AMD64_REFS=()
PREFLIGHT_ARM64_REFS=()
PREFLIGHT_ARM64_CROSS=()

preflight_image() {
    local img="$1" parsed="$2" required_arches="$3"
    local amd64_ref="" arm64_ref="" arch ref
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

# CHANGED_MANIFESTS came from the digest-comparison requests above, so
# checking platform availability here makes no additional registry call.
if [ "${#CHANGED[@]}" -gt 0 ]; then
    for i in "${!CHANGED[@]}"; do
        preflight_image "${CHANGED[$i]}" "${CHANGED_MANIFESTS[$i]}" "amd64 arm64"
    done

    if binfmt_manifest=$(inspect_platform_manifest "$BINFMT_IMAGE"); then
        preflight_image "$BINFMT_IMAGE" "$binfmt_manifest" "arm64"
    else
        inspect_status=$?
        if [ "$inspect_status" -eq 3 ]; then
            rate_limit_abort "$BINFMT_IMAGE"
        fi
        die "could not inspect the registry manifest for $BINFMT_IMAGE; this is an inspection error, not evidence that a platform is absent."
    fi
fi

mkdir -p "$OUT" || die "Couldn't create $OUT"
OUT=$(cd "$OUT" && pwd)
say "Output:           $OUT"

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
    local i preflight_index ref cross_arch
    for img in "${to_stage[@]}"; do
        say "stage $arch  $img"
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
        # like a hang (issue #81); show native layer progress (no --quiet).
        case "$img" in
            *rangerdanger-corp-ws*|*rangerdanger-eng-ws*|*rangerdanger-vendor-jump*)
                say "    large image (~2-3 GB desktop) — a few minutes is normal" ;;
            *rangerdanger-openplc*)
                say "    large image (~1 GB) — give it a minute" ;;
        esac
        docker pull "$ref" \
            || die "docker pull failed for new-version image $ref on $arch; see Docker's error above for the cause."
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
