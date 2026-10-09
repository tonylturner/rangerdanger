#!/usr/bin/env bash
#
# RangerDanger SSD delta-stage helper.
#
# Compares two release versions and saves only the images whose
# content changed between them. Delta size depends on how many image
# digests differ; a repo-only update can avoid re-shipping image archives.
#
# Usage:
#   ./stage-ssd-delta.sh <output-dir> <since-version> <new-version> [options]
#
# Options:
#   --include image1,image2   force-include images even if their digest matches.
#                             Either form works (gps-sim or rangerdanger-gps-sim);
#                             an entry that matches no candidate is an error.
#   --all                     ignore digest comparison; save every image at <new-version>
#   --include-upstream        also delta-check unchanged non-rangerdanger upstream
#                             images; new/changed upstream references are included automatically
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
# Pinned; keep in sync with setup.sh, scripts/persist-emulation.sh,
# stage-ssd.sh, stage-ssd.ps1, stage-ssd-delta.sh and stage-ssd-delta.ps1.
BINFMT_IMAGE="tonistiigi/binfmt:qemu-v10.2.1"

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

# Compose package models must be enumerated independently: combining
# them would merge same-named services from different packages.
collect_release_images() {
    local package_dir compose_file
    RANGERDANGER_ROOT="$ROOT_DIR" VERSION="$NEW" \
        docker compose --project-directory "$ROOT_DIR" \
        -f "$COMPOSE_FILE" config --images || return 1
    for package_dir in "$ROOT_DIR"/lab-definitions/packages/*; do
        [ -d "$package_dir" ] || continue
        compose_file="$package_dir/compose.release.yml"
        [ -f "$compose_file" ] || {
            printf 'Missing package release Compose file: %s\n' "$compose_file" >&2
            return 1
        }
        RANGERDANGER_ROOT="$ROOT_DIR" VERSION="$NEW" \
            docker compose --project-directory "$ROOT_DIR" \
            -f "$compose_file" config --images || return 1
    done
}
if ! ALL_IMAGES=$(collect_release_images | sort -u); then
    die "Couldn't enumerate the platform/package release image union"
fi
[ -n "$ALL_IMAGES" ] || die "Couldn't enumerate images from the release models"

# Delta membership is release-owned data, not inferred from today's package
# tree. Both records must describe the same package models as their releases.
MEMBERSHIP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/rd-delta-membership.XXXXXX") \
    || die "Couldn't create a temporary directory for release membership"
trap 'rm -rf "$MEMBERSHIP_DIR"' EXIT
OWNER_REPO="${GH_OWNER_REPO:-tonylturner/rangerdanger}"
fetch_release_record() {
    local tag="$1" output="$2"
    python3 - "$OWNER_REPO" "$tag" "$output" <<'PY'
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen

owner_repo, tag, output = sys.argv[1:]
url = (f"https://github.com/{owner_repo}/releases/download/"
       f"{quote(tag, safe='')}/release-images.json")
try:
    request = Request(url, headers={"User-Agent": "RangerDanger-SSD-delta"})
    with urlopen(request, timeout=30) as response, open(output, "wb") as record:
        record.write(response.read())
except (HTTPError, URLError, OSError) as error:
    raise SystemExit(f"cannot download release-images.json for {tag}: {error}")
PY
}
SINCE_RECORD="$MEMBERSHIP_DIR/since.json"
NEW_RECORD="$MEMBERSHIP_DIR/new.json"
fetch_release_record "$SINCE" "$SINCE_RECORD" \
    || die "Could not read the old release package membership"
fetch_release_record "$NEW" "$NEW_RECORD" \
    || die "Could not read the new release package membership"

printf '%s\n' "$ALL_IMAGES" > "$MEMBERSHIP_DIR/compose-images.txt"
CANDIDATE_NEW_REFS=()
CANDIDATE_OLD_REFS=()
CANDIDATE_OLD_MEMBERSHIP=()
MEMBERSHIP_ARGS=(
    delta-candidates
    --since-record "$SINCE_RECORD"
    --new-record "$NEW_RECORD"
    --since "$SINCE"
    --new "$NEW"
    --compose-images "$MEMBERSHIP_DIR/compose-images.txt"
    --format tsv
)
if [ "$INCLUDE_UPSTREAM" -eq 1 ] || [ "$SAVE_ALL" -eq 1 ]; then
    MEMBERSHIP_ARGS+=(--include-upstream)
fi
if ! CANDIDATE_DATA=$(python3 "$ROOT_DIR/scripts/release_image_plan.py" \
    "${MEMBERSHIP_ARGS[@]}"); then
    die "Release records and Compose image union disagree or lack package membership"
fi
CANDIDATE_IMAGES=""
while IFS=$'\t' read -r new_ref old_ref _ _ old_member _; do
    [ -n "$new_ref" ] || continue
    CANDIDATE_IMAGES="${CANDIDATE_IMAGES}${CANDIDATE_IMAGES:+
}$new_ref"
    CANDIDATE_NEW_REFS+=("$new_ref")
    CANDIDATE_OLD_REFS+=("$old_ref")
    CANDIDATE_OLD_MEMBERSHIP+=("$old_member")
done <<< "$CANDIDATE_DATA"
[ -n "$CANDIDATE_IMAGES" ] || die "The new release membership has no stageable images"

# --include accepts either form a student-facing doc would use:
# "gps-sim" or "rangerdanger-gps-sim". Normalise both sides to the short
# form so the documented examples actually match.
INCLUDE_SET=" "
for include_name in ${INCLUDE_LIST//,/ }; do
    INCLUDE_SET="$INCLUDE_SET${include_name#rangerdanger-} "
done

# Inspect each versioned image once and parse both Linux architectures
# from the same JSON response. For indexes .Manifest contains the platform
# descriptors; for single images .Image supplies the platform.
# Unknown-platform attestations are ignored.
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
CHANGED_SINCE_REFS=()
UNCHANGED=()
FORCED=()
MISSING_SINCE=()

UNCHANGED_SINCE_REFS=()
UNCHANGED_NEW_REFS=()

for i in "${!CANDIDATE_NEW_REFS[@]}"; do
    new="${CANDIDATE_NEW_REFS[$i]}"
    since="${CANDIDATE_OLD_REFS[$i]}"
    old_member="${CANDIDATE_OLD_MEMBERSHIP[$i]}"
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
    if [[ "$INCLUDE_SET" == *" ${short#rangerdanger-} "* ]]; then
        FORCED+=("$short")
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        CHANGED_SINCE_REFS+=("$since")
        continue
    fi

    if [ "$SAVE_ALL" -eq 1 ]; then
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        CHANGED_SINCE_REFS+=("$since")
        continue
    fi

    if [ "$old_member" != "yes" ]; then
        warn "  $short: absent from the $SINCE package membership; including in delta"
        MISSING_SINCE+=("$short")
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        CHANGED_SINCE_REFS+=("$since")
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
        CHANGED_SINCE_REFS+=("$since")
        continue
    fi

    if [ "$new_digest" = "$since_digest" ]; then
        UNCHANGED+=("$short")
        UNCHANGED_SINCE_REFS+=("$since")
        UNCHANGED_NEW_REFS+=("$new")
    else
        CHANGED+=("$new")
        CHANGED_MANIFESTS+=("$new_manifest")
        CHANGED_SINCE_REFS+=("$since")
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
if [ -n "$INCLUDE_LIST" ]; then
    for include_name in ${INCLUDE_LIST//,/ }; do
        matched=0
        for f in ${FORCED[@]+"${FORCED[@]}"}; do
            [ "${f#rangerdanger-}" = "${include_name#rangerdanger-}" ] && matched=1
        done
        [ "$matched" -eq 1 ] || die "--include $include_name matched no image in the selected new platform/package membership; unchanged upstream images need --include-upstream."
    done
fi
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
    kernel_path="$OUT/rangerdanger-wsl2-kernel"
    kernel_sha_path="$OUT/rangerdanger-wsl2-kernel.sha256"
    if ! curl -fsSL "$KERNEL_URL" -o "$kernel_path"; then
        rm -f "$kernel_path" "$kernel_sha_path"
        die "kernel download failed mid-stream - refusing to write a partial bundle. Re-run."
    fi
    # One retry, then refuse to write a bundle whose kernel cannot be verified.
    if ! sha_error=$(curl -fsSL --retry 2 --retry-delay 2 "$KERNEL_SHA_URL" \
        -o "$kernel_sha_path" 2>&1); then
        rm -f "$kernel_path" "$kernel_sha_path"
        die "WSL2 kernel checksum unavailable; refusing to stage an unverifiable kernel. Re-run stage-ssd-delta.sh, or place both rangerdanger-wsl2-kernel and rangerdanger-wsl2-kernel.sha256 in $OUT by hand. $KERNEL_SHA_URL: ${sha_error:-no error detail from curl}"
    fi

    expected_sha256=""
    IFS=' ' read -r expected_sha256 _ < "$kernel_sha_path" || true
    if [[ ! "$expected_sha256" =~ ^[[:xdigit:]]{64}$ ]]; then
        rm -f "$kernel_path" "$kernel_sha_path"
        die "WSL2 kernel checksum is missing or invalid; refusing to stage an unverifiable kernel. Re-run stage-ssd-delta.sh, or place both rangerdanger-wsl2-kernel and rangerdanger-wsl2-kernel.sha256 in $OUT by hand."
    fi
    expected_sha256=$(printf '%s' "$expected_sha256" | tr '[:upper:]' '[:lower:]')
    if ! actual_sha256=$(shasum -a 256 "$kernel_path" | awk '{print $1}'); then
        rm -f "$kernel_path" "$kernel_sha_path"
        die "Could not verify the staged WSL2 kernel; refusing to stage an unverifiable kernel. Re-run stage-ssd-delta.sh, or place both rangerdanger-wsl2-kernel and rangerdanger-wsl2-kernel.sha256 in $OUT by hand."
    fi
    if [ "$actual_sha256" != "$expected_sha256" ]; then
        rm -f "$kernel_path" "$kernel_sha_path"
        die "WSL2 kernel checksum mismatch; refusing to stage an unverifiable kernel. Re-run stage-ssd-delta.sh, or place both rangerdanger-wsl2-kernel and rangerdanger-wsl2-kernel.sha256 in $OUT by hand."
    fi
    kernel_size=$(du -h "$kernel_path" | awk '{print $1}')
    say "wrote $kernel_path ($kernel_size)"
    KERNEL_README_ROW="- \`rangerdanger-wsl2-kernel\` + \`.sha256\` -- custom WSL2 kernel for Windows ICS DPI labs (\`setup.ps1 -FromTarballs\` picks it up automatically)."
else
    warn "rangerdanger-wsl2-kernel not yet published for release $NEW."
    warn "  (.github/workflows/build-wsl-kernel.yml builds the kernel on tag push."
    warn "   Re-run this delta after the kernel asset publishes, OR drop the file into $OUT manually.)"
fi

banner "Write DELTA-README.md"
UNCHANGED_SECTION=""
if [ "${#UNCHANGED[@]}" -gt 0 ]; then
    UNCHANGED_SECTION=$'\n## Unchanged (kept from prior install)\n\n'
    for image in "${UNCHANGED[@]}"; do
        UNCHANGED_SECTION+="- $image"$'\n'
    done
fi
COMPOSE_DOWN_FUNCTION=$(cat <<'FUNCTION'
compose_down_project() {
    local project="$1" compose_dir range_containers volume_names remaining_volumes volume
    compose_dir=$(mktemp -d) || return 1
    volume_names=""
    if [ "$project" = "rangerdanger" ]; then
        range_containers=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || return 1
        if [ -n "$range_containers" ]; then
            volume_names=$(printf '%s\n' "$range_containers" |
                xargs docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}') || return 1
        fi
        if ! (cd "$compose_dir" && docker compose -p "$project" down -v --remove-orphans); then
            rmdir "$compose_dir"
            return 1
        fi
    elif ! (cd "$compose_dir" && docker compose -p "$project" down --remove-orphans); then
        rmdir "$compose_dir"
        return 1
    fi
    rmdir "$compose_dir"
    [ -z "$(docker ps -aq --filter "label=com.docker.compose.project=$project")" ] \
        || { echo "Containers remain for Compose project $project." >&2; return 1; }
    [ -z "$(docker network ls -q --filter "label=com.docker.compose.project=$project")" ] \
        || { echo "Networks remain for Compose project $project." >&2; return 1; }
    if [ "$project" = "rangerdanger" ]; then
        remaining_volumes=$(docker volume ls -q) || return 1
        while IFS= read -r volume; do
            [ -n "$volume" ] || continue
            printf '%s\n' "$remaining_volumes" | grep -Fxq "$volume" && {
                echo "Range volume $volume remains after teardown." >&2
                return 1
            }
        done <<< "$volume_names"
    fi
}
FUNCTION
)
START_PLATFORM_FUNCTION=$(cat <<'FUNCTION'
start_platform_and_selected_range() {
    local ready attempt range_status phase
    RANGERDANGER_ROOT="$PWD" docker compose -p rangerdanger-platform \
        --project-directory "$PWD" -f docker-compose.release.yml \
        up -d --pull never || return 1
    ready=0
    for attempt in $(seq 1 120); do
        if curl -fsS --max-time 5 http://127.0.0.1:8088/api/health >/dev/null; then
            ready=1
            break
        fi
        sleep 2
    done
    [ "$ready" -eq 1 ] || { echo "Platform API did not become ready." >&2; return 1; }
    curl -fsS --max-time 10 -X POST -H 'Content-Type: application/json' \
        -d '{}' http://127.0.0.1:8088/api/range >/dev/null || return 1
    for attempt in $(seq 1 150); do
        range_status=$(curl -fsS --max-time 5 http://127.0.0.1:8088/api/range) || {
            sleep 2
            continue
        }
        phase=$(printf '%s' "$range_status" | python3 -c \
            'import json,sys; print(json.load(sys.stdin).get("phase", ""))') || return 1
        [ "$phase" = "ready" ] && return 0
        if [ "$phase" = "failed" ]; then
            echo "Selected range failed to start: $range_status" >&2
            return 1
        fi
        sleep 2
    done
    echo "Selected range did not become ready." >&2
    return 1
}
FUNCTION
)
README_TEMPLATE="$MEMBERSHIP_DIR/DELTA-README.template"
APPLY_TABLE=""
for img in "${CHANGED[@]}"; do
    image_repo=$(python3 -c '
import sys
value = sys.argv[1].split("@", 1)[0]
last = value.rsplit("/", 1)[-1]
print(value.rsplit(":", 1)[0] if ":" in last else value)
' "$img") || die "Could not normalize changed image $img"
    short="${image_repo##*/}"
    svc=$(python3 - "$NEW_RECORD" "$image_repo" <<'PY'
import json
import sys

record = json.load(open(sys.argv[1], encoding="utf-8"))
target = sys.argv[2]
models = record["compose_files"]
owners = []
for owner, model in [("platform", models["platform"])] + sorted(models["packages"].items()):
    for service, image in model["services"].items():
        value = image.split("@", 1)[0]
        last = value.rsplit("/", 1)[-1]
        repository = value.rsplit(":", 1)[0] if ":" in last else value
        if repository == target:
            owners.append(f"{owner}/{service}")
print(", ".join(sorted(set(owners))))
PY
) || die "Could not read image membership for $img"
    [ -n "$svc" ] || die "Changed image $img has no package/service membership in $NEW"
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

APPLY_BACKUP_COMMANDS=""
ROLLBACK_RESTORE_COMMANDS=""
for i in "${!CHANGED[@]}"; do
    since_tag="${CHANGED_SINCE_REFS[$i]%@*}"
    target_tag="${CHANGED[$i]%@*}"
    [ "$since_tag" = "$target_tag" ] || continue
    parked_tag="${target_tag%:*}:before-$NEW"
    APPLY_BACKUP_COMMANDS="$APPLY_BACKUP_COMMANDS"'if ! docker image inspect "'"$parked_tag"'" >/dev/null 2>&1; then
    if docker image inspect "'"$target_tag"'" >/dev/null 2>&1; then
        docker image tag "'"$target_tag"'" "'"$parked_tag"'"
    fi
fi
'
    ROLLBACK_RESTORE_COMMANDS="$ROLLBACK_RESTORE_COMMANDS"'if docker image inspect "'"$parked_tag"'" >/dev/null 2>&1; then
    docker image tag "'"$parked_tag"'" "'"$target_tag"'"
fi
'
done
if [ -z "$APPLY_BACKUP_COMMANDS" ]; then
    APPLY_BACKUP_COMMANDS="# No mutable image tags need to be parked."
    ROLLBACK_RESTORE_COMMANDS="# No mutable image tags need to be restored."
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

cat > "$README_TEMPLATE" <<'README'
# RangerDanger - delta patch

Staged __STAGED_AT__ for upgrade from \`__SINCE__\` -> \`__NEW__\`.

## Changed

| Image | Package/service membership |
|---|---|
__APPLY_TABLE__
__KERNEL_README_ROW__
__UNCHANGED_SECTION__

## Apply

Run from the student's existing \`~/rangerdanger\` directory:

\`\`\`sh
set -e
# Set this to the directory containing this delta bundle.
DELTA_DIR="/path/to/delta-__NEW__"
cd ~/rangerdanger
test -f .env || { echo "Expected .env from setup.sh; cannot preserve the prior version." >&2; exit 1; }
__COMPOSE_DOWN_FUNCTION__
__START_PLATFORM_FUNCTION__
# Only apply a delta to the version it was built from. Check before stopping
# services or touching the install so a wrong or repeated delta is harmless.
CURRENT_VERSION=\$(awk '/^VERSION=/ { sub(/^VERSION=/, ""); print; exit }' .env)
if [ "\$CURRENT_VERSION" = "__NEW__" ]; then
    echo "This delta looks already applied: install VERSION is \$CURRENT_VERSION, but this delta expects __SINCE__; nothing was changed." >&2
    exit 1
fi
if [ "\$CURRENT_VERSION" != "__SINCE__" ]; then
    if [ -n "\$CURRENT_VERSION" ]; then
        echo "Install VERSION is \$CURRENT_VERSION; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    else
        echo "Install .env has no VERSION= line; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    fi
    exit 1
fi

# Stop only the two owned projects by labels before reading mutable state
# into the rollback snapshot. Teardown does not load either Compose model.
if ! compose_down_project rangerdanger || ! compose_down_project rangerdanger-platform
then
    echo "Could not stop and verify both RangerDanger projects; no snapshot or repo changes were made." >&2
    exit 1
fi

# Save the complete existing install, including .env and local lab/policy
# edits, for rollback. Keep the first snapshot if this delta is re-applied.
SNAPSHOT="../rangerdanger.before-__NEW__.tar.gz"
if [ ! -f "\$SNAPSHOT" ]; then
    tar czf "\$SNAPSHOT" -C .. rangerdanger || {
        rm -f "\$SNAPSHOT"
        echo "Could not snapshot ~/rangerdanger; refusing to apply the delta. The RangerDanger projects are stopped; restore them from the rollback procedure." >&2
        exit 1
    }
fi
tar tzf "\$SNAPSHOT" >/dev/null || {
    echo "Rollback snapshot is not a readable tar archive; refusing to apply the delta. The RangerDanger projects are stopped; restore them from the rollback procedure." >&2
    exit 1
}

# Update the repo, then load the changed images (if any).
tar xzf "\$DELTA_DIR/rangerdanger.tgz" -C ~
__APPLY_BACKUP_COMMANDS__
__APPLY_LOAD_COMMAND__

# Re-tag unchanged first-party images so every required :__NEW__ tag exists.
__APPLY_RETAG_COMMANDS__

# Select the new release while preserving other .env settings.
NEW_VERSION=__NEW__ awk '
  BEGIN { version = ENVIRON["NEW_VERSION"]; replaced = 0 }
  /^VERSION=/ {
    if (!replaced) print "VERSION=" version
    replaced = 1
    next
  }
  { print }
  END { if (!replaced) print "VERSION=" version }
' .env > .env.delta.tmp && mv .env.delta.tmp .env

# Start the platform from local images without contacting GHCR, then have
# its backend select the recorded package through the normal range API.
start_platform_and_selected_range
\`\`\`

**ARM64 Linux only:** OpenPLC needs amd64 emulation. When changed images
are included, \`delta-arm64.tar\` also ships \`__BINFMT_IMAGE__\`; if
OpenPLC isn't running after the restart (\`docker ps | grep openplc\`),
register it once with
\`docker run --privileged --rm __BINFMT_IMAGE__ --install amd64\`.
(setup.sh does this automatically on a fresh install; the registration
does not persist across a host reboot.) A repo-only delta has no image
archives, so it cannot supply the binfmt image. Make sure that image is
already present before applying a repo-only delta offline.

If \`docker load\` fails with "no space left on device", free space
without removing the prior \`__SINCE__\` image tags or parked mutable-image
tags named \`:before-__NEW__\`; removing an old or parked tag forfeits rollback
for that image. If any apply step after the stack is stopped fails, do not
try to start a partially updated tree: keep the snapshot and old tags, then
follow the \`Rollback\` section below.

## Rollback

The apply recipe saves the complete pre-upgrade \`~/rangerdanger\` tree
beside the install as \`../rangerdanger.before-__NEW__.tar.gz\`. That snapshot
includes all files and directories in the install tree: \`.env\`, Compose
files, lab definitions, policy files, local edits, and all of \`./data/\`
(including captures, Kali home, and simulator state; nothing in \`./data/\`
is excluded). Docker images are not part of it. It can be large and grows
with lab state.
The retained \`__SINCE__\` image tags and the parked mutable-image tags named
\`:before-__NEW__\` are reused, so rollback needs no network and no second
bundle. Keep the snapshot, retained old image tags, and parked tags until
the rollback window closes:

\`\`\`sh
set -e
cd ~/rangerdanger
__COMPOSE_DOWN_FUNCTION__
__START_PLATFORM_FUNCTION__
test -f "../rangerdanger.before-__NEW__.tar.gz" || {
    echo "Rollback snapshot not found beside ~/rangerdanger." >&2
    exit 1
}
tar tzf "../rangerdanger.before-__NEW__.tar.gz" >/dev/null || {
    echo "Rollback snapshot is not readable; leaving the current install untouched." >&2
    exit 1
}
compose_down_project rangerdanger
compose_down_project rangerdanger-platform
cd ..
rm -rf rangerdanger
tar xzf "rangerdanger.before-__NEW__.tar.gz"
cd rangerdanger
__ROLLBACK_RESTORE_COMMANDS__
start_platform_and_selected_range
\`\`\`
README

python3 - "$README_TEMPLATE" "$OUT/DELTA-README.md" "$SINCE" "$NEW" \
    "$APPLY_TABLE" "$KERNEL_README_ROW" "$UNCHANGED_SECTION" \
    "$APPLY_BACKUP_COMMANDS" "$APPLY_LOAD_COMMAND" "$APPLY_RETAG_COMMANDS" \
    "$BINFMT_IMAGE" "$ROLLBACK_RESTORE_COMMANDS" \
    "$COMPOSE_DOWN_FUNCTION" "$START_PLATFORM_FUNCTION" \
    <<'PY' || die "Could not render DELTA-README.md"
from datetime import datetime, timezone
from pathlib import Path
import sys

(
    template_path, output_path, since, new, apply_table, kernel_row,
    unchanged_section, backup_commands, load_command, retag_commands,
    binfmt_image, rollback_commands, compose_down_function,
    start_platform_function,
) = sys.argv[1:]
template = Path(template_path).read_text(encoding="utf-8")
template = template.replace(r"\$", "$").replace(r"\`", "`")
replacements = {
    "__STAGED_AT__": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "__SINCE__": since,
    "__NEW__": new,
    "__APPLY_TABLE__": apply_table,
    "__KERNEL_README_ROW__": kernel_row,
    "__UNCHANGED_SECTION__": unchanged_section,
    "__APPLY_BACKUP_COMMANDS__": backup_commands,
    "__APPLY_LOAD_COMMAND__": load_command,
    "__APPLY_RETAG_COMMANDS__": retag_commands,
    "__BINFMT_IMAGE__": binfmt_image,
    "__ROLLBACK_RESTORE_COMMANDS__": rollback_commands,
    "__COMPOSE_DOWN_FUNCTION__": compose_down_function + "\n",
    "__START_PLATFORM_FUNCTION__": start_platform_function + "\n",
}
for token, value in replacements.items():
    template = template.replace(token, value)
Path(output_path).write_text(template, encoding="utf-8")
PY
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
