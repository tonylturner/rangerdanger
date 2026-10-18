#!/usr/bin/env bash
# G6: earlier Kali conntrack history (TIME_WAIT) must drain before the monitor
# starts; a live row fails; tuple selection requires exactly one live row.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

KALI_IP=10.10.10.50
row() {
  printf '{"proto":"tcp","src":"%s","dst":"%s","sport":%s,"dport":2404,"state":%s,"timeout":%s}' \
    "$KALI_IP" "$RTU_IP" "$1" "$2" "$3"
}
envelope() {
  local rows
  rows=$(IFS=,; printf '%s' "$*")
  printf '{"sessions":[%s],"count":%s}' "$rows" "$#"
}
TIME_WAIT=$(row 41234 '"TIME_WAIT"' 117)
ESTABLISHED=$(row 41240 '"ESTABLISHED"' 431999)

# Each read returns the next queued body; the last one repeats.
QUEUE=()
READS=0
eu_api_get() {
  local index="$READS"
  READS=$((READS + 1))
  [ "$index" -lt "${#QUEUE[@]}" ] || index=$((${#QUEUE[@]} - 1))
  # shellcheck disable=SC2034 # read by the sourced gate helpers
  HTTP_STATUS=200
  # shellcheck disable=SC2034 # read by eu_read_sessions
  API_BODY="${QUEUE[$index]}"
  case "$1" in "/api/firewall/sessions?dport=2404&src=$KALI_IP") ;; *) return 1 ;; esac
}
drain() {
  READS=0
  t_run eu_wait_session_history_drained "$KALI_IP" "$RTU_IP" 2404 "G6 test"
}

QUEUE=("$(envelope "$TIME_WAIT" "$ESTABLISHED")")
drain
t_expect_fails 1 "TIME_WAIT plus ESTABLISHED fails on the live row"
t_expect_out '"state":"ESTABLISHED"' "the live row is printed"
t_expect_eq "$READS" 1 "a live row fails at once without waiting"

QUEUE=("$(envelope "$TIME_WAIT")" "$(envelope "$TIME_WAIT")" "$(envelope)")
drain
t_expect_fails 0 "TIME_WAIT history that expires gives a clean baseline"
t_expect_out "waiting up to 122s" "the wait is bounded by the row's own timeout"
t_expect_eq "$READS" 3 "the list is polled until the history is gone"

QUEUE=("$(envelope)")
drain
t_expect_fails 0 "no Kali rows is a clean baseline"
t_expect_out "PASS G6 test no earlier" "the clean baseline is reported"

QUEUE=("$(envelope "$(row 41234 '"TIME_WAIT"' 0)")")
drain
t_expect_fails 1 "TIME_WAIT that never expires fails once its budget is spent"
t_expect_out "did not expire within 5s" "the timeout names its budget"

QUEUE=("$(envelope "$(row 41234 '"TIME_WAIT"' 400)")")
drain
t_expect_fails 1 "a closing row with an over-long timeout fails at once"
QUEUE=("$(envelope "$(row 41234 null 60)")")
drain
t_expect_fails 1 "a row without a state counts as live"
QUEUE=("$(envelope "$(printf '{"proto":"tcp","src":"%s","dst":"10.30.30.21","sport":5000,"dport":2404,"state":"ESTABLISHED","timeout":30}' "$KALI_IP")")")
drain
t_expect_fails 0 "rows to another destination do not belong to the Kali-to-RTU tuple"

QUEUE=("$(envelope)")
READS=0
t_run eu_read_sessions 2404 "$KALI_IP"
t_expect_fails 1 "an empty session list still fails where a session must exist"

t_run eu_session_tuple "$(envelope "$TIME_WAIT" "$ESTABLISHED")" "$KALI_IP" "$RTU_IP" 2404
t_expect_eq "$T_STATUS" 0 "closing history does not obscure the single live monitor tuple"
t_expect_eq "$SESSION_TUPLE" "[$ESTABLISHED]" "only the live tuple is selected for deletion"
t_run eu_session_tuple "$(envelope "$ESTABLISHED")" "$KALI_IP" "$RTU_IP" 2404
t_expect_eq "$T_STATUS" 0 "a single monitor row is the exact tuple"
for state in TIME_WAIT CLOSE CLOSE_WAIT FIN_WAIT LAST_ACK; do
  closing=$(row 41234 "\"$state\"" 60)
  t_run eu_session_tuple "$(envelope "$closing" "$ESTABLISHED")" "$KALI_IP" "$RTU_IP" 2404
  t_expect_eq "$T_STATUS" 0 "$state is history during tuple selection"
  t_expect_eq "$SESSION_TUPLE" "[$ESTABLISHED]" "$state is never selected for deletion"
  QUEUE=("$(envelope "$closing")" "$(envelope)")
  drain
  t_expect_fails 0 "$state uses the same history classification during drain"
done
t_run eu_session_tuple "$(envelope "$TIME_WAIT")" "$KALI_IP" "$RTU_IP" 2404
t_expect_eq "$T_STATUS" 1 "closing-only history cannot supply a kill target"
t_expect_eq "$SESSION_TUPLE" "" "a failed selection clears the previous tuple"
t_expect_out "found 0" "no live tuple is explicit"
t_run eu_session_tuple "$(envelope "$ESTABLISHED" "$(row 41241 '"ESTABLISHED"' 60)")" "$KALI_IP" "$RTU_IP" 2404
t_expect_eq "$T_STATUS" 1 "multiple live rows remain ambiguous"
t_expect_out "found 2" "the live match count is explicit"
t_run eu_session_tuple "$(envelope "$(row 41234 null 60)" "$ESTABLISHED")" "$KALI_IP" "$RTU_IP" 2404
t_expect_eq "$T_STATUS" 1 "an unknown state is not silently discarded as history"

# G11's actual RTU-to-breaker Modbus tuple, not just the G6 monitor path.
modbus=$(jq -nc --arg src "$RTU_IP" --arg dst "$BREAKER_IP" '
  {sessions:[{proto:"tcp",src:$src,dst:$dst,sport:41850,dport:502,state:"ESTABLISHED",timeout:60},
    {proto:"tcp",src:$src,dst:$dst,sport:41849,dport:502,state:"TIME_WAIT",timeout:60}],count:2}')
t_run eu_session_tuple "$modbus" "$RTU_IP" "$BREAKER_IP" 502
t_expect_eq "$T_STATUS" 0 "G11 accepts one live Modbus tuple alongside TIME_WAIT"
t_run jq -e 'length == 1 and .[0].sport == 41850 and .[0].state == "ESTABLISHED"' <<<"$SESSION_TUPLE"
t_expect_eq "$T_STATUS" 0 "G11 kill target is the live Modbus source port"

t_finish
