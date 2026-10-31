#!/usr/bin/env bash
# Single-operation adapter to the existing EU lifecycle, never a second policy
# or reset sequence. The common Python worker owns ordering and finally cleanup.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=../eu-iec104-smoke.sh
. "$(dirname "${BASH_SOURCE[0]}")/../eu-iec104-smoke.sh"
TMP_DIR="$1"
mkdir -p "$TMP_DIR"
shift
case "$1" in
  reset) eu_reset_all ;;
  policy) eu_apply_named "$2" ;;
  traffic)
    eu_api_post /api/traffic/generate '{"duration_sec":30}'
    [ "$HTTP_STATUS" = 200 ]
    ;;
  management)
    eu_management "$2" GET "$3"
    [ "$HTTP_STATUS" = 200 ]
    printf '%s\n' "$API_BODY" >"$4"
    ;;
  reconnect-check)
    shape=$(eu_station_shape)
    eu_wait_for_operator "($shape) and .link.state == \"started\"
      and (.link.since | epoch) > (\"$2\" | epoch)
      and .link.gi.state == \"complete\"
      and (.link.gi.started_at | epoch) >= (.link.since | epoch)
      and all(.points[]; good)" 30 "command smoke fresh CC session and GI"
    ;;
  *) printf 'Unknown EU command-smoke operation: %s\n' "$1" >&2; exit 2 ;;
esac
