#!/usr/bin/env bash
# shellcheck disable=SC2034 # fixture globals consumed by sourced gate functions
# Range evidence uses the VM clock even during network cuts and service pauses.
# shellcheck disable=SC2329 # stubs invoked by sourced helpers
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
# Exercise the production clock rather than the harness's offline clock.
# shellcheck source-path=SCRIPTDIR source=../../lib/eu-range.sh
. "$T_REPO/scripts/lib/eu-range.sh"
CLOCK_LOG="$TMP_DIR/clock.log"
CLOCK_RC=0
CLOCK_HANG=0
docker() {
  printf '%s\n' "$*" >>"$CLOCK_LOG"
  if [ "$CLOCK_HANG" -eq 1 ]; then t_hang_docker; fi
  [ "$CLOCK_RC" -eq 0 ] || return "$CLOCK_RC"
  case "$*" in
    "exec $FW date -u +%Y-%m-%dT%H:%M:%S.%NZ")
      printf '2026-10-15T12:00:00.123456789Z\n' ;;
    "exec $FW date -u +%s.%N") printf '1760529600.123456789\n' ;;
    "exec $FW date -u +%s%N") printf '1760529600123456789\n' ;;
    "pause $CC") : ;;
    "inspect -f {{.Image}} rangerdanger-openplc") printf 'fixture\n' ;;
    "image inspect -f {{.Os}}/{{.Architecture}} fixture") printf 'linux/arm64\n' ;;
    "run -d "*|"exec rdinc4-i-scan-"*) : ;;
    *) return 99 ;;
  esac
}
t_run eu_utc
t_expect_eq "$T_STATUS" 0 "range clock read succeeds"
t_expect_eq "$T_OUT" 2026-10-15T12:00:00.123456789Z "UTC preserves GNU date nanoseconds"
t_expect_eq "$(cat "$CLOCK_LOG")" "exec $FW date -u +%Y-%m-%dT%H:%M:%S.%NZ" \
  "one Docker exec reads firewall time, independent of range networking"
t_run eu_epoch_seconds
t_expect_eq "$T_OUT" 1760529600.123456789 "packet windows use range epoch seconds"
t_run eu_epoch_ns
t_expect_eq "$T_OUT" 1760529600123456789 "event cutoffs retain integer nanoseconds"
CLOCK_RC=17
clock_to_file() { "$1" >"$TMP_DIR/clock.stdout"; }
for clock in eu_utc eu_epoch_seconds eu_epoch_ns; do
  t_run clock_to_file "$clock"
  t_expect_eq "$T_STATUS" 17 "$clock failure propagates without host fallback"
  t_expect_fails 1 "$clock records the Docker failure"
  t_expect_out "$clock docker exec $FW date failed (exit 17)" "$clock names its failed call"
  t_expect_eq "$(cat "$TMP_DIR/clock.stdout")" "" "$clock never manufactures a host boundary"
done
CLOCK_RC=0 CLOCK_HANG=1
# Exercise the real watchdog and the $(...) form used at evidence boundaries.
clock_boundary() {
  local boundary
  boundary=$(eu_utc) || return
  printf '%s\n' "$boundary"
}
t_run clock_boundary
t_expect_eq "$T_STATUS" 124 "a hung clock fails the evidence boundary at its real five-second limit"
t_expect_out "FAIL eu_utc docker exec $FW date failed (exit 124)" "clock timeout names the call on stderr"
t_expect_no_out "2026-" "timed-out command substitution emits no timestamp"
CLOCK_HANG=0 CLOCK_RC=17

# A clock failure cannot execute a command or validate it with an empty window.
eu_wait_valid_device() { :; }
eu_get_operator() { API_BODY=$(cat "$T_FIXTURES/operator-gi-untimed.json"); }
eu_management() { API_BODY='{"generation":42,"latest_seq":0}'; }
POSTS=0
eu_api_post() { POSTS=$((POSTS + 1)); }
t_run eu_command rmu-1-command open completed
t_expect_eq "$T_STATUS" 17 "failed start clock aborts the attempt"
t_expect_eq "$POSTS" 0 "no physical operation without its start boundary"
CLOCK_RC=0
eu_api_post() { POSTS=$((POSTS + 1)); CLOCK_RC=17; API_BODY='{}'; }
t_run eu_command rmu-1-command open completed
t_expect_eq "$T_STATUS" 17 "failed end clock cannot validate an unbounded result"
t_expect_eq "$POSTS" 1 "failed end boundary never retries the physical operation"
CLOCK_RC=0 POSTS=0

# Internally ordered VM evidence passes regardless of the host's wall clock.
eu_management() {
  if [ "$3" = '/api/audit?after=0' ]; then
    API_BODY='{"generation":42,"latest_seq":20}'
  else
    API_BODY='{"generation":42,"oldest_seq":1,"entries":[{"seq":21,
      "at":"2026-10-15T12:00:00.123456789Z","operation":"open","result":"accepted",
      "context":{"protocol":"modbus","peer":"10.30.30.20:1234"}}]}'
  fi
}
eu_api_post() {
  POSTS=$((POSTS + 1))
  # shellcheck disable=SC2034 # consumed by eu_command
  HTTP_STATUS=200
  API_BODY='{"id":"cmd-1","point":"rmu-1-command","operation":"open","result":"completed",
    "selected_at":"2026-10-15T12:00:00.123456789Z","executed_at":"2026-10-15T12:00:00.123456789Z",
    "con_at":"2026-10-15T12:00:00.123456789Z","term_at":"2026-10-15T12:00:00.123456789Z",
    "feedback":{"ioa":1101,"value":"open","at":"2026-10-15T12:00:00.123456789Z"}}'
}
: >"$CLOCK_LOG"
t_run eu_command rmu-1-command open completed
t_expect_eq "$T_STATUS" 0 "command confirmations and audit share the real helper's VM clock"
t_expect_fails 0 "range timestamps pass without a host-clock offset or tolerance"
t_expect_eq "$POSTS" 1 "exactly one physical command is issued"
t_expect_eq "$(grep -c "^exec $FW date " "$CLOCK_LOG")" 2 "both attempt boundaries use Docker time"

# G18's query end, as well as its pause boundaries, must use the range clock.
eu_reset_all() { :; }
eu_get_operator() { API_BODY='{"cache_epoch":"fixture"}'; }
eu_assert_jq() { :; }
eu_window_start() { :; }
eu_window_finish() { return 1; } # stop before packet decoding
eu_unpause_owned() { :; }
sleep() { case "$1" in 5|15|60) command sleep "$1" ;; esac; }
eu_api_get() { printf '%s\n' "$1" >>"$TMP_DIR/api-paths"; API_BODY='{}'; }
eu_historian_gap() { return 0; }
: >"$CLOCK_LOG"
t_run eu_gate_G18
t_expect_out "G18 real CC ingest resumes" "G18 reaches its continuity assertion"
t_expect_eq "$(grep -c "^exec $FW date " "$CLOCK_LOG")" 4 \
  "G18 takes all four boundaries through the shared range clock"
t_expect_eq "$(tail -n 1 "$TMP_DIR/api-paths")" \
  "/api/historian/history?point=feeder-current&since=2026-10-15T12:00:00.123456789Z&until=2026-10-15T12:00:00.123456789Z" \
  "historian query has no host-clock upper bound"

# G11 compares capture timestamps, which also originate in the VM kernel.
eu_apply_named() { EU_READY_MS=1000; EU_READY_EPOCH=$(eu_epoch_ns); }
eu_wait_for_baseline() { :; }
eu_make_custom_policy() { :; }
eu_policy_has_no_ephemeral_allow() { return 0; }
eu_commit_manual_policy() { EU_READY_MS=1000; }
eu_check_nft_chain() { :; }
# shellcheck disable=SC2034 # consumed by G11 in eu-gates-resilience.sh
eu_read_sessions() { SESSION_BODY='{}'; }
# shellcheck disable=SC2034 # consumed by G11 in eu-gates-resilience.sh
eu_session_tuple() { SESSION_TUPLE=fixture; }
eu_kill_tuple() { return 0; }
eu_wait_for_operator() { return 0; }
: >"$CLOCK_LOG"
t_run eu_gate_G11
t_expect_out "G11 four points IV+NT" "G11 reaches the capture window end"
t_expect_eq "$(cat "$CLOCK_LOG")" "$(printf '%s\n%s' \
  "exec $FW date -u +%s.%N" "exec $FW date -u +%s.%N")" \
  "both G11 packet boundaries use range epoch seconds"

# Firewall events and the Kali monitor's ASDU timestamps also come from the VM.
eu_run_kali_gi() { :; }
eu_fresh_deny_event() { printf 'deny cutoff: %s\n' "$2"; }
t_run eu_gate_G5
t_expect_out 'deny cutoff: 1760529600123456789' "G5 DENY freshness uses range nanoseconds"
MONITOR_ASDUS=0
eu_monitor_wait_event() { :; }
eu_monitor_asdu_count() { MONITOR_ASDUS=$((MONITOR_ASDUS + 1)); }
# shellcheck disable=SC2034 # consumed by eu_g6_commit_monitor_loss
eu_monitor_read() {
  MONITOR_LINES=1
  MONITOR_BODY="{\"event\":\"connected\",\"local\":null,\"peer\":\"$RTU_IP:2404\"}"
}
eu_window_finish() { :; }
eu_monitor_finish() { :; }
eu_monitor_no_asdu_after() { printf 'ASDU cutoff: %s\n' "$1"; }
t_run eu_g6_commit_monitor_loss
t_expect_eq "$T_STATUS" 0 "G6 completes commit-only established-session loss with range time"
t_expect_out 'ASDU cutoff: 1760529600123456789' "G6 ASDU cutoff uses range nanoseconds"

# The scan injector is stubbed: never start Docker resources or trace a process.
: >"$CLOCK_LOG"
t_run eu_gate_G17
t_expect_out 'G17 all five PLC points IV heartbeat' "G17 reaches its frozen-heartbeat window end"
t_expect_eq "$(grep "^exec $FW date " "$CLOCK_LOG")" "$(printf '%s\n%s' \
  "exec $FW date -u +%s.%N" "exec $FW date -u +%s.%N")" \
  "both G17 capture boundaries use range epoch seconds"
t_finish
