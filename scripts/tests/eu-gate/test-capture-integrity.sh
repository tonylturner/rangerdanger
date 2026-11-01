#!/usr/bin/env bash
# Capture integrity rejects lost statistics, kernel drops, and truncated pcap
# records for both gate capture implementations.
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

make_pcap() {
  python3 - "$1" "$2" "$3" <<'PY'
import struct
import sys

path, included, original = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
with open(path, "wb") as capture:
    capture.write(b"\xd4\xc3\xb2\xa1")
    capture.write(struct.pack("<HHIIII", 2, 4, 0, 0, 65535, 1))
    capture.write(struct.pack("<IIII", 0, 0, included, original))
    capture.write(b"x" * included)
PY
}

make_pcap "$TMP_DIR/clean.pcap" 4 4
printf '1 packets captured\n1 packets received by filter\n0 packets dropped by kernel\n' \
  >"$TMP_DIR/clean.stats"
t_run eu_check_capture_integrity "$TMP_DIR/clean.pcap" "$TMP_DIR/clean.stats" clean
t_expect_fails 0 "a complete capture with zero kernel drops passes"
t_expect_out "zero kernel drops" "the clean capture reports integrity success"

printf '1 packets captured\n1 packets received by filter\n2 packets dropped by kernel\n' \
  >"$TMP_DIR/dropped.stats"
t_run eu_check_capture_integrity "$TMP_DIR/clean.pcap" "$TMP_DIR/dropped.stats" dropped
t_expect_fails 1 "a positive kernel drop count fails"
t_expect_out "2 packets dropped by kernel" "the drop count is reported"

rm -f "$TMP_DIR/missing.stats"
t_run eu_check_capture_integrity "$TMP_DIR/clean.pcap" "$TMP_DIR/missing.stats" missing
t_expect_fails 1 "missing tcpdump statistics fail"
t_expect_out "statistics are missing or incomplete" "missing statistics are reported"

make_pcap "$TMP_DIR/truncated.pcap" 4 8
t_run eu_check_capture_integrity "$TMP_DIR/truncated.pcap" "$TMP_DIR/clean.stats" truncated
t_expect_fails 1 "a record with incl_len below orig_len fails"
t_expect_out "incl_len 4 < orig_len 8" "the truncated record lengths are reported"

t_finish
