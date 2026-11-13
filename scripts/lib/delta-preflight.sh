#!/usr/bin/env bash
# Template embedded in both stagers' generated Bash apply recipe.
delta_preflight() {
    local expected image free needed has_images
    expected='rangerdanger-installed-packages-v1
__SOURCE_PACKAGES__'
    printf '%s\n' "$expected" | cmp -s - data/installed-packages || { echo 'Delta source installed set mismatch; nothing was changed.' >&2; return 1; }
    [ "$(cat data/install-mode)" = release ] || { echo 'Delta requires a release install.' >&2; return 1; }
    while IFS= read -r image; do
        [ -z "$image" ] || docker image inspect "$image" >/dev/null || return 1
    done <<'IMAGES'
__OLD_IMAGES__
IMAGES
    ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    has_images=__HAS_IMAGES__
    export DELTA_ARCHIVES=""
    if [ "$has_images" = yes ]; then
        DELTA_ARCHIVES=$(validate_image_index "$DELTA_DIR" "$ARCH" '__NEW_VERSION__' '__TARGET_PACKAGES__' '__CHANGED_IMAGES__' delta-image-index optional) || return 1
    fi
    tar tzf "$DELTA_DIR/rangerdanger.tgz" >/dev/null || return 1
    free=$(df -Pk . | awk 'END {print $4}') || return 1
    needed=$(du -sk . "$DELTA_DIR" | awk '{n += $1} END {print n * 2}') || return 1
    [ "$free" -ge "$needed" ] || { echo 'Not enough disk space for delta and rollback snapshot.' >&2; return 1; }
}

write_delta_selection() {
    mkdir -p data
    printf 'rangerdanger-installed-packages-v1\n%s\n' '__TARGET_PACKAGES__' > data/installed-packages.tmp
    mv data/installed-packages.tmp data/installed-packages
    printf 'release\n' > data/install-mode.tmp
    mv data/install-mode.tmp data/install-mode
}
