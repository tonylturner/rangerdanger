#!/usr/bin/env bash
# Each command needs one new peer-attributed write, not just an API result.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
OPERATOR=$(cat "$T_FIXTURES/operator-gi-untimed.json")
POSTS=0
AUDIT_EDIT='.'
RESULT_EDIT='.'
eu_wait_valid_device() { :; }
eu_get_operator() { API_BODY="$OPERATOR"; }
eu_management() {
  # shellcheck disable=SC2034 # consumed by sourced gate code
  HTTP_STATUS=200
  if [ "$3" = '/api/audit?after=0' ]; then
    API_BODY='{"generation":42,"latest_seq":20,"oldest_seq":1,"entries":[]}'
  else
    API_BODY=$(jq -cn --arg at "$(eu_utc)" '{generation:42,oldest_seq:1,latest_seq:21,entries:[
      {seq:21,at:$at,operation:"open",result:"accepted",context:{protocol:"modbus",peer:"10.30.30.20:1234"}}]}' | jq -c "$AUDIT_EDIT")
  fi
}
eu_api_post() {
  POSTS=$((POSTS + 1))
  # shellcheck disable=SC2034 # consumed by sourced gate code
  HTTP_STATUS=200
  API_BODY=$(jq -cn --arg at "$(eu_utc)" '{id:"cmd-1",point:"rmu-1-command",operation:"open",result:"completed",reason:null,
    selected_at:$at,executed_at:$at,con_at:$at,term_at:$at,feedback:{ioa:1101,value:"open",at:$at}}' | jq -c "$RESULT_EDIT")
}
# The real management read comes after the result. Fix the response timestamp
# strictly inside the command window rather than manufacturing a later audit.
CLOCK_COUNT=0
eu_utc() { CLOCK_COUNT=$((CLOCK_COUNT + 1)); printf '2026-10-15T12:00:00.500Z\n'; }
t_run eu_command rmu-1-command open completed
t_expect_fails 0 "completed command requires its fresh RTU write"
t_expect_eq "$POSTS" 1 "one execute POST only"
for AUDIT_EDIT in '.entries=[]' '.generation=43' '.oldest_seq=23' \
  '.entries[0].seq=20' '.entries[0].context.peer="10.10.10.50:1234"' \
  '.entries[0].context.protocol="lab"' '.entries[0].result="rejected"' \
  '.entries[0].at="2026-10-15T11:59:00Z"' '.entries += .entries'; do
  before=$POSTS
  t_run eu_command rmu-1-command open completed
  t_expect_fails 1 "missing, stale, duplicated or unattributed write fails: $AUDIT_EDIT"
  t_expect_eq "$POSTS" "$((before + 1))" "failure never retries a physical command"
done
AUDIT_EDIT='.'
RESULT_EDIT='.result="unknown"'
t_run eu_command rmu-1-command open completed
t_expect_fails 1 "unknown does not pass as a completed or failed command"
t_finish
