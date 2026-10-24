#!/usr/bin/env bash
# RangerDanger IEC 60870-5-104 increment-3 checks: RTU, iec104cmd and the
# control centre.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Headless, rerunnable checks on throwaway Docker networks: a fake Modbus
# feeder breaker and a fake GPS station clock, the RTU under test,
# iec104cmd as a controlling station, and the real control centre (CC) with
# its HTTP API on a separate management network.
#
# RTU + iec104cmd: GI values and quality, spontaneous COT 3 changes, SBO
# open/close with COT 11 feedback and ACT-TERM, a no-op command, a negative
# ACT-CON on interlock, Modbus-loss IV+NT, measurement-invalid IV on floats
# only, GPS-loss CP56 IV, and monitor exit 3 when the RTU is killed.
# CC (check/cc_checks.sh, through its HTTP API): GI and cache, commands and
# their statuses, 400/409/503, quality causes, link loss and recovery, and
# the journal.
#
# Everything it creates is named $RD_CHECK_PREFIX-* (default
# rd-iec104-check-<unique>) and removed on exit, including the images. Needs
# Docker and Python 3; the subnets below must not overlap a Docker network.
#
# Usage: [RD_CHECK_PREFIX=name] services/iec104/check/run.sh [OUT_DIR]
#        (default OUT_DIR <repo>/build/$RD_CHECK_PREFIX)
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SVC="$(dirname "$HERE")"
ROOT="$(git -C "$SVC" rev-parse --show-toplevel)"
PREFIX="${RD_CHECK_PREFIX:-rd-iec104-check-$(python3 -c 'import uuid; print(uuid.uuid4().hex[:12])')}"
export RD_CHECK_PREFIX="$PREFIX"
export RD_CHECK_OWNER="$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
docker() { python3 "$HERE/legacy_docker.py" "$@"; }
OUT="${1:-$ROOT/build/$PREFIX}"

BASEIMG=$PREFIX-iec104-base:test
IMG=$PREFIX-iec104:test
MODELIMG=$PREFIX-model:test
PY=python:3.12-alpine
# station network: RTU, breaker, GPS and the CC's IEC 104 side (IPs match points.test.json)
NET=$PREFIX-net
SUBNET=172.28.104.0/24
RTU_IP=172.28.104.10
BRK_IP=172.28.104.20
CC_IP=172.28.104.30
GPS_IP=172.28.104.50
# management network: the CC HTTP API and the probe that calls it
MGMT=$PREFIX-mgmt
MGMT_SUBNET=172.28.105.0/24
CC_HTTP_IP=172.28.105.21
# a second CC aimed at a port nothing listens on (connect_failed, never_received)
CC2_IP=172.28.104.31
CC2_HTTP_IP=172.28.105.22
RTU=$PREFIX-rtu BRK=$PREFIX-brk GPS=$PREFIX-gps MON=$PREFIX-mon NOACTTERM=$PREFIX-no-actterm \
  CC=$PREFIX-cc CC2=$PREFIX-cc2 PROBE=$PREFIX-probe
F2=$PREFIX-f2
RMU1=$PREFIX-rmu1 RMU2=$PREFIX-rmu2 OLTC=$PREFIX-oltc PLC=$PREFIX-plc
FAILED=0
if [[ ! "$PREFIX" =~ ^rd[a-z0-9-]+$ ]]; then echo "invalid check prefix" >&2; exit 2; fi
case "$OUT" in "$ROOT"/build/*) ;; *) echo "output must be under $ROOT/build" >&2; exit 2 ;; esac

cleanup() {
  python3 "$HERE/legacy_docker.py" cleanup
}
log()  { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$OUT/timeline.log"; }
pass() { printf 'PASS  %s\n' "$*" | tee -a "$OUT/results.log"; }
fail() { printf 'FAIL  %s\n' "$*" | tee -a "$OUT/results.log"; FAILED=1; }

# run a short iec104cmd operation from a throwaway client container
cli() { docker run --rm --network "$NET" "$IMG" iec104cmd "$RTU_IP" "$@" 2>>"$OUT/cli.err"; }
# breaker/gps control via busybox wget in the iec104 image
ctl() {
  if ! docker run --rm --network "$NET" "$IMG" wget -T 5 -qO- "$1" >>"$OUT/fixture-control.log" 2>&1; then
    fail "fixture control failed: $1 (fixture-control.log)"
    exit 1
  fi
}
# Read the fake breaker's HTTP state without opening a Modbus connection.
brk_stat() {
  docker exec "$BRK" python3 -I -c '
import ast, sys, urllib.request
with urllib.request.urlopen("http://127.0.0.1:8081/", timeout=2) as r:
    print(ast.literal_eval(r.read().decode())[sys.argv[1]])' "$1"
}
# a 2s monitor snapshot; its JSONL GI reflects current quality
snap() { docker run --rm --network "$NET" "$IMG" iec104cmd "$RTU_IP" monitor --json --hold 2 2>/dev/null; }

# Refuse collisions before installing cleanup; never remove another run.
python3 "$HERE/legacy_docker.py" preflight || exit 2
trap 'cleanup || exit 2' EXIT
trap 'exit 2' INT TERM
rm -rf "$OUT"; mkdir -p "$OUT"

log "build the iec104 image"
docker build -f "$SVC/Dockerfile" --target model-check -t "$MODELIMG" "$ROOT" >"$OUT/model-check.log" 2>&1 || exit 1
docker build -q -f "$SVC/Dockerfile" -t "$BASEIMG" "$ROOT" >"$OUT/base-build.log" 2>&1 || exit 1
docker build -q -f "$HERE/Dockerfile" --build-arg "BASE_IMAGE=$BASEIMG" -t "$IMG" "$HERE" >"$OUT/check-build.log" 2>&1 || exit 1
docker run --rm --network none -v "$HERE:/check:ro" --entrypoint python3 "$IMG" -I /check/loader_checks.py || exit 1
docker image inspect "$PY" >/dev/null 2>&1 || { fail "fixture image $PY must already exist"; exit 1; }

# ---- 0. strict point-map loader (no network needed) -------------------
log "check 0: strict config loader rejects bad maps"
printf '%s' '{"schema":1,"ca":1,"bogus":1,"timers":{"t0_s":10,"t1_s":10,"t2_s":5,"t3_s":5,"k":12,"w":8},"select_timeout_ms":5000,"reconnect_interval_ms":2000,"clock":{"url":"http://x/","poll_ms":1000,"stale_after_ms":5000,"synced_status":["locked"]},"devices":[{"id":"d","host":"h","port":502,"unit":1,"poll_ms":500,"timeout_ms":1000,"stale_after_ms":3000}],"points":[{"id":"p","ioa":1,"kind":"float","asdu":"M_ME_NC_1","source":{"device":"d","table":"input","address":0,"format":"f32"}}]}' >"$OUT/bad.json"
rc=0
docker run --rm -v "$OUT/bad.json:/c.json:ro" --entrypoint iec104-rtu "$IMG" --config /c.json >"$OUT/loader.log" 2>&1 || rc=$?
if [ "$rc" != 0 ] && grep -q 'unknown key "bogus"' "$OUT/loader.log"; then
  pass "strict loader rejected an unknown key with a clear message, exit $rc"
else
  fail "strict loader did not reject the bad config (loader.log)"
fi

docker network create --subnet "$SUBNET" "$NET" >/dev/null || { fail "cannot create $NET ($SUBNET)"; exit 1; }
docker network create --subnet "$MGMT_SUBNET" "$MGMT" >/dev/null || { fail "cannot create $MGMT ($MGMT_SUBNET)"; exit 1; }

log "start fake GPS and fake breaker"
docker run -d --name "$GPS" --network "$NET" --ip "$GPS_IP" \
  -v "$HERE/fake_gps.py:/app/fake_gps.py:ro" "$PY" python3 -I /app/fake_gps.py >/dev/null
docker run -d --name "$BRK" --network "$NET" --ip "$BRK_IP" \
  -v "$HERE/fake_breaker.py:/app/fake_breaker.py:ro" "$PY" python3 -I /app/fake_breaker.py >/dev/null
sleep 2

for device in rmu-1 rmu-2 oltc plc f2; do
  case "$device" in
    rmu-1) name=$RMU1; ip=172.28.104.21 ;;
    rmu-2) name=$RMU2; ip=172.28.104.22 ;;
    oltc) name=$OLTC; ip=172.28.104.23 ;;
    plc) name=$PLC; ip=172.28.104.24 ;;
    f2) name=$F2; ip=172.28.104.25 ;;
  esac
  docker run -d --name "$name" --network "$NET" --ip "$ip" -e "KIND=$device" \
    -v "$HERE/fake_device.py:/app/fake_device.py:ro" "$PY" python3 -I /app/fake_device.py >/dev/null || exit 1
done

log "start RTU"
docker run -d --cap-add NET_ADMIN --name "$RTU" --network "$NET" --ip "$RTU_IP" \
  -v "$HERE/points.test.json:/etc/points.json:ro" "$IMG" \
  iec104-rtu --config /etc/points.json --bind "$RTU_IP" --port 2404 >/dev/null

# wait for the RTU to listen and complete a first poll
for _ in $(seq 1 20); do
  docker logs "$RTU" 2>&1 | grep "listening" >/dev/null && break
  sleep 1
done
sleep 3  # let the first poll clear device_unreachable

# one persistent monitor receives every spontaneous/feedback broadcast
log "start persistent monitor"
docker run -d --name "$MON" --network "$NET" "$IMG" iec104cmd "$RTU_IP" monitor --json --hold 120 >/dev/null
sleep 2
mon() { docker logs "$MON" 2>/dev/null; }
# wait up to ~10s for a pattern to appear in the monitor's broadcast stream
mon_has() { local _; for _ in $(seq 1 20); do mon | grep "$1" >/dev/null && return 0; sleep 0.5; done; return 1; }

# ---- 1. GI values + quality (all GOOD) --------------------------------
log "check 1: GI values and quality"
snap >"$OUT/gi-baseline.jsonl"
if grep -q '"ioa":1001,.*"value":"closed","quality":"GOOD"' "$OUT/gi-baseline.jsonl" \
  && grep -q '"ioa":1002,.*"value":false,"quality":"GOOD"' "$OUT/gi-baseline.jsonl" \
  && grep -Eq '"ioa":2001,.*"value":2[89]\.[0-9]+,"quality":"GOOD"' "$OUT/gi-baseline.jsonl" \
  && grep -q '"ioa":2002,.*"quality":"GOOD"' "$OUT/gi-baseline.jsonl"; then
  pass "GI returned the sampled feeder points GOOD with expected values"
else
  fail "GI baseline values/quality unexpected (see gi-baseline.jsonl)"
fi

# ---- 2. SBO open with COT 11 feedback + ACT-TERM ----------------------
log "check 2: SBO open command"
cli dc 3001 open -sbo >"$OUT/cmd-open.log" 2>&1; openrc=$?
sleep 1
if [ "$openrc" = 0 ] && grep -q "execute terminated (ACT-TERM)" "$OUT/cmd-open.log" \
  && mon_has '"type":31,"cot":11,"ioa":1001,"value":"open"'; then
  pass "open command: execute confirmed, ACT-TERM, COT 11 feedback value=open"
else
  fail "open command feedback missing (cmd-open.log)"
fi
if mon_has '"type":13,"cot":3,"ioa":2001,"value":0\.0'; then
  pass "current reported spontaneously (COT 3) after open"
else
  fail "spontaneous current change not observed after open"
fi

# ---- 3. close, then a no-op close -------------------------------------
log "check 3: SBO close then no-op close"
cli dc 3001 close -sbo >"$OUT/cmd-close.log" 2>&1
sleep 1
cli dc 3001 close -sbo >"$OUT/cmd-noop.log" 2>&1; nooprc=$?
sleep 1
mon_has '"type":31,"cot":11,"ioa":1001,"value":"closed"'
closed_fb=$(mon | grep -c '"type":31,"cot":11,"ioa":1001,"value":"closed"')
if [ "$nooprc" = 0 ] && grep -q "execute terminated (ACT-TERM)" "$OUT/cmd-noop.log" && [ "$closed_fb" -ge 2 ]; then
  pass "close + no-op close both produced COT 11 feedback value=closed"
else
  fail "close/no-op feedback count=$closed_fb (expected >=2)"
fi

# ---- 4. interlock: close under lockout -> negative ACT-CON ------------
log "check 4: close under lockout -> negative ACT-CON"
cli dc 3001 open -sbo >/dev/null 2>&1   # open first so close is a real transition
sleep 1
ctl "http://$BRK_IP:8081/fault?active=1"   # lockout + fault + local trip
sleep 1
connections_before=$(brk_stat connections)
reads_before=$(brk_stat read_replies)
cli dc 3001 close -sbo >"$OUT/cmd-interlock.log" 2>&1; ilrc=$?
if [ "$ilrc" != 0 ] && grep -q "execute REJECTED (negative)" "$OUT/cmd-interlock.log"; then
  pass "close under lockout rejected with negative ACT-CON (exception 04 path)"
else
  fail "interlock negative ACT-CON not observed (cmd-interlock.log)"
fi
sleep 1  # allow subsequent polling (and a reconnect on regressed code)
connections_after=$(brk_stat connections)
reads_after=$(brk_stat read_replies)
if [ "$connections_before" -gt 0 ] && [ "$connections_after" = "$connections_before" ] \
  && [ "$reads_after" -gt "$reads_before" ]; then
  pass "interlock exception kept the Modbus TCP session; polling continued (connections=$connections_after)"
else
  fail "interlock exception reconnected or stopped polling (connections=$connections_before->$connections_after, reads=$reads_before->$reads_after)"
fi
if mon_has '"type":30,"cot":3,"ioa":1002,"value":true'; then
  pass "local protection trip reported spontaneously (M_SP_TB_1 COT 3)"
else
  fail "spontaneous trip single-point not observed"
fi
ctl "http://$BRK_IP:8081/reset"
sleep 2

# ---- 4b. a poll exception is a valid reply, not a broken transport -----
log "check 4b: poll exception keeps the Modbus TCP session"
connections_before=$(brk_stat connections)
reads_before=$(brk_stat read_replies)
exceptions_before=$(brk_stat read_exceptions)
ctl "http://$BRK_IP:8081/exception-next-read"
sleep 1
snap >"$OUT/gi-poll-exception.jsonl"
connections_after=$(brk_stat connections)
reads_after=$(brk_stat read_replies)
exceptions_after=$(brk_stat read_exceptions)
if [ "$connections_before" -gt 0 ] && [ "$connections_after" = "$connections_before" ] \
  && [ "$exceptions_after" -eq "$((exceptions_before + 1))" ] \
  && [ "$reads_after" -gt "$reads_before" ] \
  && grep -q '"ioa":1001,.*"value":"closed","quality":"GOOD"' "$OUT/gi-poll-exception.jsonl"; then
  pass "poll exception kept the Modbus TCP session; GOOD polling resumed (connections=$connections_after)"
else
  fail "poll exception reconnected or did not recover (connections=$connections_before->$connections_after, exceptions=$exceptions_before->$exceptions_after, reads=$reads_before->$reads_after)"
fi

# ---- 5. measurements_valid=0 -> IV on floats only ---------------------
log "check 5: measurement invalid -> IV on floats only"
ctl "http://$BRK_IP:8081/mvalid?valid=0"
sleep 2
snap >"$OUT/gi-mvalid.jsonl"
if grep -q '"ioa":2001,.*"quality":"IV"' "$OUT/gi-mvalid.jsonl" \
  && grep -q '"ioa":2002,.*"quality":"IV"' "$OUT/gi-mvalid.jsonl" \
  && grep -q '"ioa":1001,.*"quality":"GOOD"' "$OUT/gi-mvalid.jsonl"; then
  pass "measurements_valid=0 set IV on floats, status points stayed GOOD"
else
  fail "measurement-invalid quality unexpected (gi-mvalid.jsonl)"
fi
ctl "http://$BRK_IP:8081/mvalid?valid=1"
sleep 2

# ---- 6. GPS loss -> CP56 time IV --------------------------------------
log "check 6: GPS loss -> CP56Time2a IV"
docker pause "$GPS" >/dev/null
sleep 5
cli dc 3001 open -sbo >/dev/null 2>&1   # a status change carries a fresh time tag
sleep 1
mon >"$OUT/mon-gpsiv.jsonl"
if grep -q '"ioa":1001,"value":"open"[^}]*(IV)' "$OUT/mon-gpsiv.jsonl"; then
  pass "status change after GPS loss carried CP56 time with IV set"
else
  fail "CP56 IV after GPS loss not observed (mon-gpsiv.jsonl)"
fi
docker unpause "$GPS" >/dev/null
cli dc 3001 close -sbo >/dev/null 2>&1
sleep 2

# ---- 7. Modbus loss -> IV+NT within stale_after_ms --------------------
log "check 7: Modbus loss -> IV+NT"
docker pause "$BRK" >/dev/null
sleep 5   # stale_after_ms is 3000; allow margin
snap >"$OUT/gi-modbusloss.jsonl"
if grep -q '"ioa":1001,.*"quality":"IV|NT"' "$OUT/gi-modbusloss.jsonl" \
  && grep -q '"ioa":2001,.*"quality":"IV|NT"' "$OUT/gi-modbusloss.jsonl"; then
  pass "Modbus loss set IV+NT on all device points within stale_after_ms"
else
  fail "Modbus-loss IV+NT not observed (gi-modbusloss.jsonl)"
fi
docker unpause "$BRK" >/dev/null
sleep 3

# ---- control centre checks (need the RTU running) ----------------------
# shellcheck source=services/iec104/check/cc_checks.sh
. "$HERE/cc_checks.sh"
. "$HERE/inc4_checks.sh"

# ---- 8. missing ACT-TERM fails; monitor exits 3 on link loss -----------
log "check 8: missing ACT-TERM fails and monitor exits 3 on link loss"
docker rm -f "$MON" "$CC" "$CC2" "$PROBE" "$RTU" >/dev/null 2>&1 || true
sed 's/"timeout_ms":[[:space:]]*1000/"timeout_ms":10000/g' "$HERE/points.test.json" >"$OUT/points.noactterm.json"
docker run -d --cap-add NET_ADMIN --name "$RTU" --network "$NET" --ip "$RTU_IP" \
  -v "$OUT/points.noactterm.json:/etc/points.json:ro" "$IMG" \
  iec104-rtu --config /etc/points.json --bind "$RTU_IP" --port 2404 >/dev/null
for _ in $(seq 1 20); do
  docker logs "$RTU" 2>&1 | grep "listening" >/dev/null && break
  sleep 1
done
sleep 2
ctl "http://$BRK_IP:8081/reset"
docker run -d --name "$MON" --network "$NET" "$IMG" iec104cmd "$RTU_IP" monitor --json --hold 60 >/dev/null
sleep 3
ctl "http://$BRK_IP:8081/delay-next-read-after-write?ms=15000"
docker run -d --name "$NOACTTERM" --network "$NET" "$IMG" \
  iec104cmd "$RTU_IP" --json dc 3001 open -sbo >/dev/null
delay_started=0
for _ in $(seq 1 30); do
  if docker logs "$BRK" 2>&1 | grep 'delayed FC4 read started' >/dev/null; then
    delay_started=1
    break
  fi
  sleep 0.1
done
if [ "$delay_started" = 1 ]; then
  actcon_seen=0
  for _ in $(seq 1 30); do
    if docker logs "$NOACTTERM" 2>&1 | grep 'execute confirmed' >/dev/null; then
      actcon_seen=1
      break
    fi
    sleep 0.1
  done
  if [ "$actcon_seen" != 1 ]; then
    fail "command did not receive positive ACT-CON before feedback was delayed"
  fi
  docker kill "$RTU" >/dev/null
else
  actcon_seen=0
  fail "fake breaker did not delay the post-ACT-CON feedback read"
  docker kill "$RTU" >/dev/null
fi
for _ in $(seq 1 30); do
  st="$(docker inspect -f '{{.State.Status}}' "$MON" 2>/dev/null || echo gone)"
  [ "$st" = exited ] && break
  sleep 1
done
monrc="$(docker inspect -f '{{.State.ExitCode}}' "$MON" 2>/dev/null || echo X)"
docker logs "$MON" >"$OUT/mon-linkdown.jsonl" 2>/dev/null
if [ "$monrc" = 3 ] && grep -q '"event":"link_down"' "$OUT/mon-linkdown.jsonl"; then
  pass "monitor emitted link_down and exited 3 after the RTU was killed"
else
  fail "monitor exit code=$monrc (expected 3) (mon-linkdown.jsonl)"
fi
for _ in $(seq 1 30); do
  st="$(docker inspect -f '{{.State.Status}}' "$NOACTTERM" 2>/dev/null || echo gone)"
  [ "$st" = exited ] && break
  sleep 1
done
noacttermrc="$(docker inspect -f '{{.State.ExitCode}}' "$NOACTTERM" 2>/dev/null || echo X)"
docker logs "$NOACTTERM" >"$OUT/cmd-no-actterm.jsonl" 2>&1
if [ "$delay_started" = 1 ] && [ "$actcon_seen" = 1 ] && [ "$noacttermrc" = 1 ] \
  && grep -q 'no execute termination (ACT-TERM)' "$OUT/cmd-no-actterm.jsonl" \
  && grep -q '"event":"result".*"op":"dc".*"status":"failed"' "$OUT/cmd-no-actterm.jsonl"; then
  pass "command without ACT-TERM emitted failed JSON and exited 1"
else
  fail "command without ACT-TERM exited $noacttermrc or lacked failure evidence (cmd-no-actterm.jsonl)"
fi

echo; cat "$OUT/results.log"
if [ "$FAILED" = 0 ]; then
  log "ALL CHECKS PASSED"
  exit 0
fi
log "SOME CHECKS FAILED"
exit 1
