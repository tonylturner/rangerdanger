#!/usr/bin/env bash
# CP56 validity belongs to the timed event, not a later untimed GI refresh.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
command -v tshark >/dev/null 2>&1 || { printf 'tshark is required\n' >&2; exit 1; }

pcap() {
  python3 -I "$T_DIR/make_iec104_pcap.py" "$TMP_DIR/timed.pcap" "$@" || exit 1
}
# Element = DIQ followed by CP56 (milliseconds, minute/IV, hour, day, month, year).
invalid="31/11/1/1001/01000080010f0a1a"
synced="31/3/1/1001/02000000010f0a1a"
pcap "reply:$RTU_IP>$CC_IP=$invalid" "reply:$RTU_IP>$CC_IP=3/20/1/1001/01"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 1 1
t_expect_fails 0 "timed invalid event survives an untimed GI overwrite"
pcap "reply:$RTU_IP>$CC_IP=$synced"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 2 0
t_expect_fails 0 "synchronized timed close"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 2 1
t_expect_fails 1 "wrong clock validity fails"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 1 0
t_expect_fails 1 "wrong position fails"
pcap "reply:$RTU_IP>$CC_IP=3/20/1/1001/01"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 1 1
t_expect_fails 1 "an untimed GI is not a timed event"
pcap "$CC_IP>$RTU_IP=$invalid"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 1 1
t_expect_fails 1 "wrong direction fails"
pcap "reply:$RTU_IP>$CC_IP=31/11/1/1002/01000080010f0a1a;$synced"
t_run eu_check_timed_status "$TMP_DIR/timed.pcap" 1 1
t_expect_fails 1 "fields from different ASDUs cannot combine"
t_finish
