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
#
# See docs/workshop-ssd.md for the full operator runbook including
# when to use this vs full stage-ssd.sh.

set -euo pipefail

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
BINFMT_IMAGE="tonistiigi/binfmt:latest"

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
    CANDIDATE_IMAGES=$(echo "$ALL_IMAGES" | grep -E 'ghcr\.io/tonylturner/' || true)
else
    CANDIDATE_IMAGES="$ALL_IMAGES"
fi

# Substitute :latest with the requested version for first-party images.
# Upstream images keep their pinned tag/digest as-is.
resolve_version() {
    local images="$1" version="$2"
    echo "$images" | sed -E "s|^(ghcr\.io/tonylturner/(rangerdanger-[a-z0-9-]+|containd)):latest\$|\\1:$version|" \
                  | sed -E "s|^(ghcr\.io/tonylturner/(rangerdanger-[a-z0-9-]+|containd)):[^@]+\$|\\1:$version|"
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

INCLUDE_SET=" $(echo "${INCLUDE_LIST//,/ }") "

banner "Comparing $SINCE -> $NEW across $(echo "$CANDIDATE_IMAGES" | wc -l | tr -d ' ') candidate image(s)"

CHANGED=()
UNCHANGED=()
FORCED=()
MISSING_SINCE=()

# Read the resolved lists in lockstep. We want the new-ref to save,
# but compare since-ref vs new-ref to decide whether to include.
mapfile -t SINCE_ARR <<<"$SINCE_REF"
mapfile -t NEW_ARR <<<"$NEW_REF"

for i in "${!NEW_ARR[@]}"; do
    new="${NEW_ARR[$i]}"
    since="${SINCE_ARR[$i]}"
    [ -z "$new" ] && continue

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

    new_digest=$(remote_digest "$new" || echo "")
    since_digest=$(remote_digest "$since" || echo "")

    if [ -z "$new_digest" ]; then
        warn "  $short: couldn't read digest for $new - including in delta to be safe"
        CHANGED+=("$new")
        continue
    fi
    if [ -z "$since_digest" ]; then
        warn "  $short: couldn't read $since (not pulled?) - including in delta to be safe"
        MISSING_SINCE+=("$short")
        CHANGED+=("$new")
        continue
    fi

    if [ "$new_digest" = "$since_digest" ]; then
        UNCHANGED+=("$short")
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
    local digest
    digest=$(docker buildx imagetools inspect "$img" \
        --format '{{range .Manifest.Manifests}}{{if and (eq .Platform.OS "linux") (eq .Platform.Architecture "'"$arch"'")}}{{.Digest}}{{end}}{{end}}' \
        2>/dev/null | head -1)
    if [ -n "$digest" ] && [ "$digest" != "<no value>" ]; then
        local base="${img%@*}"
        local repo="${base%:*}"
        printf '%s@%s\n' "$repo" "$digest"
        return 0
    fi
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

    local pulled_tags=""
    for img in "${to_stage[@]}"; do
        say "resolve $arch  $img"
        local ref
        if ! ref=$(resolve_platform_ref "$img" "$arch"); then
            case "$img" in
                *rangerdanger-openplc*)
                    # See stage-ssd.sh for rationale: openplc is amd64-only
                    # upstream and runs under Rosetta on Apple Silicon.
                    # Cross-include the amd64 image in arm64 bundle.
                    ref=$(resolve_platform_ref "$img" "amd64") \
                        || die "openplc: amd64 fallback resolution also failed"
                    say "    cross-arch (amd64 image, runs on arm64 via Rosetta): $ref"
                    ;;
                *)
                    say "    skip - not available for linux/$arch"
                    continue
                    ;;
            esac
        elif [ "$ref" != "$img" ]; then
            say "    -> $ref"
        fi
        docker pull --quiet "$ref" >/dev/null \
            || die "pull failed for $ref on $arch - re-run after fixing the upstream issue."
        local target_tag="${img%@*}"
        if [ "$ref" != "$target_tag" ]; then
            docker tag "$ref" "$target_tag" \
                || die "docker tag $ref -> $target_tag failed"
        fi
        pulled_tags="$pulled_tags $target_tag"
    done

    if [ -z "$pulled_tags" ]; then
        say "Nothing to save for $arch (no images compatible with this arch)"
        return 0
    fi

    say "save $arch -> $tarball"
    # shellcheck disable=SC2086
    docker save -o "$tarball" $pulled_tags \
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
git -C "$ROOT_DIR" archive --format=tar HEAD | gzip > "$OUT/rangerdanger.tgz"
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

# Build the per-image apply summary for the README.
APPLY_SERVICES=""
APPLY_TABLE=""
for img in "${CHANGED[@]}"; do
    short=$(echo "$img" | sed -E 's|.*/||; s|:.*||')
    # Map image name back to compose service name. For rangerdanger-*,
    # the service name is the same as the bit after rangerdanger-, with
    # underscores: rangerdanger-rtac-sim -> rtac_sim, etc.
    svc=$(echo "$short" | sed -E 's|^rangerdanger-||; s|-|_|g')
    APPLY_SERVICES="$APPLY_SERVICES $svc"
    APPLY_TABLE="$APPLY_TABLE| \`$short\` | \`$svc\` |
"
done

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
# 1. update the repo (always)
docker compose down
tar xzf <delta-dir>/rangerdanger.tgz -C ~/rangerdanger-new
cd ~/rangerdanger-new

# 2. load only the changed images for this host's arch
ARCH=\$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
docker load -i <delta-dir>/delta-\$ARCH.tar

# 3. restart with the offline overlay so docker compose doesn't try to pull
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
\`\`\`

\`docker compose up -d\` (no service list) is safe - compose only
recreates containers whose image digest changed.

**ARM64 Linux only:** OpenPLC needs amd64 emulation. \`delta-arm64.tar\`
ships \`tonistiigi/binfmt\` for this; if OpenPLC isn't running after the
restart (\`docker ps | grep openplc\`), register it once with
\`docker run --privileged --rm tonistiigi/binfmt --install amd64\`.
(setup.sh does this automatically on a fresh install; the registration
does not persist across a host reboot.)

If \`docker load\` fails with "no space left on device", run
\`docker system prune -a\` to clear images not in the current
compose, then re-load.

## Rollback

Pull the prior \`$SINCE\` images directly from GHCR (assumes student
has internet, or the prior full SSD bundle):

\`\`\`sh
VERSION=$SINCE docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
\`\`\`
EOF
say "wrote $OUT/DELTA-README.md"

banner "Done"
echo
echo "  Output dir:       $OUT"
echo "  Changed images:   ${#CHANGED[@]}"
echo "  Unchanged:        ${#UNCHANGED[@]}"
echo
ls -lh "$OUT" | awk 'NR>1 {printf "  %-30s %s\n", $9, $5}'
echo
echo "  Total: $(du -sh "$OUT" | awk '{print $1}')"
echo
echo "  Distribute the four files in $OUT to students. The README in"
echo "  that directory contains the exact apply commands."
