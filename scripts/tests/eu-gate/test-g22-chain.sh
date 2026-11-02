#!/usr/bin/env bash
# Actual tshark against mixed IEC104/Modbus packets; missing decoder is FAIL.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
command -v tshark >/dev/null 2>&1 || exit 1

for fixture in good multi duplicates wrong-source close no-select no-execute cross-asdu \
  wrong-ca wrong-ioa different-stream wrong-direction wrong-unit wrong-address wrong-value \
  response cross-pdu reversed timestamp-reversed malformed truncated; do
  python3 -I "$T_DIR/make_g22_chain_pcap.py" "$TMP_DIR/$fixture.pcap" "$fixture" || exit 1
  t_run eu_g22_capture_chain "$TMP_DIR/$fixture.pcap" 0 5 "$TMP_DIR/chain.json"
  case "$fixture" in
    good|multi|duplicates) t_expect_eq "$T_STATUS" 0 "$fixture proves both ordered hops" ;;
    *) t_expect_eq "$T_STATUS" 1 "$fixture fails closed" ;;
  esac
done
t_run eu_g22_capture_chain "$TMP_DIR/good.pcap" 1.2 5 "$TMP_DIR/chain.json"
t_expect_eq "$T_STATUS" 1 "pre-window select is rejected"
t_run eu_g22_capture_chain "$TMP_DIR/good.pcap" 0 3 "$TMP_DIR/chain.json"
t_expect_eq "$T_STATUS" 1 "post-window write is rejected"
t_run eu_g22_capture_chain "$TMP_DIR/absent.pcap" 0 5 "$TMP_DIR/chain.json"
t_expect_eq "$T_STATUS" 1 "unreadable capture fails"
t_run eu_g22_capture_chain "$TMP_DIR/good.pcap" 5 0 "$TMP_DIR/chain.json"
t_expect_eq "$T_STATUS" 1 "reversed window fails"

python3 -I "$T_DIR/make_iec104_pcap.py" "$TMP_DIR/filter.pcap" \
  '10.10.10.50>10.30.30.20=46/6/1/3001/82;46/6/2/3001/81;46/6/1/3002/81' || exit 1
t_run tshark -r "$TMP_DIR/filter.pcap" -Y "$(eu_iec104_command_filter 1 3001 1 10.10.10.50 1)" \
  -T fields -e frame.number
t_expect_eq "$T_STATUS" 0 "extended layer-qualified filter is accepted"
t_expect_eq "$T_OUT" "" "OPEN filter cannot combine CLOSE and other ASDUs"

mkdir "$TMP_DIR/bin"
printf '#!/bin/sh\nexit 8\n' >"$TMP_DIR/bin/tshark"
chmod +x "$TMP_DIR/bin/tshark"
OLD_PATH="$PATH"
PATH="$TMP_DIR/bin:$PATH"
t_run eu_g22_capture_chain "$TMP_DIR/good.pcap" 0 5 "$TMP_DIR/chain.json"
t_expect_eq "$T_STATUS" 1 "decoder process failure fails closed"
PATH="$OLD_PATH"

# Window lifecycle uses the real bounded helper, never the Docker daemon.
DOCKER_CALLS="$TMP_DIR/docker-calls"
DOCKER_FAIL="" DOCKER_HANG=""
docker() {
  local call="$1"
  printf '%s\n' "$*" >>"$DOCKER_CALLS"
  if [ "$call" = exec ]; then
    case "$*" in
      *"kill -INT"*) call=signal ;;
      *"kill -0"*) call=probe ;;
      *) call=pid ;;
    esac
  fi
  if [ "$DOCKER_HANG" = "$call" ]; then t_hang_docker; fi
  if [ "$DOCKER_FAIL" = "$call" ]; then return 124; fi
  case "$call" in
    inspect) printf 'fixture-image\n' ;;
    image) printf 'linux/arm64\n' ;;
    run|signal|rm) : ;;
    pid) printf '1\n' ;;
    probe) return 1 ;;
    cp) cp "$TMP_DIR/good.pcap" "$3" ;;
    logs) printf '1 packets captured\n1 packets received by filter\n0 packets dropped by kernel\n' >&2 ;;
    *) return 99 ;;
  esac
}
# Skip only the capture warmup, not the helper's watchdog sleeps.
sleep() { case "$1" in 5|15|60) command sleep "$1" ;; esac; }
window_state_cleared() {
  t_expect_eq "$CAPTURE_HOST/$CAPTURE_PATH/$CAPTURE_PID_FILE/$CAPTURE_LOG/$CAPTURE_PID/$capture_running" \
    "/////0" "$1 clears all shared capture state"
}
for call in inspect image run; do
  DOCKER_FAIL="$call"
  : >"$DOCKER_CALLS"
  t_run eu_window_start G22
  t_expect_eq "$T_STATUS" 1 "$call failure fails the capture start"
  t_expect_fails 1 "$call failure records FAIL"
  case "$call" in
    image) t_expect_out "docker image inspect" "image inspect failure names its call" ;;
    *) t_expect_out "docker $call" "$call failure names its call" ;;
  esac
  window_state_cleared "$call failure"
  if [ "$call" = run ]; then
    t_expect_eq "$(tail -n 1 "$DOCKER_CALLS")" "rm -f rdinc4-i-window-$$" "failed run still force-removes its owned container"
  else
    t_expect_no_out "docker rm" "inspection failure has not acquired a capture container"
  fi
done
DOCKER_FAIL=""
for call in success pid signal probe cp logs rm; do
  t_run eu_window_start G22
  t_expect_eq "$T_STATUS" 0 "$call fixture starts a window"
  if [ "$call" = success ]; then DOCKER_FAIL=""; else DOCKER_FAIL="$call"; fi
  if [ "$call" = pid ]; then CAPTURE_PID="" CAPTURE_PID_FILE=/tmp/pid; fi
  : >"$DOCKER_CALLS"
  t_run eu_window_finish G22
  if [ "$call" = success ]; then
    t_expect_eq "$T_STATUS" 0 "healthy capture passes integrity"
    t_expect_fails 0 "healthy capture has no FAIL"
  else
    t_expect_eq "$T_STATUS" 1 "$call timeout fails the capture finish"
    t_expect_fails 1 "$call timeout records FAIL"
    case "$call" in
      pid) t_expect_out "docker exec rdinc4-i-window-$$ cat /tmp/pid" "PID timeout names its call" ;;
      signal) t_expect_out "docker exec rdinc4-i-window-$$ kill -INT" "signal timeout names its call" ;;
      probe) t_expect_out "docker exec rdinc4-i-window-$$ kill -0" "liveness timeout is not mistaken for a stopped capture" ;;
      *) t_expect_out "docker $call" "$call timeout names its call" ;;
    esac
  fi
  t_expect_eq "$(tail -n 1 "$DOCKER_CALLS")" "rm -f rdinc4-i-window-$$" "$call finish attempts bounded removal"
  window_state_cleared "$call finish"
  DOCKER_FAIL=""
done

t_run eu_window_start G22
t_expect_eq "$T_STATUS" 0 "hung-logs fixture starts a window"
DOCKER_HANG=logs
: >"$DOCKER_CALLS"
t_run eu_window_finish G22
t_expect_eq "$T_STATUS" 1 "real five-second watchdog fails hung docker logs"
t_expect_fails 1 "hung docker logs records FAIL"
t_expect_out "G22 docker logs rdinc4-i-window-$$ failed" "real capture timeout names its Docker call"
t_expect_eq "$(tail -n 1 "$DOCKER_CALLS")" "rm -f rdinc4-i-window-$$" "capture timeout still removes its container"
window_state_cleared "real capture timeout"
t_finish
