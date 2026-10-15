# shellcheck shell=bash
# Historian continuity and ingest/solver boundary gates (G18-G19).
# Loaded by eu-iec104-smoke.sh; sourcing only defines helpers/gates.

eu_gate_G18() {
  local since paused resumed until epoch
  eu_reset_all || return
  since=$(eu_utc)
  eu_api_get /api/historian/points || return
  eu_assert_jq "$API_BODY" 'length == 31' "G18 historian exposes 31 points"
  eu_get_operator || return
  epoch=$(printf '%s' "$API_BODY" | jq -r '.cache_epoch')
  eu_window_start G18 || return
  cc_paused=1
  docker pause "$CC" >/dev/null || { fail "G18 CC pause failed"; return 1; }
  # One push may already be in flight (contract budget 500ms). Drain it
  # before measuring the genuine ingest-silence window.
  sleep 1
  paused=$(eu_utc)
  sleep 5
  # End the confirmed-paused window before starting unpause: an ingest can
  # legitimately arrive before the Docker CLI has returned.
  resumed=$(eu_utc)
  eu_unpause_owned "$CC" cc "$cc_paused" || return
  sleep 3
  until=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  eu_api_get "/api/historian/history?point=feeder-current&since=$since&until=$until" || return
  if eu_historian_gap "$API_BODY" "$epoch" "$paused" "$resumed"; then
    pass "G18 real CC ingest resumes with schema-2 samples and an outage-spanning gap"
  else fail "G18 historian outage evidence failed ($paused to $resumed): $API_BODY"; fi
  # A valid snapshot on a read listener must not ingest, even from enterprise.
  eu_get_operator || return
  if docker exec "$KALI" curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -X POST \
    -H 'Content-Type: application/json' -d "$API_BODY" http://10.70.70.40:8080/api/ingest >"$TMP_DIR/read-ingest.status"; then
    case "$(cat "$TMP_DIR/read-ingest.status")" in
      404|405) pass "G18 read listener rejects real POST /api/ingest" ;;
      *) fail "G18 read listener accepted ingest: $(cat "$TMP_DIR/read-ingest.status")" ;;
    esac
  else fail "G18 enterprise read listener transport failed"; fi
  eu_window_finish G18 || return
  if tshark -r "$TMP_DIR/G18.pcap" -d tcp.port==8081,http \
    -Y 'ip.src == 10.60.60.20 && ip.dst == 10.70.70.40 && tcp.dstport == 8081 && http.request.method == "POST" && http.request.uri == "/api/ingest"' \
    -T fields -e frame.number >"$TMP_DIR/G18-ingest.frames" 2>"$TMP_DIR/G18-decode.err" && [ -s "$TMP_DIR/G18-ingest.frames" ]; then
    pass "G18 capture proves real CC POST /api/ingest over 8081 in this attempt"
  else fail "G18 no captured CC ingest operation (G18.pcap)"; fi
}

eu_gate_G19() {
  local container ip port rc path method body
  eu_get_operator || return
  body="$API_BODY"
  eu_apply_named improved || return
  eu_window_start G19 || return
  for container in "$KALI" "$RTU" rangerdanger-eng-ws; do
    for ip in 10.70.70.40 10.99.99.23 10.50.50.20; do
      # Only Kali probes solver surfaces; all three untrusted peers probe ingest.
      [ "$ip" = 10.70.70.40 ] || [ "$container" = "$KALI" ] || continue
      port=8080; path=/api/electrical; method=GET
      if [ "$ip" = 10.70.70.40 ]; then port=8081; path=/api/ingest; method=POST; fi
      if [ "$container" = "$RTU" ]; then
        docker exec "$container" wget -T 3 -O - --post-data="$body" "http://$ip:$port$path" \
          >"$TMP_DIR/G19-$container-$ip.out" 2>"$TMP_DIR/G19-$container-$ip.err"
        rc=$?
        if [ "$rc" = 1 ] && grep -Eq 'timed out|Connection refused' "$TMP_DIR/G19-$container-$ip.err"; then
          pass "G19 $container -> $ip:$port denied (wget $rc)"
        else fail "G19 $container -> $ip:$port not proven denied: wget $rc $(cat "$TMP_DIR/G19-$container-$ip.err")"; fi
      else
        docker exec "$container" curl -sS --connect-timeout 2 --max-time 3 -X "$method" \
          -H 'Content-Type: application/json' -d "$body" "http://$ip:$port$path" \
          >"$TMP_DIR/G19-$container-$ip.out" 2>"$TMP_DIR/G19-$container-$ip.err"
        rc=$?
        if [ "$rc" = 28 ] || [ "$rc" = 7 ]; then
          pass "G19 $container -> $ip:$port denied (curl $rc)"
        else fail "G19 $container -> $ip:$port not denied: curl $rc $(cat "$TMP_DIR/G19-$container-$ip.out")"; fi
      fi
    done
  done
  eu_window_finish G19 || return
  if tshark -r "$TMP_DIR/G19.pcap" -Y 'ip.src == 10.10.10.50 && tcp.flags.syn == 1 && tcp.flags.ack == 0' \
    -T fields -e ip.dst -e tcp.dstport >"$TMP_DIR/G19-syn.tsv" 2>"$TMP_DIR/G19-decode.err"; then
    for ip in 10.70.70.40 10.99.99.23 10.50.50.20; do
      port=8080; [ "$ip" != 10.70.70.40 ] || port=8081
      if grep -q "$(printf '%s\t%s' "$ip" "$port")" "$TMP_DIR/G19-syn.tsv"; then
        pass "G19 capture proves fresh Kali SYN to $ip:$port"
      else fail "G19 no fresh Kali SYN evidence to $ip:$port"; fi
    done
  else fail "G19 SYN capture decode failed"; fi
  if tshark -r "$TMP_DIR/G19.pcap" -d tcp.port==8081,http \
    -Y 'ip.src == 10.70.70.40 && ip.dst == 10.60.60.20 && tcp.srcport == 8081 && http.response.code == 200' \
    -T fields -e frame.number >"$TMP_DIR/G19-cc-ingest.frames" 2>"$TMP_DIR/G19-ingest.err" && [ -s "$TMP_DIR/G19-cc-ingest.frames" ]; then
    pass "G19 real CC ingest remains HTTP 200 over 8081 under improved"
  else fail "G19 improved has no successful CC ingest protocol operation"; fi
}
