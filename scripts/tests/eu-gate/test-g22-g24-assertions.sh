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
eu_lab_step() { :; }
eu_lab_exec() { :; }
eu_get_field() {
  API_BODY='{"devices":{"breaker-1":{"revision":37}}}'
}
eu_assert_unchanged_breaker() { :; }
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

t_run eu_lab_hardening_walk eu-hardening-configurations
t_expect_eq "$T_STATUS" 0 "G22 remote OPEN succeeds without a protection trip"
t_expect_eq "$FIELD_WAITS" 2 "G22 leaves its separate restore assertion in place"
t_expect_no_out "protection_tripped" "G22 open assertion does not constrain protection state"
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

t_finish
