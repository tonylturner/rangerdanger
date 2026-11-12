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

# Read JSON without adding a developer-tool dependency to offline installs.
# Validate the whole document, not a sed match that could accept truncated JSON
# or a nested/duplicate package key. Only the top-level package is selection data.
assert_runtime_package_selected() {
    local file="$ROOT_DIR/backend/data/range.json" package
    [ -e "$file" ] || [ -L "$file" ] || return 0
    if [ ! -f "$file" ] || [ "$(LC_ALL=C tr -cd '\000' < "$file" | wc -c | tr -d ' ')" != 0 ]; then
        die "Invalid runtime record $file. Repair the runtime record before rerunning setup."
        return 1
    fi
    if ! package=$(LC_ALL=C awk '
        function fail() { exit 1 }
        function ws() { while (substr(doc, pos, 1) ~ /^[ \t\r\n]$/) pos++ }
        function utf8(c,    n, count, min, i, nextbyte) {
            n = index(bytes, c) + 127
            if (n >= 194 && n <= 223) { count = 1; min = 128; n -= 192 }
            else if (n >= 224 && n <= 239) { count = 2; min = 2048; n -= 224 }
            else if (n >= 240 && n <= 244) { count = 3; min = 65536; n -= 240 }
            else fail()
            text = text c
            for (i = 0; i < count; i++) {
                c = substr(doc, pos++, 1)
                if (c == "") fail()
                nextbyte = index(bytes, c) + 127
                if (nextbyte < 128 || nextbyte > 191) fail()
                n = n * 64 + nextbyte - 128
                text = text c
            }
            if (n < min || n > 1114111 || (n >= 55296 && n <= 57343)) fail()
        }
        function string(    c, e, h, n, i) {
            if (substr(doc, pos++, 1) != "\"") fail()
            text = ""
            while (pos <= length(doc)) {
                c = substr(doc, pos++, 1)
                if (c == "\"") return
                if (c ~ /[\001-\037]/) fail()
                if (index(bytes, c)) { utf8(c); continue }
                if (c != "\\") { text = text c; continue }
                e = substr(doc, pos++, 1)
                if (e == "u") {
                    h = substr(doc, pos, 4)
                    if (length(h) != 4 || h ~ /[^0-9a-fA-F]/) fail()
                    pos += 4
                    n = 0
                    for (i = 1; i <= 4; i++)
                        n = n * 16 + index("0123456789abcdef", tolower(substr(h, i, 1))) - 1
                    # Non-ASCII is irrelevant to package IDs; keep it distinct
                    # for duplicate-key detection without requiring Unicode awk.
                    text = text (n > 0 && n < 128 ? sprintf("%c", n) : "\\u" tolower(h))
                } else {
                    if (e !~ /^["\\\/bfnrt]$/) fail()
                    text = text (e == "\"" || e == "\\" || e == "/" ? e : "\\" e)
                }
            }
            fail()
        }
        function value(depth,    c, id, key) {
            if (depth > 64) fail()
            ws()
            c = substr(doc, pos, 1)
            kind = c
            text = ""
            if (c == "\"") { string(); return }
            if (c == "{" || c == "[") {
                id = ++objects
                pos++
                ws()
                if (substr(doc, pos, 1) == (c == "{" ? "}" : "]")) pos++
                else while (1) {
                    if (c == "{") {
                        string()
                        key = text
                        if ((id SUBSEP key) in keys) fail()
                        keys[id, key] = 1
                        ws()
                        if (substr(doc, pos++, 1) != ":") fail()
                    }
                    value(depth + 1)
                    if (depth == 0 && c == "{" && key == "package") {
                        if (kind != "\"") fail()
                        package = text
                    }
                    ws()
                    if (substr(doc, pos, 1) == (c == "{" ? "}" : "]")) { pos++; break }
                    if (substr(doc, pos++, 1) != ",") fail()
                    ws()
                }
                kind = c
                return
            }
            if (match(substr(doc, pos), /^(true|false|null)/)) {
                pos += RLENGTH
                return
            }
            if (match(substr(doc, pos), /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?/)) {
                pos += RLENGTH
                return
            }
            fail()
        }
        BEGIN { for (i = 128; i <= 255; i++) bytes = bytes sprintf("%c", i) }
        { doc = doc $0 "\n" }
        END {
            pos = 1
            value(0)
            ws()
            if (kind != "{" || pos <= length(doc) || package !~ /^[a-z0-9-]+$/) exit 1
            print package
        }
    ' "$file"); then
        die "Invalid runtime record $file. Repair the runtime record before rerunning setup."
        return 1
    fi
    printf '%s\n' "$SELECTED_PACKAGES" | grep -qxF "$package" || {
        die "The range runtime record names $package, which --packages drops. Switch the range to a selected package first (the range switcher, or POST /api/range), then re-run."
        return 1
    }
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
