#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called indirectly by sourced gates
# G22 remote OPEN must not require a protection trip; G24 probes fixed-address
# vendor services without host discovery and distinguishes open from filtered.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OPEN_FIELD='{"devices":{"breaker-1":{"position":"open","protection_tripped":false,"revision":36}},"process":{"truth_stale":false,"solved_at":"2026-10-26T12:00:02Z","buses":{"s1":{"live":false}},"devices":{"feeder-breaker":{"delivered":true,"solved_revision":36}}}}'
RESTORED_FIELD='{"devices":{"breaker-1":{"position":"closed","protection_tripped":false,"revision":37}},"process":{"truth_stale":false,"buses":{"s1":{"live":true}},"devices":{"feeder-breaker":{"delivered":true,"solved_revision":37}}}}'
BASELINE_FIELD=$(printf '%s' "$RESTORED_FIELD" | jq -c \
  '.devices["breaker-1"].revision = 35 | .process.devices["feeder-breaker"].solved_revision = 35')
FIELD_WAITS=0
ATTACK_PREDICATE=""
DENIED_EXIT=2
DENIED_HTTP=200
DENIED_RESPONSE=""
UNCHANGED_CALLS=0
RECONNECT_CALLS=0
RECONNECT_FAIL_AT=0
EVENTS=""
READY_EXPR=""
G22_STATE=""
G22_JOURNAL=""
G22_JOURNAL_CALLS=0
G22_CLOCK=0
eu_lab_step() { EVENTS="$EVENTS step:$2"; }
eu_lab_exec() { API_BODY='{"exit_code":0,"stdout":"","stderr":""}'; }
# Capture and evidence have dedicated real-decoder/reducer tests. These stubs
# isolate the original G22/G24 assertion tests and must never reach Docker.
eu_window_start() { :; }
eu_window_finish() { :; }
eu_g22_capture_chain() { :; }
# shellcheck disable=SC2034 # consumed by the sourced G22 walk
eu_lab_g22_event() { G22_TIME="2026-10-26T12:00:01Z"; }
eu_lab_g22_write_evidence() { :; }
eu_save_evidence() { :; }
eu_api_post() {
  case "$1" in
    /api/workshop/nodes/*) EVENTS="$EVENTS deny" ;;
  esac
  # shellcheck disable=SC2034 # consumed by eu_lab_exec_denied
  HTTP_STATUS="$DENIED_HTTP"
  if [ -n "$DENIED_RESPONSE" ]; then
    API_BODY="$DENIED_RESPONSE"
  else
    API_BODY=$(jq -nc --argjson code "$DENIED_EXIT" \
      '{exit_code:$code,stdout:"",stderr:"connection failed"}')
  fi
}
eu_get_field() {
  API_BODY='{"devices":{"breaker-1":{"revision":37}}}'
}
eu_assert_unchanged_breaker() {
  UNCHANGED_CALLS=$((UNCHANGED_CALLS + 1))
  EVENTS="$EVENTS unchanged"
}
eu_wait_for_operator() {
  READY_EXPR="$1"
  WAIT_BODY="$G22_STATE"
}
eu_api_get() {
  EVENTS="$EVENTS get:$1"
  case "$1" in
    /api/operator/state) API_BODY="$G22_STATE" ;;
    /api/operator/journal?after=0)
      G22_JOURNAL_CALLS=$((G22_JOURNAL_CALLS + 1))
      API_BODY="$G22_JOURNAL"
      EVENTS="$EVENTS ready"
      ;;
    *) return 1 ;;
  esac
}
eu_now_ms() {
  G22_CLOCK=$((G22_CLOCK + 1))
  printf '%s\n' "$G22_CLOCK"
}
# Reconnect admission/receipt boundaries have their own lesson tests.
eu_lab_reconnect_cc() {
  RECONNECT_CALLS=$((RECONNECT_CALLS + 1))
  EVENTS="$EVENTS reconnect"
  [ "$RECONNECT_FAIL_AT" -eq 0 ] || [ "$RECONNECT_CALLS" -ne "$RECONNECT_FAIL_AT" ]
}
eu_wait_for_field() {
  local field
  FIELD_WAITS=$((FIELD_WAITS + 1))
  case "$FIELD_WAITS" in
    1) field="$BASELINE_FIELD" ;;
    2)
      ATTACK_PREDICATE="$1"
      field="$OPEN_FIELD"
      ;;
    3) field="$RESTORED_FIELD" ;;
    *)
      fail "unexpected G22 field wait $FIELD_WAITS"
      return 1
      ;;
  esac
  # shellcheck disable=SC2034 # consumed by the sourced G22 walk
  WAIT_BODY="$field"
  if ! printf '%s' "$field" | jq -e "$EU_JQ_DEFS $1" >/dev/null; then
    fail "G22 field assertion rejected its expected state"
    return 1
  fi
}

# The wait helper uses current operator state; the independent journal fixture
# deliberately stamps gi_started ten microseconds after state gi.started_at.
NOW="2026-10-26T12:00:03Z"
GI="2026-10-26T12:00:00Z"
G22_STATE=$(jq -cn --slurpfile inventory "$PACKAGE/protocols/points.json" \
  --arg now "$NOW" --arg gi "$GI" '
  {schema:3,cache_epoch:"cc-g22",snapshot_seq:44,rendered_at:$now,
   link:{state:"started",since:"2026-10-26T11:59:59Z",connection_seq:2,
     gi:{state:"complete",started_at:$gi,last_complete_at:"2026-10-26T12:00:01Z",
       next_due_at:"2026-10-26T12:15:00Z",trigger:"connect"}},
   points:[$inventory[0].points[] as $p |
     {id:$p.id,ioa:$p.ioa,kind:$p.kind,device:$p.source.device,
      limits:($p.limits // null),asdu:(if $p.kind == "float" then "M_ME_NC_1"
        elif $p.kind == "step-position" then "M_ST_NA_1"
        elif $p.kind == "double-point" then "M_DP_NA_1" else "M_SP_NA_1" end),
      reporting:(if $p.kind == "float" then {cyclic_ms:10000,deadband:0.1} else null end),
      value:(if $p.kind == "float" then 20.0
        elif $p.kind == "step-position" then {value:0,transient:false}
        elif $p.kind == "double-point" then "closed" else true end),
      quality:{iv:($p.ioa == 2301),nt:false,sb:false,bl:false,
        ov:(if $p.kind == "float" or $p.kind == "step-position" then false else null end)},
      stale_reason:(if $p.ioa == 2301 then "measurement_invalid" else null end),
      source_time:null,received_at:"2026-10-26T12:00:02Z",cot:3}]}')
G22_JOURNAL=$(jq -cn '
  {cache_epoch:"cc-g22",oldest_seq:1,latest_seq:2,
   entries:([{seq:1,at:"2026-10-26T12:00:00.000010Z",kind:"gi_started",detail:{connection_seq:2}},
     {seq:2,at:"2026-10-26T12:00:01Z",kind:"gi_complete",detail:{connection_seq:2}}])}')
G22_INITIAL_STATE="$G22_STATE"
G22_INITIAL_JOURNAL="$G22_JOURNAL"
G22_GOOD_STATE=$(printf '%s' "$G22_INITIAL_STATE" | jq -c '
  (.points[] | select(.ioa == 2301)) |=
    (.quality.iv = false | .stale_reason = null)')

for DENIED_EXIT in 2 1; do
  FIELD_WAITS=0
  UNCHANGED_CALLS=0
  RECONNECT_CALLS=0
  EVENTS=""
  READY_EXPR=""
  t_run eu_lab_hardening_walk eu-hardening-configurations
  t_expect_eq "$T_STATUS" 0 "G22 accepts hardened retest denial with exit $DENIED_EXIT"
  t_expect_eq "$FIELD_WAITS" 3 "G22 adds a baseline and leaves its separate restore assertion in place"
  t_expect_eq "$UNCHANGED_CALLS" 1 "G22 walk still proves the breaker was unchanged"
  t_expect_eq "$RECONNECT_CALLS" 2 "G22 walk verifies the CC before and after the hardened retest"
  t_expect_no_out "protection_tripped" "G22 open assertion does not constrain protection state"

  UNCHANGED_CALLS=0
  RECONNECT_CALLS=0
  EVENTS=""
  READY_EXPR=""
  t_run eu_lab_hardening_prepare eu-hardening-configurations
  t_expect_eq "$T_STATUS" 0 "G22 prepare accepts hardened retest denial with exit $DENIED_EXIT"
  t_expect_eq "$UNCHANGED_CALLS" 1 "G22 prepare still proves the breaker was unchanged"
  t_expect_eq "$RECONNECT_CALLS" 2 "G22 prepare reconnects both before attack and before validation"
  case " $EVENTS " in
    *" deny unchanged reconnect step:validate get:/api/operator/state "*) t_ok "G22 prepare waits for delivered CC evidence after Validate" ;;
    *) t_not_ok "G22 prepare refreshes delivered CC evidence after the denied retest: $EVENTS" ;;
  esac
done

if [ -z "$G22_STATE" ] || [ -z "$G22_JOURNAL" ]; then
  t_not_ok "G22 CC readiness fixtures are valid JSON"
else
  t_ok "G22 CC readiness fixtures are valid JSON"
fi
t_run jq -e --arg gi "$GI" '
  .entries | map(select(.kind == "gi_started"))[0].at as $journal_gi
  | $journal_gi != $gi and $journal_gi == "2026-10-26T12:00:00.000010Z"
' <<<"$G22_JOURNAL"
t_expect_eq "$T_STATUS" 0 "G22 fixture separates journal and state GI timestamps by ten microseconds"
G22_CLOCK=0
G22_STATE="$G22_GOOD_STATE"
G22_JOURNAL_CALLS=0
t_run eu_lab_wait_cc_delivered 1 "G22 healthy CC wait without point_good" true
t_expect_eq "$T_STATUS" 0 "fresh GOOD observations pass with a microsecond-offset journal timestamp"
t_expect_eq "$G22_JOURNAL_CALLS" 0 "delivered CC wait does not fetch the journal"
G22_STATE="$G22_INITIAL_STATE"
G22_CLOCK=0
t_run eu_lab_wait_cc_delivered 1 "G22 measurement-invalid CC wait" true
t_expect_eq "$T_STATUS" 0 "fresh IV measurement_invalid observations pass"
G22_CLOCK=0
t_run eu_lab_wait_good_cc
t_expect_eq "$T_STATUS" 0 "shared student CC wait accepts delivered measurement_invalid"
READY_EXPR=$(eu_lab_cc_delivery_predicate "$(eu_station_shape)" \
  'point("plc-1-input-valid").value == true')
T_OUT="$READY_EXPR"
t_expect_out 'point("plc-1-input-valid").value == true' \
  "G22 readiness retains the independent PLC input-valid requirement"
for quality in nt sb bl ov; do
  G22_STATE=$(printf '%s' "$G22_STATE" | jq -c --arg quality "$quality" '
    (.points[] | select(.ioa == 2301)).quality[$quality] = true')
  G22_CLOCK=0
  t_run eu_lab_wait_cc_delivered 0 "G22 invalid quality wait" true
  t_expect_eq "$T_STATUS" 1 "IV measurement_invalid with $quality is rejected"
  G22_STATE=$(printf '%s' "$G22_STATE" | jq -c --arg quality "$quality" '
    (.points[] | select(.ioa == 2301)).quality[$quality] = false')
done
G22_STATE=$(printf '%s' "$G22_STATE" | jq -c '
  (.points[] | select(.ioa == 2301)) |=
    (.quality.iv = true | .stale_reason = null)')
G22_CLOCK=0
t_run eu_lab_wait_cc_delivered 0 "G22 IV without measurement-invalid reason wait" true
t_expect_eq "$T_STATUS" 1 "IV without measurement_invalid is rejected"
G22_STATE="$G22_INITIAL_STATE"
G22_STATE=$(printf '%s' "$G22_STATE" | jq -c '
  (.points[] | select(.ioa == 2301)) |=
    (.quality.nt = true | .stale_reason = "link_lost")')
G22_CLOCK=0
t_run eu_lab_wait_cc_delivered 0 "G22 link-lost wait" true
t_expect_eq "$T_STATUS" 1 "link_lost remains a failure"
G22_STATE=$(printf '%s' "$G22_STATE" | jq -c '
  (.points[] | select(.ioa == 2301)) |=
    (.quality.nt = false | .stale_reason = "measurement_invalid")')
G22_STATE=$(printf '%s' "$G22_STATE" | jq -c '
  (.points[] | select(.ioa == 2301)).received_at = "2026-10-26T11:59:59Z"')
G22_CLOCK=0
t_run eu_lab_wait_cc_delivered 0 "G22 stale receipt wait" true
t_expect_eq "$T_STATUS" 1 "measurement_invalid with a pre-GI receipt is rejected"
G22_STATE="$G22_INITIAL_STATE"
G22_CLOCK=0
G22_STATE=$(printf '%s' "$G22_STATE" | jq -c \
  '.points |= map(select(.ioa != 2301))')
t_run eu_lab_wait_cc_delivered 0 "G22 missing station point wait" true
t_expect_eq "$T_STATUS" 1 "a missing station point fails"
G22_STATE="$G22_INITIAL_STATE"
G22_JOURNAL="$G22_INITIAL_JOURNAL"

DENIED_EXIT=0
FIELD_WAITS=0
UNCHANGED_CALLS=0
RECONNECT_CALLS=0
EVENTS=""
READY_EXPR=""
t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "G22 rejects a hardened retest that exits successfully"
t_expect_fails 1 "G22 reports a successful retest as not denied"
t_expect_eq "$UNCHANGED_CALLS" 0 "G22 does not claim unchanged state after an allowed retest"
case " $EVENTS " in
  *" deny unchanged reconnect step:validate "*) t_not_ok "G22 walk does not refresh and validate an allowed retest" ;;
  *) t_ok "G22 walk does not refresh and validate an allowed retest" ;;
esac
t_run eu_lab_hardening_prepare eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "G22 prepare rejects a hardened retest that exits successfully"
t_expect_fails 1 "G22 prepare reports a successful retest as not denied"

DENIED_EXIT=2
RECONNECT_CALLS=0
RECONNECT_FAIL_AT=2
EVENTS=""
READY_EXPR=""
t_run eu_lab_hardening_prepare eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "G22 prepare fails closed when the post-retest reconnect fails"
case " $EVENTS " in
  *" deny unchanged reconnect "*) t_expect_no_out "step:validate" "G22 prepare does not validate after a failed post-retest reconnect" ;;
  *) t_not_ok "G22 prepare reached its final reconnect: $EVENTS" ;;
esac
RECONNECT_FAIL_AT=0

DENIED_EXIT=2
DENIED_RESPONSE='{"exit_code":2,"stdout":7,"stderr":""}'
t_run eu_lab_exec_denied kali-1 'iec104cmd target'
t_expect_eq "$T_STATUS" 1 "G22 denied command requires string stdout and stderr"
t_expect_fails 1 "G22 malformed command response is reported"
DENIED_RESPONSE='{"stdout":"","stderr":""}'
t_run eu_lab_exec_denied kali-1 'iec104cmd target'
t_expect_eq "$T_STATUS" 1 "G22 denied command requires a numeric exit code"
DENIED_HTTP=503
DENIED_RESPONSE='{"exit_code":2,"stdout":"","stderr":""}'
t_run eu_lab_exec_denied kali-1 'iec104cmd target'
t_expect_eq "$T_STATUS" 1 "G22 denied command requires HTTP 200"
DENIED_HTTP=200
DENIED_RESPONSE=""

STALE_FIELD=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.process.devices["feeder-breaker"].solved_revision = 35')
t_run jq -e "$EU_JQ_DEFS $ATTACK_PREDICATE" <<<"$STALE_FIELD"
t_expect_eq "$T_STATUS" 1 "G22 rejects an undelivered stale solver revision"
UNDELIVERED_FIELD=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.process.devices["feeder-breaker"].delivered = false')
t_run jq -e "$EU_JQ_DEFS $ATTACK_PREDICATE" <<<"$UNDELIVERED_FIELD"
t_expect_eq "$T_STATUS" 1 "G22 requires the solver result to be delivered"
POSITION_CLOSED=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.devices["breaker-1"].position = "closed"')
t_run jq -e "$EU_JQ_DEFS $ATTACK_PREDICATE" <<<"$POSITION_CLOSED"
t_expect_eq "$T_STATUS" 1 "G22 requires the breaker to be open"
SECTION_LIVE=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.process.buses.s1.live = true')
t_run jq -e "$EU_JQ_DEFS $ATTACK_PREDICATE" <<<"$SECTION_LIVE"
t_expect_eq "$T_STATUS" 1 "G22 requires the section to be out"

NMAP_STDOUT=""
NMAP_EXEC_STATUS=0
NMAP_COMMAND=""
eu_lab_exec() {
  NMAP_COMMAND="$2"
  [ "$NMAP_EXEC_STATUS" -eq 0 ] || return "$NMAP_EXEC_STATUS"
  # shellcheck disable=SC2034 # consumed by eu_lab_nmap_vendor after the stub returns
  API_BODY=$(jq -nc --arg stdout "$NMAP_STDOUT" \
    '{exit_code:0,stdout:$stdout,stderr:""}')
}

NMAP_STDOUT='3389/tcp open  ms-wbt-server
5900/tcp open  vnc'
t_run eu_lab_nmap_vendor open
t_expect_eq "$T_STATUS" 0 "G24 accepts both open service ports"
t_expect_eq "$NMAP_COMMAND" \
  'nmap -Pn -p 3389,5900 10.20.20.10 --max-retries 1 -T4' \
  "G24 disables discovery on the open probe"

NMAP_STDOUT='3389/tcp filtered  ms-wbt-server
5900/tcp filtered  vnc'
t_run eu_lab_nmap_vendor filtered
t_expect_eq "$T_STATUS" 0 "G24 accepts both filtered service ports"
t_expect_eq "$NMAP_COMMAND" \
  'nmap -Pn -p 3389,5900 10.20.20.10 --max-retries 1 -T4' \
  "G24 disables discovery on the filtered probe"

NMAP_STDOUT='3389/tcp open  ms-wbt-server
5900/tcp filtered  vnc'
t_run eu_lab_nmap_vendor open
t_expect_eq "$T_STATUS" 1 "G24 open expectation rejects a filtered port"
t_expect_fails 1 "G24 reports an open-versus-filtered mismatch"
t_run eu_lab_nmap_vendor filtered
t_expect_eq "$T_STATUS" 1 "G24 filtered expectation rejects an open port"
t_expect_fails 1 "G24 reports a filtered-versus-open mismatch"

NMAP_EXEC_STATUS=7
t_run eu_lab_nmap_vendor open
t_expect_eq "$T_STATUS" 7 "G24 propagates an explicit probe execution failure"
t_expect_eq "$NMAP_COMMAND" \
  'nmap -Pn -p 3389,5900 10.20.20.10 --max-retries 1 -T4' \
  "G24 still uses discovery suppression on execution failure"

# G24's hardened retest is defined by its audit window and held positions,
# not by stable simulator revision counters.
G24_FIELD_CALLS=0
G24_AUDIT_ZERO_CALLS=0
G24_AUDIT_WINDOW_CALLS=0
G24_EVENTS=""
G24_RETEST_EXIT=1
G24_RETEST_AUDIT='{"entries":[
  {"seq":9,"result":"rejected","context":{"peer":"10.20.20.10:41111"}},
  {"seq":10,"result":"accepted","context":{"peer":"10.30.30.20:502"}}
]}'
G24_RMU1_POSITION=closed
G24_RMU2_POSITION=open
eu_lab_step() { :; }
eu_lab_nmap_vendor() { :; }
eu_wait_for_field() { :; }
eu_lab_exec() {
  local expected="${3:-0}" exit_code=0
  G24_EVENTS="${G24_EVENTS} exec:$2"
  if [ "$expected" = 1 ]; then
    exit_code="$G24_RETEST_EXIT"
  fi
  API_BODY=$(jq -nc --argjson code "$exit_code" '{exit_code:$code,stdout:"",stderr:""}')
  [ "$exit_code" = "$expected" ]
}
eu_management() {
  local path="$3"
  G24_EVENTS="${G24_EVENTS} audit:$path"
  case "$path" in
    /api/audit\?after=0)
      G24_AUDIT_ZERO_CALLS=$((G24_AUDIT_ZERO_CALLS + 1))
      if [ "$G24_AUDIT_ZERO_CALLS" -eq 1 ]; then
        API_BODY='{"latest_seq":7,"entries":[]}'
      else
        API_BODY='{"latest_seq":8,"entries":[]}'
      fi
      ;;
    /api/audit\?after=7)
      G24_AUDIT_WINDOW_CALLS=$((G24_AUDIT_WINDOW_CALLS + 1))
      API_BODY='{"entries":[{"seq":8,"operation":"open","result":"accepted","after":{"position":"open"},"context":{"protocol":"modbus","peer":"10.20.20.10:41322"}}]}'
      ;;
    /api/audit\?after=8)
      G24_AUDIT_WINDOW_CALLS=$((G24_AUDIT_WINDOW_CALLS + 1))
      API_BODY="$G24_RETEST_AUDIT"
      ;;
    *)
      fail "unexpected G24 audit query $path"
      return 1
      ;;
  esac
}
eu_get_field() {
  G24_FIELD_CALLS=$((G24_FIELD_CALLS + 1))
  if [ "$G24_FIELD_CALLS" -eq 1 ]; then
    API_BODY='{"devices":{"rmu-1":{"position":"open","revision":40},"rmu-2":{"position":"closed","revision":50}}}'
  else
    # shellcheck disable=SC2034 # consumed by eu_lab_vendor_walk
    API_BODY=$(jq -nc --arg one "$G24_RMU1_POSITION" --arg two "$G24_RMU2_POSITION" \
      '{devices:{"rmu-1":{"position":$one,"revision":41},"rmu-2":{"position":$two,"revision":51}}}')
  fi
}

t_run eu_lab_vendor_walk eu-vendor-rdp-compromise
t_expect_eq "$T_STATUS" 0 "G24 accepts no accepted vendor write and held positions despite changed revisions"
t_expect_eq "$G24_FIELD_CALLS" 2 "G24 checks positions after the denied retest without revision snapshots"
t_expect_eq "$G24_AUDIT_ZERO_CALLS" 2 "G24 captures an audit cursor before each vendor operation"
t_expect_eq "$G24_AUDIT_WINDOW_CALLS" 2 "G24 checks both positive and denied audit windows"
t_expect_eq "$G24_EVENTS" \
  " audit:/api/audit?after=0 exec:mbpoll -m tcp -a 1 -t 4 -0 -r 0 -1 10.40.40.21 1 audit:/api/audit?after=7 audit:/api/audit?after=0 exec:mbpoll -m tcp -a 1 -t 4 -0 -r 0 -1 10.40.40.21 1 audit:/api/audit?after=8" \
  "G24 captures the retest cursor before the denied write and queries it afterward"
t_expect_out "no accepted vendor write" "G24 pass message names the audit and position invariants"

G24_FIELD_CALLS=0
G24_AUDIT_ZERO_CALLS=0
G24_AUDIT_WINDOW_CALLS=0
G24_EVENTS=""
G24_RETEST_AUDIT='{"entries":[{"seq":9,"result":"accepted","context":{"peer":"10.20.20.10:41111"}}]}'
t_run eu_lab_vendor_walk eu-vendor-rdp-compromise
t_expect_eq "$T_STATUS" 1 "G24 rejects an accepted vendor-attributed post-cursor audit entry"
t_expect_fails 1 "G24 reports the accepted vendor-attributed retest"
t_expect_out "accepted vendor-attributed RMU audit entry" "G24 identifies the failed audit invariant"

G24_FIELD_CALLS=0
G24_AUDIT_ZERO_CALLS=0
G24_AUDIT_WINDOW_CALLS=0
G24_EVENTS=""
G24_RETEST_AUDIT='{"entries":[]}'
G24_RMU1_POSITION=open
t_run eu_lab_vendor_walk eu-vendor-rdp-compromise
t_expect_eq "$T_STATUS" 1 "G24 rejects a changed RMU1 position"
t_expect_out "changed the restored RMU positions" "G24 identifies a position mismatch"

G24_FIELD_CALLS=0
G24_AUDIT_ZERO_CALLS=0
G24_AUDIT_WINDOW_CALLS=0
G24_EVENTS=""
G24_RMU1_POSITION=closed
G24_RMU2_POSITION=closed
t_run eu_lab_vendor_walk eu-vendor-rdp-compromise
t_expect_eq "$T_STATUS" 1 "G24 rejects a changed RMU2 position"

t_finish
