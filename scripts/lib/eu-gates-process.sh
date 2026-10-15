# shellcheck shell=bash
# RMU interlocks, OLTC regulation and PLC scan-thread failure gates (G15-G17).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_fresh_points() {
  local predicate="$1" since="$2" budget="$3" label="$4"
  eu_wait_for_operator "def point(\$id): [.points[] | select(.id == \$id and good
    and (.received_at | epoch) >= (\"$since\" | epoch))]
    | if length == 1 then .[0] else error(\"no fresh good observation\") end;
    ($predicate)" "$budget" "$label" || return
  pass "$label: fresh protocol observations"
}

eu_gate_G15() {
  local section expected since
  eu_apply_named improved || return
  eu_reset_all || return
  eu_management rmu-1 POST /api/sim/actions '{"action":"earth_close"}' || return
  if [ "$HTTP_STATUS" = 409 ] && printf '%s' "$API_BODY" | jq -e '.reason | type == "string" and length > 0' >/dev/null; then
    pass "G15 earth_close refused on a closed/energised way: $API_BODY"
  else fail "G15 unsafe earth_close not refused: HTTP $HTTP_STATUS $API_BODY"; return 1; fi
  eu_command rmu-2-command close rejected live_live || return
  eu_action rmu-1 set_local || return
  eu_wait_for_operator 'point("rmu-1-remote").value == false and (point("rmu-1-remote") | good)' 10 "G15 local observed" || return
  eu_command rmu-1-command open rejected not_remote || return
  eu_action rmu-1 set_remote || return
  eu_command rmu-1-command open completed || return
  eu_wait_for_field '.devices["rmu-1"].measurements.valid and .process.buses.s2.live == false' 10 "G15 isolated S2 solve" || return
  eu_action rmu-1 earth_close || return
  eu_command rmu-1-command close rejected earth_closed || return
  eu_action rmu-1 earth_open || return
  eu_command rmu-1-command close completed || return
  for section in s1 s2; do
    eu_reset_all || return
    since=$(eu_utc)
    expected=false; [ "$section" != s2 ] || expected=true
    eu_action process "fault_$section" || return
    eu_wait_for_field ".devices[\"breaker-1\"].protection_tripped and .devices[\"breaker-1\"].lockout
      and .devices[\"rmu-1\"].fault_passage == $expected and .devices[\"rmu-1\"].measurements.valid
      and .process.buses.s2.live == false" 10 "G15 $section trip/FPI and solve" || return
    eu_fresh_points "point(\"feeder-protection-trip\").value and point(\"rmu-1-fault-passage\").value == $expected" \
      "$since" 10 "G15 $section protection/FPI via Modbus/IEC104" || return
    eu_command rmu-1-command open completed || return
    eu_wait_for_field '.devices["rmu-2"].measurements.valid and .devices["rmu-2"].measurements.section_live == false' 10 "G15 tie ready" || return
    since=$(eu_utc)
    eu_command rmu-2-command close completed || return
    if [ "$section" = s1 ]; then
      eu_wait_for_field '.process.buses.s2.live and .process.buses.s3.live and .process.f2_tripped == false' 10 "G15 S1 tie restores S2" || return
      eu_fresh_points 'point("rmu-2-section-kv").value > 18 and point("rmu-2-source-kv").value > 18' "$since" 10 "G15 restored voltage via protocols" || return
      eu_command feeder-breaker-command close rejected lockout || return
      eu_action process clear_fault || return
      eu_action breaker-1 reset_lockout || return
      eu_command rmu-1-command close completed || return
      eu_wait_for_field '.devices["breaker-1"].measurements.valid and .process.buses.s1.live' 10 "G15 reverse feed S1 through tie" || return
      eu_command feeder-breaker-command close rejected live_live || return
    else
      eu_wait_for_field '.process.f2_tripped and .process.buses.s2.live == false and .process.buses.s3.live == false' 10 "G15 S2 backfeed trips F2" || return
      eu_fresh_points 'point("rmu-1-fault-passage").value and point("rmu-2-fault-passage").value
        and point("rmu-2-source-kv").value < 0.1' "$since" 10 "G15 S2 FPI and S3 loss via protocols" || return
    fi
  done
  eu_reset_all
}
eu_gate_G16() {
  local cursor generation since source tap count budget
  eu_reset_all || return
  eu_command oltc-1-mode manual completed || return
  eu_command oltc-1-step raise completed || return
  eu_command oltc-1-step lower completed || return
  eu_action oltc-1 set_local || return
  eu_command oltc-1-mode auto rejected not_remote || return
  eu_action oltc-1 set_remote || return
  eu_action oltc-1 block || return
  eu_command oltc-1-step raise rejected blocked || return
  eu_action oltc-1 unblock || return
  eu_command oltc-1-mode auto completed || return
  for source in load 0.90 1.10; do
    eu_reset_all || return
    eu_wait_for_field '.devices["oltc-1"].armed and .devices["oltc-1"].tap == 0' 10 "G16 armed nominal" || return
    eu_management oltc-1 GET '/api/audit?after=0' || return
    cursor=$(printf '%s' "$API_BODY" | jq -r '.latest_seq // 0')
    generation=$(printf '%s' "$API_BODY" | jq -r '.generation')
    since=$(eu_utc)
    case "$source" in
      load) tap=1; count=1; budget=35; eu_action process set_load 3.0 || return ;;
      0.90) tap=8; count=8; budget=130; eu_action process set_source "$source" || return ;;
      1.10) tap=-7; count=7; budget=120; eu_action process set_source "$source" || return ;;
    esac
    eu_fresh_points "point(\"oltc-1-tap\").value == {value:$tap,transient:false}
      and (point(\"oltc-1-tap\") | good)" "$since" "$budget" "G16 $source reaches tap $tap" || return
    sleep 15
    eu_plc_requests "$cursor" "$count" "$generation" || return
    if [ "$source" = 0.90 ]; then
      eu_fresh_points 'point("oltc-1-at-upper").value and point("oltc-1-bus-kv").value < 19.8' "$since" 10 "G16 upper limit remains undervoltage" || return
      eu_command oltc-1-mode manual completed || return
      eu_command oltc-1-step raise rejected limit || return
      eu_command oltc-1-step lower completed || return
    else
      eu_fresh_points "point(\"oltc-1-tap\").value == {value:$tap,transient:false}
        and point(\"oltc-1-bus-kv\").value >= 19.8 and point(\"oltc-1-bus-kv\").value <= 20.2" \
        "$since" 10 "G16 $source settles in band without extra steps" || return
      if [ "$source" = 1.10 ]; then
        eu_command oltc-1-mode manual completed || return
        eu_command oltc-1-step lower completed || return
        eu_command oltc-1-step lower rejected limit || return
        eu_command oltc-1-step raise completed || return
      fi
    fi
  done
  eu_reset_all
}

eu_stop_plc_scan() {
  [ -n "$PLC_SCAN_HELPER" ] || return 0
  if ! docker stop -t 3 "$PLC_SCAN_HELPER" >/dev/null; then
    fail "G17 cannot stop owned PLC tracer $PLC_SCAN_HELPER"; return 1
  fi
  docker logs "$PLC_SCAN_HELPER" >"$TMP_DIR/G17-scan.log" 2>&1 || return
  docker rm "$PLC_SCAN_HELPER" >/dev/null || return
  PLC_SCAN_HELPER=""
  if grep -q '"event": "scan_frozen"' "$TMP_DIR/G17-scan.log" \
    && ! grep -q '"event": "scan_resumed"' "$TMP_DIR/G17-scan.log"; then
    fail "G17 tracer did not confirm scan resumed: $(cat "$TMP_DIR/G17-scan.log")"; return 1
  fi
}
eu_gate_G17() {
  local image platform attempt frozen_at until
  eu_reset_all || return
  eu_window_start G17 || return
  image=$(docker inspect -f '{{.Image}}' rangerdanger-openplc) || return
  platform=$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image") || return
  PLC_SCAN_HELPER="rdinc4-i-scan-$$"
  if ! docker run -d --platform "$platform" --name "$PLC_SCAN_HELPER" --network none --pid container:rangerdanger-openplc \
    --cap-add SYS_PTRACE --entrypoint python3 "$image" -c "$(cat "$SCRIPT_DIR/tests/eu-gate/freeze-plc-scan.py")" \
    >"$TMP_DIR/G17-tracer.id"; then
    fail "G17 cannot start scan-thread fault injector"; return 1
  fi
  for ((attempt=0; attempt<30; attempt++)); do
    docker exec "$PLC_SCAN_HELPER" test -s /tmp/scan-frozen >/dev/null 2>&1 && break
    sleep 0.1
  done
  if [ "$attempt" -eq 30 ]; then
    docker logs "$PLC_SCAN_HELPER" >"$TMP_DIR/G17-scan.log" 2>&1
    fail "G17 scan freeze unavailable: $(cat "$TMP_DIR/G17-scan.log")"; return 1
  fi
  frozen_at=$(python3 -c 'import time; print(time.time())')
  if eu_wait_for_operator '.link.state == "started"
    and ([.points[] | select(.id | startswith("plc-1-"))] | length == 5
      and all(.[]; .quality.iv and .stale_reason == "heartbeat"))
    and all(.points[] | select(.id | startswith("plc-1-") | not); good)' 5 "G17 responsive PLC heartbeat staleness"; then
    pass "G17 all five PLC points IV heartbeat while every healthy peer remains GOOD"
  fi
  until=$(python3 -c 'import time; print(time.time())')
  eu_window_finish G17 || return
  eu_stop_plc_scan || return
  if tshark -r "$TMP_DIR/G17.pcap" -Y 'ip.src == 10.30.30.30 && tcp.srcport == 502 && modbus.func_code == 3 && modbus.byte_cnt == 12' \
    -T fields -e frame.time_epoch -e modbus.regval_uint16 >"$TMP_DIR/G17-registers.tsv" 2>"$TMP_DIR/G17-decode.err" \
    && python3 - "$TMP_DIR/G17-registers.tsv" "$frozen_at" "$until" <<'PYHEARTBEAT'
import sys
rows = [line.strip().split('\t') for line in open(sys.argv[1])]
values = [v.split(',') for t,v in rows if float(sys.argv[2]) <= float(t) <= float(sys.argv[3])]
assert len(values) >= 3 and all(len(v) == 6 for v in values), values
assert len({v[-1] for v in values}) == 1, values
print(f"{len(values)} real FC3 responses with a constant heartbeat: {values[0][-1]}")
PYHEARTBEAT
  then pass "G17 protocol capture proves responsive PLC FC3 with frozen heartbeat (no replacement server)"
  else fail "G17 no responsive, constant-heartbeat PLC protocol evidence"; fi
  if eu_wait_for_operator 'all(.points[]; good)' 10 "G17 resumed scan heartbeat recovery"; then
    pass "G17 PLC heartbeat observations recover through Modbus/IEC104"
  fi
}
