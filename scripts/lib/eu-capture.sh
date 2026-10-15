# shellcheck shell=bash
# Owned packet-capture windows and IEC104/Modbus evidence decoding.
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_stop_capture() {
  [ "$capture_running" -eq 1 ] || return 0
  if [ -z "$CAPTURE_PID" ] && [ -n "$CAPTURE_PID_FILE" ]; then
    local attempt
    for ((attempt = 0; attempt < 20; attempt++)); do
      CAPTURE_PID=$(docker exec "${CAPTURE_HOST:-$FW}" cat "$CAPTURE_PID_FILE" 2>/dev/null) || CAPTURE_PID=""
      [[ "$CAPTURE_PID" =~ ^[0-9]+$ ]] && break
      sleep 0.1
    done
  fi
  if [ -z "$CAPTURE_PID" ] || ! docker exec "${CAPTURE_HOST:-$FW}" sh -c "kill -INT '$CAPTURE_PID'" \
    >/dev/null 2>"$TMP_DIR/capture-kill.err"; then
    fail "could not stop owned tcpdump capture: $(cat "$TMP_DIR/capture-kill.err")"
    return 1
  fi
  local i
  for ((i = 0; i < 40; i++)); do
    if ! docker exec "${CAPTURE_HOST:-$FW}" sh -c "kill -0 '$CAPTURE_PID'" >/dev/null 2>&1; then
      capture_running=0
      return 0
    fi
    sleep 0.25
  done
  fail "owned tcpdump process $CAPTURE_PID did not stop after SIGINT"
  return 1
}
# eu_iec104_command_filter CA IOA SE: CC-to-RTU frames carrying a C_DC_NA_1
# (TypeID 46) activation for the breaker command with S/E = SE. The # layer
# operator binds the fields to one ASDU, so a segment carrying several APDUs
# cannot combine fields of different ASDUs; up to k = 12 APDUs per segment.
eu_iec104_command_filter() {
  local ca="$1" ioa="$2" se="$3" layer filter=""
  for layer in 1 2 3 4 5 6 7 8 9 10 11 12; do
    filter="$filter${filter:+ || }(iec60870_asdu.typeid#$layer == 46"
    filter="$filter && iec60870_asdu.causetx#$layer == 6 && iec60870_asdu.addr#$layer == $ca"
    filter="$filter && iec60870_asdu.ioa#$layer == $ioa && iec60870_asdu.dco.se#$layer == $se)"
  done
  printf 'ip.src == %s && ip.dst == %s && tcp.dstport == 2404 && (%s)' "$CC_IP" "$RTU_IP" "$filter"
}
# eu_check_iec104_command PCAP CA IOA SE NAME: at least one frame matches the
# filter. Frame existence avoids parsing tshark's Boolean or aggregated fields.
eu_check_iec104_command() {
  local pcap="$1" ca="$2" ioa="$3" se="$4" name="$5" frames
  if ! frames=$(tshark -r "$pcap" -Y "$(eu_iec104_command_filter "$ca" "$ioa" "$se")" \
    -T fields -e frame.number 2>"$TMP_DIR/tshark-iec104.err"); then
    fail "G3 IEC104 $name decode failed: $(cat "$TMP_DIR/tshark-iec104.err")"
    return 1
  fi
  if [ -z "$frames" ]; then
    fail "G3 capture has no C_DC_NA_1 TypeID 46 $name (COT 6, CA $ca, IOA $ioa) from $CC_IP to $RTU_IP:2404"
    return 1
  fi
  pass "G3 capture decodes C_DC_NA_1 $name (COT 6, CA $ca, IOA $ioa) from $CC_IP to $RTU_IP:2404 in frame(s) $(printf '%s' "$frames" | tr '\n' ' ')"
}
eu_decode_capture() {
  if [ ! -s "$TMP_DIR/eu-iec104-gate.pcap" ]; then
    fail "G3 packet capture is empty"
    return 1
  fi
  pass "G3 packet capture copied and non-empty"
  if tshark -r "$TMP_DIR/eu-iec104-gate.pcap" -Y '_ws.malformed' -T fields -e frame.number \
    >"$TMP_DIR/malformed.txt" 2>"$TMP_DIR/tshark.err"; then
    if [ ! -s "$TMP_DIR/malformed.txt" ]; then
      pass "G3 capture contains no Wireshark-marked malformed frame"
    else
      fail "G3 malformed frames found: $(cat "$TMP_DIR/malformed.txt")"
    fi
  else
    fail "G3 tshark malformed-frame decode failed: $(cat "$TMP_DIR/tshark.err")"
  fi
  local ca ioa
  ca=$(jq -e '.ca | numbers' "$PACKAGE/protocols/points.json" 2>/dev/null) || ca=""
  ioa=$(jq -e '[.commands[] | select(.id == "feeder-breaker-command") | .ioa | numbers]
    | if length == 1 then .[0] else error("one command IOA expected") end' \
    "$PACKAGE/protocols/points.json" 2>/dev/null) || ioa=""
  if [ -z "$ca" ] || [ -z "$ioa" ]; then
    fail "G3 points.json lacks the common address or the feeder-breaker-command IOA"
  else
    eu_check_iec104_command "$TMP_DIR/eu-iec104-gate.pcap" "$ca" "$ioa" 1 select
    eu_check_iec104_command "$TMP_DIR/eu-iec104-gate.pcap" "$ca" "$ioa" 0 execute
  fi
  if tshark -r "$TMP_DIR/eu-iec104-gate.pcap" -Y 'tcp.port == 502 && modbus.func_code == 6' \
    -T fields -e ip.src -e ip.dst -e modbus.func_code >"$TMP_DIR/modbus-decode.txt" \
    2>"$TMP_DIR/tshark-modbus.err"; then
    if awk -F '\t' '
      $1 == "10.30.30.20" && $2 == "10.40.40.20" && $3 == "6" { found = 1 }
      END { exit(found ? 0 : 1) }
    ' "$TMP_DIR/modbus-decode.txt"; then
      pass "G3 capture decodes Modbus FC6 on the RTU/breaker hop"
    else
      fail "G3 capture does not prove Modbus FC6 on the RTU/breaker hop"
    fi
  else
    fail "G3 Modbus tshark decode failed: $(cat "$TMP_DIR/tshark-modbus.err")"
  fi
}

# Captures are attempt-local. Copies are retained with API/audit logs on failure.
eu_check_timed_status() {
  local pcap="$1" position="$2" invalid="$3" layer filter="" frames
  for layer in 1 2 3 4 5 6 7 8 9 10 11 12; do
    filter="$filter${filter:+ || }(iec60870_asdu.typeid#$layer == 31"
    filter="$filter && (iec60870_asdu.causetx#$layer == 3 || iec60870_asdu.causetx#$layer == 11)"
    filter="$filter && iec60870_asdu.addr#$layer == 1 && iec60870_asdu.ioa#$layer == 1001"
    filter="$filter && iec60870_asdu.diq.dpi#$layer == $position && iec60870_asdu.cp56time.iv#$layer == $invalid)"
  done
  if ! frames=$(tshark -r "$pcap" -Y "ip.src == $RTU_IP && ip.dst == $CC_IP && tcp.srcport == 2404 && ($filter)" \
    -T fields -e frame.number 2>"$TMP_DIR/timed-status.err"); then
    fail "G2 timed status decode failed: $(cat "$TMP_DIR/timed-status.err")"; return 1
  fi
  if [ -z "$frames" ]; then
    fail "G2 no timed breaker event position=$position CP56-invalid=$invalid in $pcap"; return 1
  fi
  pass "G2 actual timed breaker event position=$position CP56-invalid=$invalid in frame(s) $(printf '%s' "$frames" | tr '\n' ' ')"
}

eu_window_start() {
  local label="$1" peer="$FW" image platform
  case "$label" in G11|G17) peer="$RTU" ;; esac
  CAPTURE_HOST="rdinc4-i-window-$$"
  CAPTURE_PATH=/tmp/window.pcap CAPTURE_PID=1
  CAPTURE_PID_FILE="" CAPTURE_LOG=""
  # shellcheck disable=SC2034 # read by shared capture teardown
  capture_running=1
  image=$(docker inspect -f '{{.Image}}' "$FW") || return
  platform=$(docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image") || return
  # Reuse the already-running image, without pulling/tagging it. Sharing the
  # RTU namespace is necessary to observe its same-subnet PLC polls too.
  if ! docker run -d --platform "$platform" --name "$CAPTURE_HOST" --network "container:$peer" \
    --cap-add NET_RAW --entrypoint tcpdump "$image" --immediate-mode -i any -s 0 -U -w "$CAPTURE_PATH" \
    'tcp port 2404 or tcp port 502 or tcp port 8080 or tcp port 8081' >"$TMP_DIR/$label-capture.id"; then
    fail "$label cannot start owned capture container"; return 1
  fi
  sleep 1
}
eu_window_finish() {
  local label="$1"
  eu_stop_capture || return
  docker cp "$CAPTURE_HOST:$CAPTURE_PATH" "$TMP_DIR/$label.pcap" >/dev/null || {
    fail "$label cannot retrieve attempt capture"; return 1;
  }
  docker rm "$CAPTURE_HOST" >/dev/null || return
  CAPTURE_HOST=""
  # shellcheck disable=SC2034 # shared with capture teardown
  CAPTURE_PATH="" CAPTURE_PID_FILE="" CAPTURE_LOG="" CAPTURE_PID=""
}

# Poll requests, not CC/GI refreshes, measure the independent RTU scheduler.
eu_peer_poll_gaps() {
  local pcap="$1" since="$2" until="$3"
  tshark -r "$pcap" -Y 'ip.src == 10.30.30.20 && tcp.dstport == 502 && (modbus.func_code == 3 || modbus.func_code == 4)' \
    -T fields -e frame.time_epoch -e ip.dst -e tcp.seq_raw >"$TMP_DIR/polls.tsv" 2>"$TMP_DIR/polls.err" || {
    fail "G11 poll decode failed: $(cat "$TMP_DIR/polls.err")"; return 1;
  }
  if python3 - "$TMP_DIR/polls.tsv" "$PACKAGE/protocols/points.json" "$since" "$until" <<'PY'
import json, sys
path, points, start, end = sys.argv[1:]
start, end = float(start), float(end)
config = json.load(open(points))
for device in config['devices']:
    if device['id'] == 'feeder-breaker':
        continue
    ip = device['host']
    rows, seen = [], set()
    for line in open(path):
        stamp, dst, seq = line.strip().split('\t')
        stamp = float(stamp)
        if dst == ip and start <= stamp <= end and (dst, seq) not in seen:
            rows.append(stamp)
            seen.add((dst, seq))
    limit = (device['poll_ms'] + 250) / 1000
    gaps = [b-a for a,b in zip([start]+rows, rows+[end])]
    assert len(rows) >= 2 and max(gaps) <= limit, (device['id'], len(rows), max(gaps), limit)
    print(f"{device['id']}: {len(rows)} fresh polls; max gap {max(gaps):.3f}s <= {limit:.3f}s")
PY
  then pass "G11 every healthy peer poll gap <= poll_ms + 250ms during dropped breaker conduit"
  else fail "G11 healthy-peer protocol freshness failed (polls.tsv)"; return 1; fi
}
