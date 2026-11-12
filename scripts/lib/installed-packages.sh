#!/usr/bin/env bash
# Host-owned installation selection. No Docker or lifecycle operations here.

available_packages() {
    local d
    for d in "$ROOT_DIR"/lab-definitions/packages/*; do
        [ ! -f "$d/package.yml" ] || basename "$d"
    done | LC_ALL=C sort
}

validate_package() {
    case "$1" in
        ""|*[!a-z0-9-]*) die "Invalid package id '$1'." ;;
    esac
    available_packages | grep -qxF "$1" || die "Unknown package '$1'."
}

normalize_packages() {
    local item rest="$1"
    while :; do
        item=${rest%%,*}
        validate_package "$item" || return 1
        printf '%s\n' "$item"
        case "$rest" in *,*) rest=${rest#*,} ;; *) break ;; esac
    done | LC_ALL=C sort -u
}

read_installed_packages() {
    local file="$ROOT_DIR/data/installed-packages" line previous="" n=0
    local LC_ALL=C
    local fix="Invalid $ROOT_DIR/data/installed-packages. Repair the record, or remove it and run ./setup.sh --packages <ids> (Windows: .\\setup.ps1 -Packages <ids>)."
    [ -f "$file" ] || { die "$fix"; return 1; }
    [ "$(LC_ALL=C tr -cd '\000' < "$file" | wc -c | tr -d ' ')" = 0 ] || { die "$fix"; return 1; }
    while IFS= read -r line; do
        n=$((n + 1))
        if [ "$n" -eq 1 ]; then
            [ "$line" = rangerdanger-installed-packages-v1 ] || { die "$fix"; return 1; }
        else
            case "$line" in ""|*[!a-z0-9-]*) die "$fix"; return 1 ;; esac
            available_packages | grep -qxF "$line" || { die "$fix"; return 1; }
            if [ -n "$previous" ] && [[ "$previous" > "$line" || "$previous" == "$line" ]]; then
                die "$fix"; return 1
            fi
            previous=$line
            printf '%s\n' "$line"
        fi
    done < "$file"
    # An unterminated last line leaves data in line; require canonical LF.
    [ "$n" -gt 1 ] && [ -z "$line" ] || { die "$fix"; return 1; }
}

write_installed_packages() {
    local file="$ROOT_DIR/data/installed-packages" tmp
    mkdir -p "$ROOT_DIR/data"
    tmp=$(mktemp "$file.tmp.XXXXXX") || return 1
    if ! { printf 'rangerdanger-installed-packages-v1\n'; printf '%s\n' "$SELECTED_PACKAGES"; } > "$tmp"; then
        rm -f "$tmp"; return 1
    fi
    mv -f "$tmp" "$file"
}

installed_env_value() {
    local value
    value=$(sed -n "s/^$1=//p" "$ROOT_DIR/.env")
    [ -n "$value" ] || { die "Missing $1 in $ROOT_DIR/.env; run setup first."; return 1; }
    case "$value" in *$'\n'*) die "Ambiguous $1 in $ROOT_DIR/.env; run setup first."; return 1 ;; esac
    printf '%s\n' "$value"
}
