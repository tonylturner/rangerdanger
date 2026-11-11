#!/usr/bin/env bash
# Even a failed network boundary must not turn G19 into historian ingestion.
# shellcheck disable=SC2329 # stubs called by sourced gates
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CALLS="$TMP_DIR/history-calls"
: >"$CALLS"
BOUNDARY=unavailable
CURL_RC=28
: >"$TMP_DIR/archived"
eu_apply_named() { :; }
eu_window_start() { :; }
eu_window_finish() { :; }
docker() {
  local arg payload="" take_next=false
  printf '%s\n' "$*" >>"$CALLS"
  for arg in "$@"; do
    if [ "$take_next" = true ]; then payload="$arg"; take_next=false; fi
    case "$arg" in
      -d) take_next=true ;;
      --post-data=*) payload="${arg#--post-data=}" ;;
    esac
  done
  if printf '%s' "$payload" | jq -e '.schema == 3 and (.points | length > 0)' >/dev/null; then
    printf 'archived\n' >>"$TMP_DIR/archived"
  fi
  if [ "$BOUNDARY" = unavailable ]; then
    case "$*" in
      # A firewall RESET can yield the same refusal; G19 claims no drop proof.
      *wget*) printf 'Connection refused\n' >&2; return 1 ;;
      *) return "$CURL_RC" ;;
    esac
  fi
  # Historian would reject the schema even if a faulty rule allowed this peer.
  printf 'invalid schema\n'
  return 0
}
tshark() {
  case "$*" in
    *http.response.code*) printf '123\n' ;;
    *) printf '10.70.70.40\t8081\n10.99.99.23\t8080\n10.50.50.20\t8080\n' ;;
  esac
}
t_run eu_gate_G19
t_expect_fails 0 "unavailable probes and real CC ingest evidence pass G19"
t_expect_eq "$(wc -l <"$TMP_DIR/archived" | tr -d ' ')" 0 "unavailable probes create no archive rows"
t_expect_out "unavailable (wget 1; not drop proof)" "refusal is not labelled firewall denial"
t_expect_no_out "denied" "G19 makes no DENY/drop claim"
t_expect_eq "$(grep -c '\"schema\":0' "$CALLS")" 5 "every untrusted probe uses a non-archivable body"
CURL_RC=7
t_run eu_gate_G19
t_expect_fails 0 "curl connect failure, including RESET refusal, proves only unavailability"
t_expect_out "unavailable (curl 7; not drop proof)" "generic curl failure is not labelled dropped"
BOUNDARY=allowed
t_run eu_gate_G19
t_expect_fails 5 "a reachable boundary fails (including HTTP rejection), never passes on 400"
t_expect_eq "$(wc -l <"$TMP_DIR/archived" | tr -d ' ')" 0 "even an allowed boundary cannot archive the G19 probe"
t_finish
