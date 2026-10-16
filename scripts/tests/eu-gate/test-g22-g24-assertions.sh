#!/usr/bin/env bash
# shellcheck disable=SC2329 # test stubs are called indirectly by sourced gates
# G22 remote OPEN must not require a protection trip; G24 probes fixed-address
# vendor services without host discovery and distinguishes open from filtered.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OPEN_FIELD='{"devices":{"breaker-1":{"position":"open","protection_tripped":false,"revision":36}},"process":{"buses":{"s1":{"live":false}},"devices":{"feeder-breaker":{"delivered":true,"solved_revision":36}}}}'
RESTORED_FIELD='{"devices":{"breaker-1":{"position":"closed","protection_tripped":false,"revision":37}},"process":{"devices":{"feeder-breaker":{"delivered":true,"solved_revision":37}}}}'
FIELD_WAITS=0
ATTACK_PREDICATE=""
DENIED_EXIT=2
DENIED_HTTP=200
DENIED_RESPONSE=""
UNCHANGED_CALLS=0
eu_lab_step() { :; }
eu_lab_exec() { :; }
eu_api_post() {
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
}
eu_wait_for_operator() { :; }
eu_wait_for_field() {
  local field
  FIELD_WAITS=$((FIELD_WAITS + 1))
  case "$FIELD_WAITS" in
    1)
      ATTACK_PREDICATE="$1"
      field="$OPEN_FIELD"
      ;;
    2) field="$RESTORED_FIELD" ;;
    *)
      fail "unexpected G22 field wait $FIELD_WAITS"
      return 1
      ;;
  esac
  if ! printf '%s' "$field" | jq -e "$1" >/dev/null; then
    fail "G22 field assertion rejected its expected state"
    return 1
  fi
}

for DENIED_EXIT in 2 1; do
  FIELD_WAITS=0
  UNCHANGED_CALLS=0
  t_run eu_lab_hardening_walk eu-hardening-configurations
  t_expect_eq "$T_STATUS" 0 "G22 accepts hardened retest denial with exit $DENIED_EXIT"
  t_expect_eq "$FIELD_WAITS" 2 "G22 leaves its separate restore assertion in place"
  t_expect_eq "$UNCHANGED_CALLS" 1 "G22 walk still proves the breaker was unchanged"
  t_expect_no_out "protection_tripped" "G22 open assertion does not constrain protection state"

  UNCHANGED_CALLS=0
  t_run eu_lab_hardening_prepare eu-hardening-configurations
  t_expect_eq "$T_STATUS" 0 "G22 prepare accepts hardened retest denial with exit $DENIED_EXIT"
  t_expect_eq "$UNCHANGED_CALLS" 1 "G22 prepare still proves the breaker was unchanged"
done

DENIED_EXIT=0
FIELD_WAITS=0
UNCHANGED_CALLS=0
t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "G22 rejects a hardened retest that exits successfully"
t_expect_fails 1 "G22 reports a successful retest as not denied"
t_expect_eq "$UNCHANGED_CALLS" 0 "G22 does not claim unchanged state after an allowed retest"
t_run eu_lab_hardening_prepare eu-hardening-configurations
t_expect_eq "$T_STATUS" 1 "G22 prepare rejects a hardened retest that exits successfully"
t_expect_fails 1 "G22 prepare reports a successful retest as not denied"

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
t_run jq -e "$ATTACK_PREDICATE" <<<"$STALE_FIELD"
t_expect_eq "$T_STATUS" 1 "G22 rejects an undelivered stale solver revision"
UNDELIVERED_FIELD=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.process.devices["feeder-breaker"].delivered = false')
t_run jq -e "$ATTACK_PREDICATE" <<<"$UNDELIVERED_FIELD"
t_expect_eq "$T_STATUS" 1 "G22 requires the solver result to be delivered"
POSITION_CLOSED=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.devices["breaker-1"].position = "closed"')
t_run jq -e "$ATTACK_PREDICATE" <<<"$POSITION_CLOSED"
t_expect_eq "$T_STATUS" 1 "G22 requires the breaker to be open"
SECTION_LIVE=$(printf '%s' "$OPEN_FIELD" | jq -c \
  '.process.buses.s1.live = true')
t_run jq -e "$ATTACK_PREDICATE" <<<"$SECTION_LIVE"
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
