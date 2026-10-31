#!/usr/bin/env bash
# G3: TypeID 46 select and execute are proved by display-filter frame existence,
# whatever tshark prints for Booleans, and per ASDU inside multi-APDU segments.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if ! command -v tshark >/dev/null 2>&1; then
  printf 'tshark is not installed: the G3 decode cases were not run\n'
  exit 0
fi

CC_TO_RTU="$CC_IP>$RTU_IP"
SELECT="46/6/1/3001/81"
EXECUTE="46/6/1/3001/01"
pcap() {
  local name="$1"
  shift
  python3 -I "$T_DIR/make_iec104_pcap.py" "$TMP_DIR/$name.pcap" "$@" || exit 1
  printf '%s' "$TMP_DIR/$name.pcap"
}
check_both() {
  local file="$1" select_fails="$2" execute_fails="$3" label="$4"
  t_run eu_check_iec104_command "$file" 1 3001 1 select
  t_expect_fails "$select_fails" "$label: select"
  t_run eu_check_iec104_command "$file" 1 3001 0 execute
  t_expect_fails "$execute_fails" "$label: execute"
}

file=$(pcap separate "$CC_TO_RTU=$SELECT" "$CC_TO_RTU=$EXECUTE")
t_run tshark -r "$file" -T fields -e iec60870_asdu.dco.se
t_expect_out "True" "the local tshark prints S/E as a Boolean (the run-1 decoder compared it to 1/0)"
check_both "$file" 0 0 "select and execute in separate frames"

file=$(pcap multi "$CC_TO_RTU=$SELECT;$EXECUTE")
t_run tshark -r "$file" -T fields -e iec60870_asdu.typeid -e iec60870_asdu.dco.se
t_expect_out "46,46" "tshark aggregates the fields of a multi-APDU segment"
check_both "$file" 0 0 "select and execute in one multi-APDU segment"

file=$(pcap gi-first "$CC_TO_RTU=100/6/1/0/14;$SELECT" "$CC_TO_RTU=$EXECUTE")
check_both "$file" 0 0 "a command ASDU after a GI ASDU in the same segment"

file=$(pcap select-only "$CC_TO_RTU=$SELECT")
check_both "$file" 0 1 "select without execute"

file=$(pcap mixed-ioa "$CC_TO_RTU=46/6/1/3002/01;$SELECT")
check_both "$file" 0 1 "execute fields from a different-IOA ASDU are not combined"

file=$(pcap wrong-ioa "$CC_TO_RTU=46/6/1/3002/81" "$CC_TO_RTU=46/6/1/3002/01")
check_both "$file" 1 1 "commands to another IOA"

file=$(pcap wrong-ca "$CC_TO_RTU=46/6/2/3001/81" "$CC_TO_RTU=46/6/2/3001/01")
check_both "$file" 1 1 "commands to another common address"

file=$(pcap confirmations "reply:$RTU_IP>$CC_IP=46/7/1/3001/81" "reply:$RTU_IP>$CC_IP=46/7/1/3001/01")
check_both "$file" 1 1 "RTU-to-CC confirmations (COT 7) are not CC commands"

file=$(pcap single-command "$CC_TO_RTU=45/6/1/3001/81" "$CC_TO_RTU=45/6/1/3001/01")
check_both "$file" 1 1 "single commands (TypeID 45) are not C_DC_NA_1"

t_run eu_check_iec104_command "$TMP_DIR/missing.pcap" 1 3001 1 select
t_expect_fails 1 "an unreadable capture fails"
t_expect_out "decode failed" "an unreadable capture reports the tshark error"

t_finish
