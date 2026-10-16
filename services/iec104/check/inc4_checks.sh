# shellcheck shell=bash
# Increment-4 checks, sourced with the existing throwaway RTU and CC.
docker network connect --ip 172.28.104.40 "$NET" "$PROBE"
inc4_probe() { docker exec "$PROBE" python3 /app/inc4_probe.py "$@"; }
if inc4_probe main >"$OUT/inc4-probe.log" 2>&1; then
  pass "increment 4: schema, RMU/OLTC SBO, transient VTI, budgets, ambiguous write, heartbeat, historian"
else
  fail "increment 4 (inc4-probe.log)"
fi

log "increment 4: real DROP of one device's Modbus conduit, healthy poll-gap evidence"
docker exec "$RTU" iptables -A OUTPUT -d 172.28.104.21 -p tcp --dport 502 -j DROP
if inc4_probe drop >"$OUT/inc4-drop.log" 2>&1; then
  pass "one DROPped device never starved a healthy peer beyond poll_ms + 250 ms"
else
  fail "DROP/healthy peer poll gaps (inc4-drop.log)"
fi
docker exec "$RTU" iptables -D OUTPUT -d 172.28.104.21 -p tcp --dport 502 -j DROP
probe wait 10 "all_good(s) and good(pt(s,'rmu-1-position'))" >/dev/null || fail "RMU DROP recovery"

log "increment 4: freshness refusal before select (still-started link)"
docker pause "$RTU" >/dev/null
if inc4_probe stale >"$OUT/inc4-stale.log" 2>&1; then
  pass "stale feedback refused with 409 before select while link remained started"
else
  fail "stale feedback refusal (inc4-stale.log)"
fi
docker unpause "$RTU" >/dev/null
probe wait 15 "all_good(s)" >/dev/null || fail "post-pause recovery"

docker rm -f "$MON" >/dev/null
docker run -d --name "$MON" --network "$NET" "$IMG" iec104cmd "$RTU_IP" monitor --json --hold 30 >/dev/null
sleep 1
cli sc 3302 off -sbo >"$OUT/inc4-cli-mode.log" 2>&1 || fail "iec104cmd SC manual"
cli rc 3301 raise -sbo >"$OUT/inc4-cli-raise.log" 2>&1 || fail "iec104cmd RC raise"
cli rc 3301 lower -sbo >"$OUT/inc4-cli-lower.log" 2>&1 || fail "iec104cmd RC lower"
snap >"$OUT/inc4-cli-step.jsonl"
docker logs "$MON" >"$OUT/inc4-cli-step-spont.jsonl" 2>/dev/null
if grep -q '"type":5,.*"ioa":1301,.*"transient":false.*"quality":"GOOD"' "$OUT/inc4-cli-step.jsonl"; then
  pass "iec104cmd rc lower/raise and M_ST_NA_1 JSON decoding"
else
  fail "iec104cmd step decoding"
fi
if grep -q '"type":32,"cot":3,"ioa":1301,.*"transient":true.*"quality":"GOOD"' "$OUT/inc4-cli-step-spont.jsonl" \
  && grep -q '"type":32,"cot":11,"ioa":1301,.*"transient":false' "$OUT/inc4-cli-step-spont.jsonl"; then
  pass "iec104cmd M_ST_TB_1 moving spontaneous and settled COT 11 decoding"
else
  fail "iec104cmd timed step decoding"
fi

log "timed historian push: immediate untimed GI and bounded overflow"
docker exec "$PROBE" python3 /app/timed_push.py >"$OUT/timed-push.log" 2>&1 &
timed_probe_pid=$!
start_cc "$CC2" "$CC2_IP" "$CC2_HTTP_IP" "172.28.104.40:2404"
if wait "$timed_probe_pid"; then
  pass "timed historian snapshots survive immediate GI; bounded oldest-drop overflow"
else
  fail "timed historian snapshots (timed-push.log)"
fi
docker rm -f "$CC2" >/dev/null
