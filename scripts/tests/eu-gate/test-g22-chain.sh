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
t_finish
