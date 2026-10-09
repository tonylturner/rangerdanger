#!/usr/bin/env bash
# Cleanup: Kali has no kill executable, so the monitor stop must signal with
# the shell builtin inside `sh -c`, stop the recorded iec104cmd child itself,
# and keep the monitor files (and the gate evidence) when the stop fails.
# The fake docker runs Kali commands on this host with a fake iec104cmd.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

FAKE_BIN="$TMP_DIR/bin"
mkdir -p "$FAKE_BIN"
cat >"$FAKE_BIN/iec104cmd" <<'SH'
#!/bin/sh
printf '{"event":"connected","at":"2026-10-09T21:00:00Z","local":null,"peer":"%s:2404"}\n' "$1"
exec sleep "$FAKE_MONITOR_SECONDS"
SH
chmod +x "$FAKE_BIN/iec104cmd"
export FAKE_MONITOR_SECONDS=60
REFUSE_SIGNAL=0
DOCKER_LOG="$TMP_DIR/docker-calls.log"
: >"$DOCKER_LOG"

docker() {
  local detach=0
  printf '%s\n' "$*" >>"$DOCKER_LOG"
  case "$1" in
    exec)
      shift
      if [ "$1" = "-d" ]; then
        detach=1
        shift
      fi
      shift
      if [ "$1" = "kill" ]; then
        # shellcheck disable=SC2016 # the literal Docker error text
        printf 'OCI runtime exec failed: exec: "kill": executable file not found in $PATH\n' >&2
        return 127
      fi
      if [ "$REFUSE_SIGNAL" -eq 1 ] && [ "$1" = "sh" ] && [ "${4:-}" = "eu-stop" ]; then
        printf 'simulated exec failure\n' >&2
        return 126
      fi
      if [ "$detach" -eq 1 ]; then
        (PATH="$FAKE_BIN:$PATH" "$@" </dev/null >/dev/null 2>&1 &)
        return 0
      fi
      PATH="$FAKE_BIN:$PATH" "$@"
      ;;
    cp)
      cp "${2#*:}" "$3"
      ;;
    *)
      return 99
      ;;
  esac
}
monitor_files_exist() {
  [ -e "$MONITOR_LOG" ] && [ -e "$MONITOR_PID" ]
}
start_monitor() {
  monitor_running=0
  MONITOR_STOP_FAILED=0
  eu_monitor_start || exit 1
  local i
  for ((i = 0; i < 40; i++)); do
    [ -s "$MONITOR_PID" ] && return 0
    sleep 0.1
  done
  t_not_ok "the supervisor did not record the iec104cmd PID"
  exit 1
}
t_cleanup_hook() {
  local pid
  pid=$(cat "$MONITOR_PID" 2>/dev/null) || pid=""
  [ -z "$pid" ] || kill -KILL "$pid" 2>/dev/null
  rm -f "$MONITOR_LOG" "$MONITOR_ERR" "$MONITOR_RC" "$MONITOR_PID"
}

# Running monitor: the recorded PID is iec104cmd's own, and the stop is verified.
start_monitor
child=$(cat "$MONITOR_PID")
for ((i = 0; i < 20; i++)); do
  command_name=$(ps -o command= -p "$child" | awk '{print $1}')
  [ "$command_name" = "sleep" ] && break
  sleep 0.1
done
t_expect_eq "$command_name" "sleep" "the recorded PID is the monitor child (exec'd by the fake iec104cmd), not a wrapper shell"
t_run eu_stop_monitor
t_expect_fails 0 "a running monitor stops with the builtin kill"
t_expect_out "supervisor recorded exit status 143" "the supervisor recorded the child's SIGTERM exit"
t_expect_eq "$monitor_running" 0 "the monitor is no longer marked running"
if kill -0 "$child" 2>/dev/null; then
  t_not_ok "the iec104cmd child is still alive after the stop"
else
  t_ok "the iec104cmd child is gone after the stop"
fi
t_run grep -c "^exec $KALI kill" "$DOCKER_LOG"
t_expect_eq "$T_OUT" 0 "no docker exec of a kill executable was attempted"

# A monitor that already finished is not signalled again.
FAKE_MONITOR_SECONDS=0
start_monitor
for ((i = 0; i < 40; i++)); do
  [ -s "$MONITOR_RC" ] && break
  sleep 0.1
done
t_run eu_stop_monitor
t_expect_fails 0 "an already finished monitor needs no signal"
t_expect_out "supervisor recorded exit status 0" "its own exit status is kept"

# The run-1 call shape fails in an image without a kill executable.
t_run docker exec "$KALI" kill -TERM 1
t_expect_eq "$T_STATUS" 127 "exec of a missing kill executable fails (the run-1 cleanup defect)"

# A stop that cannot be verified fails and keeps the diagnostics.
FAKE_MONITOR_SECONDS=60
start_monitor
REFUSE_SIGNAL=1
t_run eu_stop_monitor
t_expect_fails 1 "an unverified stop fails"
t_expect_out "simulated exec failure" "the docker exec output is part of the failure"
t_expect_eq "$MONITOR_STOP_FAILED" 1 "the failed stop is recorded for cleanup"

EVIDENCE="$TMP_DIR/evidence"
outer_tmp="$TMP_DIR"
printf 'GET /api/firewall/sessions HTTP 200 {}\n' >"$outer_tmp/api-responses.log"
(
  # shellcheck disable=SC2030,SC2034 # cleanup state for this subshell only
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/eu-gate-cleanup.XXXXXX")
  cp "$outer_tmp/api-responses.log" "$TMP_DIR/"
  export EU_GATE_EVIDENCE_DIR="$EVIDENCE"
  # shellcheck disable=SC2034 # read by eu_cleanup
  state_touched=0 capture_running=0
  eu_cleanup 0
) >"$outer_tmp/cleanup.out" 2>&1
T_STATUS=$?
T_OUT=$(cat "$outer_tmp/cleanup.out")
t_expect_eq "$T_STATUS" 1 "cleanup after a failed stop ends the gate as FAIL"
t_expect_out "kept the Kali monitor files" "cleanup says it kept the monitor files"
if monitor_files_exist; then
  t_ok "the Kali monitor files were not removed"
else
  t_not_ok "the Kali monitor files were removed despite the failed stop"
fi
if [ -s "$EVIDENCE/api-responses.log" ] && [ -s "$EVIDENCE/kali-${MONITOR_LOG##*/}" ] \
  && [ -s "$EVIDENCE/kali-${MONITOR_PID##*/}" ]; then
  t_ok "the evidence directory holds the response log and the Kali monitor files"
else
  t_not_ok "the evidence directory is incomplete: $(ls -A "$EVIDENCE" 2>&1)"
fi

t_finish
