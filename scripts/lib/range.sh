# shellcheck shell=bash
#
# GET /api/range helpers for setup.sh and the host scripts. Source this
# file after setting API to the portal base URL (http://localhost:8088).
# Top-level status fields use sed; readiness receipts match the backend's
# compact JSON encoding so the installer does not require Python.

# range_field <json> <key>: one top-level string field. Absent, null and
# "" all read as empty; escaped characters stay escaped.
range_field() {
    printf '%s' "$1" | sed -nE 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p'
}

# range_status: the current status body, or "unreachable".
range_status() {
    curl -fsS --max-time 10 "$API/api/range" 2>/dev/null || echo unreachable
}

# Consume this status observation's receipt, not a separately fetched apply.
range_has_protection() {
    # This is not a general JSON parser: only backend-owned compact receipts
    # are accepted. TestInstallerReceiptEncoding pins their field order and
    # formatting; smokes retain the strict Python verifier.
    printf '%s' "$1" | grep -qE '"containd":\{"applyId":"([^"\\]|\\.)+","protection":"protecting"[,}]'
}

# range_is_ready <package>: true when the range is that package and ready.
range_is_ready() {
    local status
    status=$(range_status)
    [ "$(range_field "$status" package)" = "$1" ] && [ "$(range_field "$status" phase)" = ready ] \
        && range_has_protection "$status"
}
