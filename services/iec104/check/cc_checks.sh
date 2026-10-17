# RangerDanger IEC 60870-5-104 checks: the control centre (contract §6).
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Sourced by check/run.sh once the RTU checks have run and the RTU, fake
# breaker and fake GPS are up. Starts the real iec104-cc against the RTU
# with its HTTP API on the management network, and a python probe container
# on that network (check/ccprobe.py) that every assertion goes through: the
# CC learns plant state only through IEC 104, and the harness reads it only
# through the CC API.
# shellcheck shell=bash

CC_URL="http://$CC_HTTP_IP:8080"
probe() { docker exec "$PROBE" python3 -I /app/ccprobe.py "$CC_URL" "$@"; }
# cc_cmd NAME OP: POST a command; "<status>" goes to $OUT/NAME.status, the body to $OUT/NAME.json
cc_cmd() {
  local out
  out="$(probe cmd "$2")"
  printf '%s\n' "${out%% *}" >"$OUT/$1.status"
  printf '%s\n' "${out#* }" >"$OUT/$1.json"
}
# jtest FILE EXPR: EXPR (python, over the parsed JSON `d`) must hold
jtest() {
  docker exec -i "$PROBE" python3 -I -c \
    'import json,sys; d=json.load(sys.stdin); sys.exit(0 if eval(sys.argv[1]) else 1)' "$2" <"$1"
}
# jget FILE EXPR: print EXPR (python, over the parsed JSON `d`)
jget() {
  docker exec -i "$PROBE" python3 -I -c 'import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1]))' "$2" <"$1"
}
# cmd_ok NAME OP VALUE: the command completed with matched COT 11 feedback carrying VALUE
cmd_ok() {
  cc_cmd "$1" "$2"
  [ "$(cat "$OUT/$1.status")" = 200 ] && jtest "$OUT/$1.json" \
    "d['result']=='completed' and d['id'].startswith('cmd-') and d['selected_at'] and d['executed_at'] \
     and d['con_at'] and d['term_at'] and d['feedback']['value']=='$3' and d['feedback']['ioa']==1001 \
     and d['feedback']['at'] and d['reason'] is None"
}
POS="pt(s,'feeder-breaker-position')"
TRIP="pt(s,'feeder-protection-trip')"

# ---- CC 0. strict config and fatal binds (no network needed) -----------
log "CC check 0: strict config loader, fatal --http and --bind failures"
rc=0
docker run --rm -v "$OUT/bad.json:/c.json:ro" --entrypoint iec104-cc "$IMG" --config /c.json --bind 0.0.0.0 \
  --target 127.0.0.1:2404 --http 127.0.0.1:18080 >"$OUT/cc-loader.log" 2>&1 || rc=$?
if [ "$rc" = 1 ] && grep -q 'unknown key "bogus"' "$OUT/cc-loader.log"; then
  pass "CC: strict loader rejected an unknown key, exit 1"
else
  fail "CC: strict loader did not reject the bad config (cc-loader.log, exit $rc)"
fi
rc=0
docker run --rm -v "$HERE/points.test.json:/c.json:ro" --entrypoint iec104-cc "$IMG" --config /c.json \
  --bind 0.0.0.0 --target 127.0.0.1:2404 --http 192.0.2.1:8080 >"$OUT/cc-httpbind.log" 2>&1 || rc=$?
rc2=0
docker run --rm -v "$HERE/points.test.json:/c.json:ro" --entrypoint iec104-cc "$IMG" --config /c.json \
  --bind 192.0.2.1 --target 127.0.0.1:2404 --http 127.0.0.1:18080 >"$OUT/cc-bind.log" 2>&1 || rc2=$?
if [ "$rc" = 1 ] && grep -q 'cannot listen on 192.0.2.1:8080' "$OUT/cc-httpbind.log" \
  && [ "$rc2" = 1 ] && grep -q -- '--bind 192.0.2.1' "$OUT/cc-bind.log"; then
  pass "CC: a failed --http bind and a non-local --bind are fatal (exit 1)"
else
  fail "CC: bind failures not fatal (cc-httpbind.log exit $rc, cc-bind.log exit $rc2)"
fi

log "start the API probe"
docker run -d --name "$PROBE" --network "$MGMT" --ip 172.28.105.30 \
  -v "$HERE:/app:ro" "$PY" python3 -I /app/fake_historian.py >/dev/null

# start_cc NAME IEC104_IP HTTP_IP TARGET: the CC on the station network, its API on the management network
start_cc() {
  docker create --name "$1" --network "$NET" --ip "$2" -v "$HERE/points.test.json:/etc/points.json:ro" "$IMG" \
    iec104-cc --config /etc/points.json --bind "$2" --target "$4" --http "$3:8080" --historian-url http://172.28.105.30:8081/api/ingest --historian-bind "$3" >/dev/null
  docker network connect --ip "$3" "$MGMT" "$1"
  docker start "$1" >/dev/null
}

# ---- CC 0b. RTU unreachable: one connect_failed, never_received -------
log "CC check 0b: nothing listening -> one connect_failed, points never_received, 503, SIGTERM exit 0"
start_cc "$CC2" "$CC2_IP" "$CC2_HTTP_IP" "$GPS_IP:2404"
sleep 7   # several reconnect attempts at reconnect_interval_ms=2000
cc2() { docker exec "$PROBE" python3 -I /app/ccprobe.py "http://$CC2_HTTP_IP:8080" "$@"; }
if cc2 wait 5 "link(s)!='started' and s['link']['connection_seq']==0 and s['link']['last_rx_at'] is None \
     and s['link']['gi']=={'state':'none','trigger':None,'started_at':None,'last_complete_at':None,'next_due_at':None} \
     and [(e['kind'],e['detail']) for e in j['entries']]==[('link_down',{'peer':'$GPS_IP:2404','connection_seq':0,'reason':'connect_failed'})] \
     and all(p['value'] is None and p['stale_reason']=='never_received' and p['quality']['iv'] and p['quality']['nt'] \
       and p['asdu'] is None and p['received_at'] is None and p['cot'] is None and p['source_time'] is None for p in s['points'])" \
  >"$OUT/cc2-state-never.json" && cc2 cmd open | grep -q '^503 {"error":"link not started"}$'; then
  pass "CC: unreachable RTU -> exactly one link_down connect_failed, points never_received (null value), 503"
else
  fail "CC: never-connected state (cc2-state-never.json)"
fi
docker stop -t 5 "$CC2" >/dev/null
if [ "$(docker inspect -f '{{.State.ExitCode}}' "$CC2")" = 0 ]; then
  pass "CC: exits 0 on SIGTERM"
else
  fail "CC: SIGTERM exit code $(docker inspect -f '{{.State.ExitCode}}' "$CC2")"
fi
docker logs "$CC2" >"$OUT/cc2.log" 2>&1
docker rm -f "$CC2" >/dev/null

log "start the control centre"
ctl "http://$BRK_IP:8081/reset"
start_cc "$CC" "$CC_IP" "$CC_HTTP_IP" "$RTU_IP:2404"

# ---- CC 1. connect, STARTDT, GI, all four points GOOD -----------------
log "CC check 1: GI complete, four points GOOD with the device values"
if probe wait 30 "link(s)=='started' and s['link']['gi']['state']=='complete' and s['link']['connection_seq']>=1 \
    and all_good(s) and ${POS}['value']=='closed' and ${TRIP}['value'] is False \
    and abs(pt(s,'feeder-current')['value']-28.9)<0.01 and abs(pt(s,'feeder-voltage')['value']-20.0)<0.01 \
    and ${POS}['quality']['ov'] is None and pt(s,'feeder-current')['quality']['ov'] is False \
    and pt(s,'feeder-current')['source_time'] is None and pt(s,'feeder-current')['source_time_invalid'] is None \
    and kinds(j)[:2]==['link_up','gi_started'] and last(j,'gi_complete')['detail']['points_good']==31" \
  >"$OUT/cc-state-baseline.json"; then
  pass "CC: link started, GI complete, four points GOOD with correct values"
else
  fail "CC: baseline state not reached (cc-state-baseline.json)"
fi
probe get /api/health >"$OUT/cc-health.txt"
# the compose healthcheck uses busybox wget from the iec104 image
docker run --rm --network "$MGMT" "$IMG" wget -qO- "$CC_URL/api/health" >"$OUT/cc-health-wget.txt" 2>&1
if grep -q '^200 {"status":"ok","link":"started","historian":' "$OUT/cc-health.txt" \
  && grep -q '^{"status":"ok","link":"started","historian":' "$OUT/cc-health-wget.txt"; then
  pass "CC: /api/health reports the started link (also to busybox wget)"
else
  fail "CC: /api/health unexpected (cc-health.txt)"
fi

# ---- CC 2. SBO commands: open, close, no-op close ---------------------
log "CC check 2: open, close and a no-op close complete with COT 11 feedback"
if cmd_ok cc-cmd-open open open \
  && probe wait 10 "${POS}['value']=='open' and ${POS}['cot'] in (11,20) and good($POS)  and last(j,'command')['detail']['result']=='completed'" >/dev/null; then
  pass "CC: open -> completed, feedback matched, cache open with COT 11 and an RTU observation time"
else
  fail "CC: open command (cc-cmd-open.json)"
fi
if cmd_ok cc-cmd-close close closed && probe wait 10 "${POS}['value']=='closed'" >/dev/null; then
  pass "CC: close -> completed, feedback matched closed"
else
  fail "CC: close command (cc-cmd-close.json)"
fi
if cmd_ok cc-cmd-noop close closed; then
  pass "CC: no-op close -> completed with COT 11 feedback"
else
  fail "CC: no-op command (cc-cmd-noop.json)"
fi

# ---- CC 3. 409 while a command is in flight ---------------------------
log "CC check 3: a second command while one is in flight -> 409"
ctl "http://$BRK_IP:8081/delay?ms=700"
probe overlap 0.2 >"$OUT/cc-overlap.txt"
ctl "http://$BRK_IP:8081/delay?ms=0"
if grep -q '^first 200 .*"result":"completed"' "$OUT/cc-overlap.txt" \
  && grep -q '^second 409 {"error":"a command is in flight"}' "$OUT/cc-overlap.txt"; then
  pass "CC: overlapping command answered 409; the first completed"
else
  fail "CC: overlap (cc-overlap.txt)"
fi
cmd_ok cc-cmd-restore close closed || fail "CC: could not restore closed (cc-cmd-restore.json)"

# ---- CC 4. interlock -> rejected --------------------------------------
log "CC check 4: close under lockout -> rejected"
cmd_ok cc-cmd-open2 open open || fail "CC: open before the interlock check (cc-cmd-open2.json)"
ctl "http://$BRK_IP:8081/fault?active=1"
probe wait 10 "${TRIP}['value'] is True and ${TRIP}['cot'] in (3,20)" >/dev/null || fail "CC: protection trip not seen"
cc_cmd cc-cmd-interlock close
if [ "$(cat "$OUT/cc-cmd-interlock.status")" = 200 ] && jtest "$OUT/cc-cmd-interlock.json" \
  "d['result']=='rejected' and d['selected_at'] and d['executed_at'] and d['con_at'] \
   and d['term_at'] is None and d['feedback'] is None and d['reason']"; then
  pass "CC: close under lockout -> rejected (negative execute ACT-CON)"
else
  fail "CC: interlock result (cc-cmd-interlock.json)"
fi
ctl "http://$BRK_IP:8081/reset"
probe wait 10 "all_good(s) and ${POS}['value']=='closed' and ${TRIP}['value'] is False" >/dev/null \
  || fail "CC: state did not follow the breaker reset"

# ---- CC 5. 400 / 404 / 405 / 411 / 413 / 431 / 501 / 505 --------------
log "CC check 5: bad input"
if probe badinput >"$OUT/cc-badinput.txt"; then
  pass "CC: every malformed request answered with the expected 4xx/5xx and an error body"
else
  fail "CC: bad-input handling (cc-badinput.txt)"
fi

# ---- CC 6. measurements invalid -> floats measurement_invalid ---------
log "CC check 6: measurements invalid -> floats IV measurement_invalid"
ctl "http://$BRK_IP:8081/mvalid?valid=0"
if probe wait 10 "link(s)=='started' and good($POS) and good($TRIP) and all(pt(s,i)['stale_reason']=='measurement_invalid' \
     and pt(s,i)['quality']['iv'] and not pt(s,i)['quality']['nt'] for i in FLOATS) \
     and abs(pt(s,'feeder-current')['value']-28.9)<0.01" >"$OUT/cc-state-minvalid.json"; then
  pass "CC: floats IV measurement_invalid with their last values, status points GOOD"
else
  fail "CC: measurement-invalid state (cc-state-minvalid.json)"
fi
ctl "http://$BRK_IP:8081/mvalid?valid=1"
probe wait 10 "all_good(s) and last(j,'point_good')['detail']['point'] in FLOATS" >/dev/null \
  || fail "CC: floats did not recover after measurements became valid"

# ---- CC 7. Modbus loss -> device_unreachable, link still started ------
log "CC check 7: Modbus loss -> device_unreachable with the link started"
docker pause "$BRK" >/dev/null
if probe wait 15 "link(s)=='started' and all_stale(s,'device_unreachable')" >"$OUT/cc-state-modbusloss.json"; then
  pass "CC: four points IV+NT device_unreachable, link started"
else
  fail "CC: Modbus-loss state (cc-state-modbusloss.json)"
fi
docker unpause "$BRK" >/dev/null
probe wait 15 "all_good(s)" >/dev/null || fail "CC: points did not recover after Modbus came back"

# ---- CC 8. RTU paused -> link_lost; back -> new connection, GI --------
log "CC check 8: RTU paused -> t1 timeout, link_lost cache, 503; unpaused -> recovery"
probe wait 5 "all_good(s)" >"$OUT/cc-state-before-pause.json"
docker pause "$RTU" >/dev/null
if probe wait 40 "link(s)!='started' and all_stale(s,'link_lost')" >"$OUT/cc-state-linklost.json" \
  && docker exec -i "$PROBE" python3 -I -c '
import json, sys
before, after = (json.loads(l) for l in sys.stdin.read().split("\n", 1))
keys = ("value", "received_at", "source_time", "source_time_invalid", "cot")
pb = {p["id"]: p for p in before["points"]}
sys.exit(0 if all(all(p[k] == pb[p["id"]][k] for k in keys) for p in after["points"]) else 1)' \
    < <(cat "$OUT/cc-state-before-pause.json" "$OUT/cc-state-linklost.json"); then
  pass "CC: link down, four points IV+NT link_lost with value and times preserved"
else
  fail "CC: link-lost cache (cc-state-before-pause.json vs cc-state-linklost.json)"
fi
if probe wait 5 "(lambda d: d is not None and d['detail']['reason']=='t1_timeout' \
     and d['detail']['peer']=='$RTU_IP:2404' and set(d['detail'])=={'peer','connection_seq','reason'} \
     and sorted(e['detail']['point'] for e in j['entries'] if e['kind']=='point_stale' and e['seq']>d['seq'] \
       and e['detail']['stale_reason']=='link_lost')==sorted(p['id'] for p in s['points']))(last(j,'link_down'))" >/dev/null; then
  pass "CC: journal link_down (t1_timeout) then point_stale link_lost for all four points"
else
  fail "CC: link-down journal"
fi
cc_cmd cc-cmd-linkdown open
probe get /api/health >"$OUT/cc-health-down.txt"
if [ "$(cat "$OUT/cc-cmd-linkdown.status")" = 503 ] && grep -q '"error":"link not started"' "$OUT/cc-cmd-linkdown.json" \
  && grep -Eq '^200 \{"status":"ok","link":"(connecting|disconnected)","historian":' "$OUT/cc-health-down.txt"; then
  pass "CC: command with the link down -> 503; health stays 200"
else
  fail "CC: link-down command/health (cc-cmd-linkdown.json, cc-health-down.txt)"
fi
docker unpause "$RTU" >/dev/null
if probe wait 40 "link(s)=='started' and s['link']['gi']['state']=='complete' and all_good(s) \
     and last(j,'link_up')['seq']>last(j,'link_down')['seq'] \
     and last(j,'link_up')['detail']['connection_seq']>last(j,'link_down')['detail']['connection_seq'] \
     and last(j,'gi_complete')['detail']['connection_seq']==last(j,'link_up')['detail']['connection_seq'] \
     and s['link']['connection_seq']==last(j,'link_up')['detail']['connection_seq']" >"$OUT/cc-state-recovered.json"; then
  pass "CC: RTU back -> link_up with a higher connection_seq, GI complete, four points GOOD"
else
  fail "CC: recovery after the RTU pause (cc-state-recovered.json)"
fi

# ---- CC 9. RTU stopped -> peer_closed; started -> recovery ------------
log "CC check 9: RTU stopped -> peer_closed; restarted -> recovery"
docker stop -t 5 "$RTU" >/dev/null
if probe wait 20 "link(s)!='started' and all_stale(s,'link_lost') and last(j,'link_down')['detail']['reason']=='peer_closed'" \
  >/dev/null; then
  pass "CC: RTU stop -> link_down peer_closed, four points link_lost"
else
  fail "CC: RTU-stop link loss"
fi
docker start "$RTU" >/dev/null
if probe wait 40 "link(s)=='started' and all_good(s) and ${POS}['value']=='closed' \
     and last(j,'link_up')['seq']>last(j,'link_down')['seq']" >/dev/null; then
  pass "CC: RTU restart -> link up, points GOOD through fresh reports"
else
  fail "CC: recovery after the RTU restart"
fi

# ---- CC 10. journal integrity ------------------------------------------
log "CC check 10: journal shape and ordering"
probe get "/api/journal?after=0" | cut -d' ' -f2- >"$OUT/cc-journal.json"
probe get "/api/journal?after=5" | cut -d' ' -f2- >"$OUT/cc-journal-after5.json"
probe get /api/state | cut -d' ' -f2- >"$OUT/cc-state-final.json"
epoch="$(jget "$OUT/cc-state-baseline.json" "d['cache_epoch']")"
details="{'link_up':{'peer','connection_seq'},'link_down':{'peer','connection_seq','reason'},'gi_started':{'connection_seq','trigger'}, \
  'gi_complete':{'connection_seq','points_good','trigger'},'point_stale':{'point','stale_reason','value'},'point_good':{'point','value'}, \
  'status_event':{'id','ioa','asdu','cot','value','source_time','source_time_invalid'},'command':{'id','point','operation','result','reason','selected_at','executed_at','con_at','term_at','feedback'}}"
if jtest "$OUT/cc-journal.json" "d['cache_epoch']=='$epoch' and d['oldest_seq']==1 \
     and d['latest_seq']==d['entries'][-1]['seq'] and [e['seq'] for e in d['entries']]==list(range(1,d['latest_seq']+1)) \
     and all(set(e)=={'seq','at','kind','detail'} and set(e['detail'])==${details}[e['kind']] for e in d['entries'])" \
  && jtest "$OUT/cc-journal-after5.json" "d['entries'][0]['seq']==6" \
  && jtest "$OUT/cc-state-final.json" "d['cache_epoch']=='$epoch'"; then
  pass "CC: journal seqs contiguous from 1, one cache_epoch, details exactly as specified, ?after= filters"
else
  fail "CC: journal integrity (cc-journal.json)"
fi
docker logs "$CC" >"$OUT/cc.log" 2>&1
