#!/usr/bin/env bash
# RangerDanger IEC 60870-5-104 spike: scripted, rerunnable correctness checks.
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Builds the image, runs the RTU, the control centre and iec104cmd on a
# throwaway Docker network, records TCP/2404 on that network's bridge, and
# asserts each check on the decoded capture plus the program logs. Needs
# docker, tshark and python3 on the host. No host port is published and
# nothing outside the throwaway network is touched. Evidence and the pcap
# are left under the output directory for the report.
#
# Usage: services/iec104/check/run.sh [OUT_DIR]   (default <repo>/build/iec104-spike)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SVC="$(dirname "$HERE")"
ROOT="$(git -C "$SVC" rev-parse --show-toplevel)"
OUT="${1:-$ROOT/build/iec104-spike}"
TSHARK="${TSHARK:-tshark}"

IMG=rd-iec104:spike
CAPIMG=rd-iec104-capture:spike
NET=rd-iec104-check
SUBNET=10.204.104.0/24
RTU_IP=10.204.104.10
CC_IP=10.204.104.20
RTU=rd-iec104-check-rtu CC=rd-iec104-check-cc CAP=rd-iec104-check-cap
SELECT_TIMEOUT_MS=3000 CC_T1=3 CC_T3=4
PCAP="$OUT/iec104.pcap"
FAILED=0

cleanup() { docker rm -f "$CC" "$RTU" "$CAP" >/dev/null 2>&1 || true; docker network rm "$NET" >/dev/null 2>&1 || true; }
trap cleanup EXIT
log()  { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$OUT/timeline.log"; }
pass() { printf 'PASS  %s\n' "$*" | tee -a "$OUT/results.log"; }
fail() { printf 'FAIL  %s\n' "$*" | tee -a "$OUT/results.log"; FAILED=1; }

# iec104cmd from a dedicated address; asserts the process exit code.
# cmd <octet> <name> <want-exit> <args...>
cmd() {
  local octet=$1 name=$2 want=$3; shift 3
  log "run $name: iec104cmd $* (10.204.104.$octet)"
  local rc=0
  docker run --rm --network "$NET" --ip "10.204.104.$octet" "$IMG" iec104cmd "$RTU_IP" "$@" >"$OUT/$name.log" 2>&1 || rc=$?
  [ "$rc" = "$want" ] && pass "$name exit=$rc" || fail "$name exit=$rc want=$want (see $name.log)"
}
# wait_cc <pattern> <count> <limit-seconds>
wait_cc() {
  local p=$1 n=$2 lim=$3 w=0
  while [ "$(docker logs "$CC" 2>&1 | grep -c -- "$p" || true)" -lt "$n" ]; do
    [ "$w" -ge "$lim" ] && { fail "control centre never logged '$p' x$n in ${lim}s"; return 1; }
    sleep 1; w=$((w+1))
  done
}
# tshark field query on the capture
tf() { "$TSHARK" -r "$PCAP" -Y "$1" -T fields "${@:2}" 2>/dev/null; }

command -v "$TSHARK" >/dev/null || { echo "tshark not found (set TSHARK)"; exit 2; }
rm -rf "$OUT"; mkdir -p "$OUT"; cleanup

log "build image and capture helper"
docker build -q -t "$IMG" "$SVC" >/dev/null
docker build -q -t "$CAPIMG" -f "$HERE/capture.Dockerfile" "$HERE" >/dev/null

NET_ID="$(docker network create --subnet "$SUBNET" "$NET")"
docker run -d --name "$CAP" --net host --cap-add NET_ADMIN --cap-add NET_RAW -v "$OUT:/out" \
  "$CAPIMG" -i "br-${NET_ID:0:12}" -U -w /out/iec104.pcap 'tcp port 2404' >/dev/null
sleep 1
log "start RTU (select-timeout=${SELECT_TIMEOUT_MS}ms)"
docker run -d --name "$RTU" --network "$NET" --ip "$RTU_IP" "$IMG" iec104-rtu -select-timeout "$SELECT_TIMEOUT_MS" >/dev/null
sleep 1
log "start control centre (t1=${CC_T1}s t3=${CC_T3}s)"
docker run -d --name "$CC" --network "$NET" --ip "$CC_IP" "$IMG" iec104-cc "$RTU_IP" -t1 "$CC_T1" -t3 "$CC_T3" -poll-ms 1000 >/dev/null
wait_cc "GI complete" 1 20

cmd 35 01-gi               0 gi
cmd 30 02-sbo-open         0 dc 3001 open  -sbo
cmd 36 03-sbo-close        0 dc 3001 close -sbo
log "local protection trip: SIGUSR1 to the RTU"
docker kill -s USR1 "$RTU" >/dev/null; sleep 1
cmd 31 05-exec-no-select   1 dc 3001 close
cmd 32 06-select-expiry    1 dc 3001 close -delay-ms $((SELECT_TIMEOUT_MS + 1500))
cmd 33 07-value-mismatch   1 dc 3001 close -mismatch
cmd 34 08-stopdt-startdt   0 stopstart

log "idle $((CC_T3 * 3))s so TESTFR runs on t3"
sleep $((CC_T3 * 3))
log "t1: pause the RTU so the control centre's TESTFR goes unanswered"
docker pause "$RTU" >/dev/null
wait_cc "cache marked stale" 1 $((CC_T3 + CC_T1 + 10)) || true
sleep $((CC_T1 + 2))
docker unpause "$RTU" >/dev/null
wait_cc "GI complete" 2 60 || true
log "restart: SIGKILL the RTU, then start it again"
docker kill "$RTU" >/dev/null
wait_cc "cache marked stale" 2 $((CC_T3 + CC_T1 + 15)) || true
docker start "$RTU" >/dev/null
wait_cc "GI complete" 3 60 || true
sleep 2

log "collect logs and pcap"
docker stop -t 5 "$CC" >/dev/null; docker logs "$CC" >"$OUT/cc.log" 2>&1
docker logs "$RTU" >"$OUT/rtu.log" 2>&1
docker stop -t 5 "$CAP" >/dev/null
docker image inspect "$IMG" --format '{{.Architecture}} {{.Size}}' >"$OUT/image.txt"

# ---- assertions on protocol evidence ----
log "assert on the capture"
[ "$(tf '_ws.malformed || _ws.expert.severity=="Error"' -e frame.number | wc -l | tr -d ' ')" = 0 ] \
  && pass "zero malformed/error frames" || fail "malformed or error frames present"
# GI: ACT(6) -> ACT_CON(7) -> ACT_TERM(10), and monitoring objects at COT 20
grep -q . <(tf 'iec60870_asdu.typeid==100 && iec60870_asdu.causetx==7 && iec60870_asdu.nega==0' -e frame.number) \
  && grep -q . <(tf 'iec60870_asdu.typeid==100 && iec60870_asdu.causetx==10' -e frame.number) \
  && grep -q . <(tf '(iec60870_asdu.typeid==3||iec60870_asdu.typeid==13) && iec60870_asdu.causetx==20' -e frame.number) \
  && pass "GI: ACT_CON + ACT_TERM + COT20 monitoring objects" || fail "GI evidence missing"
# Spontaneous M_DP_TB_1 (type 31) COT 3 with a CP56Time2a timestamp (the local trip)
grep -q . <(tf 'iec60870_asdu.typeid==31 && iec60870_asdu.causetx==3 && iec60870_asdu.cp56time' -e frame.number) \
  && pass "spontaneous M_DP_TB_1 COT 3 with CP56Time2a" || fail "spontaneous COT-3 time-tagged event missing"
# SBO: positive select then positive execute (dco.se marks select)
grep -q . <(tf 'iec60870_asdu.typeid==46 && iec60870_asdu.dco.se==1 && iec60870_asdu.causetx==7 && iec60870_asdu.nega==0' -e frame.number) \
  && grep -q . <(tf 'iec60870_asdu.typeid==46 && iec60870_asdu.dco.se==0 && iec60870_asdu.causetx==7 && iec60870_asdu.nega==0' -e frame.number) \
  && pass "SBO select+execute confirmed positively" || fail "SBO positive confirms missing"
# At least one negative execute confirm (no-select / expiry / mismatch)
[ "$(tf 'iec60870_asdu.typeid==46 && iec60870_asdu.dco.se==0 && iec60870_asdu.causetx==7 && iec60870_asdu.nega==1' -e frame.number | wc -l | tr -d ' ')" -ge 3 ] \
  && pass "execute without/invalid select rejected (>=3 negative confirms)" || fail "expected >=3 negative execute confirms"
# TESTFR act (U 0x10) and con (0x20) on idle
grep -q . <(tf 'iec60870_104.utype==0x10' -e frame.number) && grep -q . <(tf 'iec60870_104.utype==0x20' -e frame.number) \
  && pass "TESTFR act/con exchanged on idle" || fail "TESTFR frames missing"
# STOPDT/STARTDT: act (0x04/0x01) and con (0x08/0x02)
grep -q . <(tf 'iec60870_104.utype==0x04' -e frame.number) && grep -q . <(tf 'iec60870_104.utype==0x08' -e frame.number) \
  && pass "STOPDT act/con exchanged" || fail "STOPDT frames missing"

log "assert on the control-centre log"
[ "$(grep -c 'cache marked stale on link loss: .*q=IV|NT' "$OUT/cc.log")" -ge 2 ] \
  && pass "control centre marked values IV|NT on link loss (x>=2)" || fail "link-loss quality marking missing"
[ "$(grep -c 'GI complete' "$OUT/cc.log")" -ge 3 ] \
  && pass "control centre reconnected and re-ran GI, quality recovered (x>=3)" || fail "reconnect+GI recovery missing"

"$TSHARK" -r "$PCAP" -q -z expert > "$OUT/expert.txt" 2>&1 || true
echo; cat "$OUT/results.log"
[ "$FAILED" = 0 ] && { log "ALL CHECKS PASSED"; exit 0; } || { log "SOME CHECKS FAILED"; exit 1; }
