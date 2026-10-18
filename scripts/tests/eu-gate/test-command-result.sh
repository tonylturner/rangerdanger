#!/usr/bin/env bash
# Every command evidence assertion identifies its failed field or inequality.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
RESULT='{"id":"cmd-1","point":"rmu-1-command","operation":"open","result":"completed","selected_at":"2026-10-15T12:00:00.1Z","executed_at":"2026-10-15T12:00:00.2Z","con_at":"2026-10-15T12:00:00.3Z","feedback":{"ioa":1101,"value":"open","at":"2026-10-15T12:00:00.4Z"},"term_at":"2026-10-15T12:00:00.5Z"}'
check_result() {
  eu_command_result "$1" rmu-1-command open "${2:-completed}" \
    2026-10-15T12:00:00Z 2026-10-15T12:00:01Z 1101 '"open"'
}
mutation() {
  local body
  body=$(printf '%s' "$RESULT" | jq -c "$1")
  t_run check_result "$body" "${3:-completed}"
  t_expect_eq "$T_STATUS" 1 "rejects $1"
  t_expect_out "invalid command evidence: $2" "names the failed assertion"
}
t_run check_result "$RESULT"
t_expect_eq "$T_STATUS" 0 "ordered confirmations and feedback pass"
mutation '.point="other"' point:
mutation '.operation="close"' operation:
mutation '.result="unknown"' result:
mutation '.id=""' id:
mutation '.id=123' id:
mutation '.selected_at="2026-10-15T11:59:59.999999Z"' 'start <= selected_at:'
mutation '.executed_at="2026-10-15T12:00:00.099999Z"' 'selected_at <= executed_at:'
mutation '.con_at="2026-10-15T12:00:00.199999Z"' 'executed_at <= con_at:'
mutation '.con_at="2026-10-15T12:00:01.000001Z"' 'con_at <= end:'
mutation '.feedback.ioa=1001' feedback.ioa:
mutation '.feedback.value="closed"' feedback.value:
mutation '.feedback.at="2026-10-15T12:00:00.299999Z"' 'con_at <= feedback.at:'
mutation '.term_at="2026-10-15T12:00:00.399999Z"' 'feedback.at <= term_at:'
mutation '.term_at="2026-10-15T12:00:01.000001Z"' 'term_at <= end:'
for field in selected_at executed_at con_at term_at feedback.at; do
  mutation ".$field=null" "$field: expected UTC timestamp"
  mutation ".$field=\"not-a-dateZ\"" "$field: invalid UTC timestamp"
done
mutation '.result="rejected" | .term_at=null' 'feedback:' rejected
mutation '.result="rejected" | .feedback=null' 'term_at:' rejected
mutation '.result="unknown"' 'expected result:' unknown

# Inclusivity and precision are unchanged: equality passes, one microsecond
# outside either boundary fails, even if internal ordering is otherwise valid.
BOUNDARY=$(printf '%s' "$RESULT" | jq -c '
  .selected_at="2026-10-15T12:00:00Z" | .term_at="2026-10-15T12:00:01Z"')
t_run check_result "$BOUNDARY"
t_expect_eq "$T_STATUS" 0 "exact window endpoints remain inclusive"
REJECTED=$(printf '%s' "$RESULT" | jq -c '.result="rejected" | .feedback=null | .term_at=null')
t_run check_result "$REJECTED" rejected
t_expect_eq "$T_STATUS" 0 "ordered negative confirmation with null feedback/term passes"
t_finish
