#!/usr/bin/env bash
# Native, versioned SSD index. Validation completes before any Docker load.
image_sha256() {
    case "$(uname -s)" in
        Darwin) shasum -a 256 "$1" ;;
        Linux) sha256sum "$1" ;;
        *) die "Unsupported checksum host; use macOS or Linux."; return 1 ;;
    esac | awk '{print $1}'
}

validate_image_index() {
    local dir="$1" arch="$2" version="$3" selected="$4" required="$5"
    local prefix="${6:-image-index}" membership="${7:-required}"
    local index="$dir/$prefix-$arch.txt" line n=0 owner image path hash extra part cursor
    local chosen="" found="" actual="" expected mappings="" existing
    [ -f "$index" ] || { die "Re-stage this bundle with rangerdanger-image-index-v1; monolithic bundles are not supported ($index missing)."; return 1; }
    [ "$(LC_ALL=C tr -cd '\000' < "$index" | wc -c | tr -d ' ')" = 0 ] || { die "Malformed image index."; return 1; }
    [ "$(cat "$dir/.version")" = "$version" ] || { die "Bundle VERSION mismatch."; return 1; }
    while IFS= read -r line; do
        n=$((n + 1))
        case "$n" in
            1) [ "$line" = rangerdanger-image-index-v1 ] || { die "Bundle requires rangerdanger-image-index-v1."; return 1; }; continue ;;
            2) [ "$line" = "version$(printf '\t')$version" ] || { die "Index VERSION mismatch."; return 1; }; continue ;;
            3) [ "$line" = "arch$(printf '\t')$arch" ] || { die "Index architecture mismatch."; return 1; }; continue ;;
        esac
        IFS=$'\t' read -r owner image path hash extra <<< "$line"
        [ "$line" = "$(printf '%s\t%s\t%s\t%s' "$owner" "$image" "$path" "$hash")" ] && [ -z "$extra" ] \
            || { die "Malformed image index row."; return 1; }
        case "$owner" in platform) ;; ""|*[!a-z0-9-]*) die "Invalid index owner."; return 1 ;; esac
        case "$image" in ""|*[[:space:]]*) die "Invalid indexed image."; return 1 ;; esac
        case "$path" in ""|/*|*\\*|*:*|*'//'*) die "Unsafe index path: $path"; return 1 ;; esac
        cursor="$dir"
        while IFS= read -r part; do
            case "$part" in ""|.|..) die "Unsafe index path: $path"; return 1 ;; esac
            cursor="$cursor/$part"
            [ ! -L "$cursor" ] || { die "Symlink in index path: $path"; return 1; }
        done <<< "${path//\//$'\n'}"
        [[ "$hash" =~ ^[0-9a-f]{64}$ ]] || { die "Invalid index checksum."; return 1; }
        existing=$(printf '%s' "$mappings" | awk -F '\t' -v image="$image" -v path="$path" '$1 == image || $2 == path {print; exit}')
        [ -z "$existing" ] || [ "$existing" = "$(printf '%s\t%s\t%s' "$image" "$path" "$hash")" ] || { die "Conflicting image index mapping."; return 1; }
        mappings="$mappings$image$(printf '\t')$path$(printf '\t')$hash
"
        if [ "$owner" = platform ] || printf '%s\n' "$selected" | grep -qxF "$owner"; then
            found="$found$owner
"
            actual="$actual$image
"
            if ! printf '%s' "$chosen" | grep -qxF "$dir/$path"; then
                [ -f "$dir/$path" ] && [ "$(image_sha256 "$dir/$path")" = "$hash" ] \
                    || { die "Index checksum mismatch or missing archive: $path"; return 1; }
                chosen="$chosen$dir/$path
"
            fi
        fi
    done < "$index"
    [ "$n" -gt 3 ] && [ -z "$line" ] || { die "Truncated image index."; return 1; }
    if [ "$membership" = required ]; then
        for owner in platform $selected; do
            printf '%s' "$found" | grep -qxF "$owner" || { die "Selected package missing from bundle: $owner"; return 1; }
        done
    fi
    expected=$(printf '%s\n' "$required" | LC_ALL=C sort -u)
    actual=$(printf '%s' "$actual" | LC_ALL=C sort -u)
    # binfmt is a staging helper, not a Compose image.
    actual=$(printf '%s\n' "$actual" | grep -vxF 'tonistiigi/binfmt:qemu-v10.2.1' || true)
    [ "$actual" = "$expected" ] || { die "Indexed selected image closure differs from Compose."; return 1; }
    printf '%s' "$chosen" | LC_ALL=C sort -u
}
