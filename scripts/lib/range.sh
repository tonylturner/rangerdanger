# shellcheck shell=bash
#
# GET /api/range helpers for setup.sh and the host scripts. Source this
# file after setting API to the portal base URL (http://localhost:8088).
# The status body is one flat JSON object, so parsing needs only sed:
# no jq or python3 dependency.

# range_field <json> <key>: one top-level string field. Absent, null and
# "" all read as empty; escaped characters stay escaped.
range_field() {
    printf '%s' "$1" | sed -nE 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p'
}

# range_status: the current status body, or "unreachable".
range_status() {
    curl -fsS --max-time 10 "$API/api/range" 2>/dev/null || echo unreachable
}

# range_is_ready <package>: true when the range is that package and ready.
range_is_ready() {
    local status
    status=$(range_status)
    [ "$(range_field "$status" package)" = "$1" ] && [ "$(range_field "$status" phase)" = ready ]
}
