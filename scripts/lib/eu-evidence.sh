# shellcheck shell=bash
# Attempt-local audit, command-result and historian evidence predicates.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

# eu_check_g13_audit AUDIT GENERATION BASELINE CURSOR FINAL OBSERVED [DEVICE]:
# prove the full revision interval, not just the sparsely observed revisions.
# FINAL is an independent, solved field snapshot taken before reading the audit.
# Autonomous changes beyond that fixed cut belong to the next interval.
eu_check_g13_audit() {
  local audit="$1" generation="$2" baseline="$3" cursor="$4" final="$5" observed="$6"
  local revisions truth_revisions revision label="G13${7:+ $7}"
  if printf '%s' "$audit" | jq -e --argjson generation "$generation" \
    '.generation == $generation' >/dev/null 2>&1; then
    pass "$label audit generation equals device generation $generation"
  else
    fail "$label audit generation differs from device generation $generation: $audit"
  fi
  if printf '%s' "$audit" | jq -e --argjson cursor "$cursor" '
    (.entries | type == "array" and length > 0)
    and all(.entries[]; (.seq | type == "number") and .seq > $cursor)
    and (.oldest_seq | type == "number" and . <= $cursor + 1)
  ' >/dev/null 2>&1; then
    pass "$label audit after cursor $cursor is non-empty and lost no entries"
  else
    fail "$label audit after cursor $cursor is empty, not cursor-bounded, or lost entries: $audit"
    return 1
  fi
  revisions=$(jq -nce --argjson baseline "$baseline" --argjson final "$final" --argjson observed "$observed" '
    select(($baseline | type == "number" and . == floor)
      and ($final | type == "number" and . == floor) and $final > $baseline
      and ($observed | type == "array" and length > 0)
      and all($observed[]; type == "number" and . > $baseline and . <= $final))
    | [range($baseline + 1; $final + 1)]') || {
    fail "$label invalid revision interval baseline=$baseline final=$final observed=$observed: $audit"
    return 1
  }
  truth_revisions=$(printf '%s' "$audit" | jq -c '
    [.entries[] | select(.operation != "first_solve"
        and (.before.revision | type == "number") and (.after.revision | type == "number")
        and .after.revision > .before.revision) | .after.revision] | unique') || return
  if printf '%s' "$truth_revisions" | jq -e --argjson revisions "$revisions" --argjson final "$final" '
    map(select(. <= $final)) == $revisions
  ' >/dev/null 2>&1; then
    pass "$label device audit records exactly the truth revisions $revisions"
  else
    fail "$label device audit truth revisions=$truth_revisions differ from expected=$revisions through final=$final; observed=$observed; audit=$audit"
  fi
  for revision in $(printf '%s' "$revisions" | jq -r '.[]'); do
    if printf '%s' "$audit" | jq -e --argjson revision "$revision" '
      any(.entries[]; .operation == "first_solve" and .detail.revision == $revision)
    ' >/dev/null 2>&1; then
      pass "$label solver first_solve audit covers device revision $revision"
    else
      fail "$label solver lacks first_solve audit for device revision $revision"
    fi
  done
  if printf '%s' "$audit" | jq -e --argjson baseline "$baseline" '
    all(.entries[] | select(.operation == "first_solve");
      (.detail.revision | type == "number") and .detail.revision > $baseline)
  ' >/dev/null 2>&1; then
    pass "$label every first_solve after the cursor is for a post-baseline revision"
  else
    fail "$label a first_solve after the cursor is unparsable or not after baseline revision $baseline"
  fi
}
# PLC accepted requests must be new, peer-attributed Modbus writes, never just
# a tap that happened to have the desired value before the attempted load change.
eu_plc_requests() {
  local cursor="$1" count="$2" generation="$3" gate="${4:-G16}"
  eu_management oltc-1 GET "/api/audit?after=$cursor" || return
  if printf '%s' "$API_BODY" | jq -e --argjson cursor "$cursor" --argjson count "$count" --argjson gen "$generation" '
    .generation == $gen and .oldest_seq <= $cursor + 1
    and all(.entries[] | select(.seq > $cursor and .operation == "plc_request"
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.30:"))); .result == "accepted")
    and ([.entries[] | select(.seq > $cursor and .operation == "plc_request" and .result == "accepted"
      and .context.protocol == "modbus" and (.context.peer | startswith("10.30.30.30:")))] | length) == $count' >/dev/null; then
    pass "$gate exactly $count new PLC Modbus request(s) after cursor $cursor"
  else fail "$gate PLC request evidence mismatch: $API_BODY"; return 1; fi
}
eu_command_result() {
  python3 - "$@" <<'PYRESULT'
import datetime, json, sys
body, point, op, expected, start, end, ioa, target = sys.argv[1:]
def at(value, field):
    assert isinstance(value, str) and value.endswith('Z'), f'{field}: expected UTC timestamp, got {value!r}'
    try:
        return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError as error:
        raise ValueError(f'{field}: invalid UTC timestamp {value!r}') from error
try:
    b = json.loads(body)
    assert b['point'] == point, f'point: expected {point!r}, got {b["point"]!r}'
    assert b['operation'] == op, f'operation: expected {op!r}, got {b["operation"]!r}'
    assert b['result'] == expected, f'result: expected {expected!r}, got {b["result"]!r}'
    assert isinstance(b['id'], str) and b['id'], f'id: expected non-empty string, got {b["id"]!r}'
    start_at, end_at = at(start, 'start'), at(end, 'end')
    selected, executed, con = (at(b[key], key) for key in ('selected_at', 'executed_at', 'con_at'))
    assert start_at <= selected, f'start <= selected_at: {start!r} > {b["selected_at"]!r}'
    assert selected <= executed, f'selected_at <= executed_at: {b["selected_at"]!r} > {b["executed_at"]!r}'
    assert executed <= con, f'executed_at <= con_at: {b["executed_at"]!r} > {b["con_at"]!r}'
    assert con <= end_at, f'con_at <= end: {b["con_at"]!r} > {end!r}'
    if expected == 'completed':
        f = b['feedback']
        assert f['ioa'] == int(ioa), f'feedback.ioa: expected {ioa}, got {f["ioa"]!r}'
        assert f['value'] == json.loads(target), f'feedback.value: expected {target}, got {f["value"]!r}'
        feedback_at, term = at(f['at'], 'feedback.at'), at(b['term_at'], 'term_at')
        assert con <= feedback_at, f'con_at <= feedback.at: {b["con_at"]!r} > {f["at"]!r}'
        assert feedback_at <= term, f'feedback.at <= term_at: {f["at"]!r} > {b["term_at"]!r}'
        assert term <= end_at, f'term_at <= end: {b["term_at"]!r} > {end!r}'
    else:
        assert expected == 'rejected', f'expected result: must be completed or rejected, got {expected!r}'
        assert b['feedback'] is None, f'feedback: rejected command must have null feedback, got {b["feedback"]!r}'
        assert b['term_at'] is None, f'term_at: rejected command must have null term_at, got {b["term_at"]!r}'
except (AssertionError, ValueError, KeyError, TypeError) as error:
    print('invalid command evidence:', error, file=sys.stderr)
    sys.exit(1)
PYRESULT
}
eu_historian_gap() {
  printf '%s' "$1" | jq -e --arg epoch "$2" --arg pause "$3" --arg resume "$4" "$EU_JQ_DEFS"'
    .point == "feeder-current" and any(.segments[]; .cache_epoch == $epoch and (.samples | length > 0)
      and all(.samples[]; (.snapshot_seq | type == "number") and (.value | type == "number")
        and (.quality.ov | type == "boolean") and (.cc_received_at | type == "string")))
    and all(.segments[].samples[]; (.ingested_at | epoch) < ($pause | epoch) or (.ingested_at | epoch) >= ($resume | epoch))
    and any(.gaps[]; (.from | epoch) <= ($pause | epoch) and (.to | epoch) >= ($resume | epoch))' >/dev/null
}
