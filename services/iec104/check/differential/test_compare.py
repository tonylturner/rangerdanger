# SPDX-License-Identifier: Apache-2.0
"""Capture mutation and end-to-end comparator tests."""

from __future__ import annotations

import json
import struct
import tempfile
import unittest
from pathlib import Path

from apdu import split_complete_apdus
from compare import (
    InputError,
    _apply_allowlist,
    _run_comparison,
    load_allowlist,
)

ROOT = Path(__file__).resolve().parents[4]
CAPTURE_DIR = ROOT / "services" / "iec104" / "captures"
HERE = Path(__file__).resolve().parent
ALLOWLIST = HERE / "allowlist.json"


def _read_pcap(path: Path) -> tuple[bytes, str, list[tuple[int, int, bytearray]]]:
    data = path.read_bytes()
    magic = data[:4]
    byte_order = {
        b"\xd4\xc3\xb2\xa1": "<",
        b"\xa1\xb2\xc3\xd4": ">",
        b"\x4d\x3c\xb2\xa1": "<",
        b"\xa1\xb2\x3c\x4d": ">",
    }.get(magic)
    if byte_order is None:
        raise AssertionError(f"unsupported pcap magic {magic.hex()}")
    records = []
    offset = 24
    while offset < len(data):
        if offset + 16 > len(data):
            raise AssertionError("truncated pcap record header")
        seconds, fraction, captured, original = struct.unpack(
            byte_order + "IIII", data[offset:offset + 16]
        )
        offset += 16
        packet = bytearray(data[offset:offset + captured])
        if len(packet) != captured:
            raise AssertionError("truncated pcap record")
        records.append((seconds, fraction, packet))
        offset += captured
    return data[:24], byte_order, records


def _write_pcap(
    path: Path,
    header: bytes,
    byte_order: str,
    records: list[tuple[int, int, bytearray]],
) -> None:
    with path.open("wb") as output:
        output.write(header)
        for seconds, fraction, packet in records:
            output.write(
                struct.pack(byte_order + "IIII", seconds, fraction, len(packet), len(packet))
            )
            output.write(packet)


def _tcp_payload(packet: bytearray) -> tuple[int, int, tuple[bytes, bytes, int, int]] | None:
    if len(packet) < 14 or packet[12:14] != b"\x08\x00":
        return None
    ip = 14
    if len(packet) < ip + 20 or packet[ip + 9] != 6:
        return None
    ip_header_len = (packet[ip] & 0x0F) * 4
    if packet[ip] >> 4 != 4 or len(packet) < ip + ip_header_len:
        return None
    fragment = struct.unpack(">H", packet[ip + 6:ip + 8])[0] & 0x1FFF
    if fragment:
        return None
    total_len = struct.unpack(">H", packet[ip + 2:ip + 4])[0]
    tcp = ip + ip_header_len
    if total_len < ip_header_len + 20 or len(packet) < tcp + 20:
        return None
    tcp_header_len = (packet[tcp + 12] >> 4) * 4
    payload_start = tcp + tcp_header_len
    payload_end = min(ip + total_len, len(packet))
    if payload_start > payload_end:
        return None
    source_port, destination_port = struct.unpack(">HH", packet[tcp:tcp + 4])
    key = (
        bytes(packet[ip + 12:ip + 16]),
        bytes(packet[ip + 16:ip + 20]),
        source_port,
        destination_port,
    )
    return payload_start, payload_end - payload_start, key


def _checksum(data: bytes) -> int:
    if len(data) & 1:
        data += b"\0"
    words = struct.unpack(">" + "H" * (len(data) // 2), data)
    total = sum(words)
    total = (total & 0xFFFF) + (total >> 16)
    total = (total & 0xFFFF) + (total >> 16)
    return (~total) & 0xFFFF


def _rechecksum_tcp(packet: bytearray) -> None:
    ip = 14
    ip_header_len = (packet[ip] & 0x0F) * 4
    total_len = struct.unpack(">H", packet[ip + 2:ip + 4])[0]
    tcp = ip + ip_header_len
    tcp_len = total_len - ip_header_len
    packet[tcp + 16:tcp + 18] = b"\0\0"
    pseudo = bytes(packet[ip + 12:ip + 20]) + b"\0\x06" + struct.pack(">H", tcp_len)
    checksum = _checksum(pseudo + bytes(packet[tcp:tcp + tcp_len]))
    packet[tcp + 16:tcp + 18] = struct.pack(">H", checksum)


def _apdu_slices(payload: bytes) -> list[tuple[int, bytes]]:
    slices = []
    cursor = 0
    for apdu in split_complete_apdus(payload):
        slices.append((cursor, apdu))
        cursor += len(apdu)
    return slices


def _rewrite(
    source: Path,
    target: Path,
    transform,
    *,
    first_only: bool = True,
) -> int:
    header, byte_order, records = _read_pcap(source)
    changes = 0
    for _, _, packet in records:
        parsed = _tcp_payload(packet)
        if not parsed:
            continue
        payload_start, payload_len, _ = parsed
        payload = bytes(packet[payload_start:payload_start + payload_len])
        replacement = bytearray(payload)
        changed = 0
        for relative, apdu in _apdu_slices(payload):
            if len(apdu) < 12 or apdu[0] != 0x68 or apdu[2] & 1:
                continue
            count = transform(replacement, relative, apdu)
            changed += count
            if count and first_only:
                break
        if changed:
            packet[payload_start:payload_start + payload_len] = replacement
            _rechecksum_tcp(packet)
            changes += changed
            if first_only:
                break
    if not changes:
        raise AssertionError(f"mutation found no matching APDU in {source.name}")
    _write_pcap(target, header, byte_order, records)
    return changes


def _flip_cot(payload: bytearray, start: int, _apdu: bytes) -> int:
    payload[start + 8] ^= 0x01
    return 1


def _flip_quality(payload: bytearray, start: int, apdu: bytes) -> int:
    if apdu[6] != 13 or len(apdu) <= 19:
        return 0
    payload[start + 19] ^= 0x01
    return 1


def _flip_cp56_iv(payload: bytearray, start: int, apdu: bytes) -> int:
    if apdu[6] != 31 or len(apdu) <= 18:
        return 0
    payload[start + 18] ^= 0x80
    return 1


def _shift_cp56(payload: bytearray, start: int, apdu: bytes) -> int:
    if apdu[6] != 31:
        return 0
    count = apdu[7] & 0x7F
    sq = bool(apdu[7] & 0x80)
    cursor = start + 12
    if sq and count:
        cursor += 3
    changed = 0
    for _ in range(count):
        if not sq:
            cursor += 3
        timestamp = cursor + 1
        if timestamp + 7 > start + len(apdu):
            return changed
        ms = int.from_bytes(payload[timestamp:timestamp + 2], "little")
        minute = payload[timestamp + 2]
        ms += 200
        if ms >= 60_000:
            ms -= 60_000
            minute = (minute & 0xC0) | ((minute + 1) & 0x3F)
        payload[timestamp:timestamp + 2] = ms.to_bytes(2, "little")
        payload[timestamp + 2] = minute
        cursor += 8
        changed += 1
    return changed


def _drop_one_payload_frame(source: Path, target: Path) -> None:
    header, byte_order, records = _read_pcap(source)
    for index, (_, _, packet) in enumerate(records):
        parsed = _tcp_payload(packet)
        if not parsed:
            continue
        start, size, _ = parsed
        payload = bytes(packet[start:start + size])
        apdus = _apdu_slices(payload)
        if len(apdus) == 1 and apdus[0][0] == 0 and len(apdus[0][1]) == len(payload):
            del records[index]
            _write_pcap(target, header, byte_order, records)
            return
    raise AssertionError("capture has no single-APDU TCP segment to drop")


def _duplicate_one_payload_frame(source: Path, target: Path) -> None:
    header, byte_order, records = _read_pcap(source)
    for index, record in enumerate(records):
        parsed = _tcp_payload(record[2])
        if not parsed:
            continue
        start, size, _ = parsed
        payload = bytes(record[2][start:start + size])
        apdus = _apdu_slices(payload)
        if len(apdus) == 1 and apdus[0][0] == 0 and len(apdus[0][1]) == size:
            duplicate = (record[0], record[1], bytearray(record[2]))
            records.insert(index + 1, duplicate)
            _write_pcap(target, header, byte_order, records)
            return
    raise AssertionError("capture has no single-APDU TCP segment to retransmit")


def _swap_two_apdus(source: Path, target: Path) -> None:
    header, byte_order, records = _read_pcap(source)
    candidates = []
    for record_index, (_, _, packet) in enumerate(records):
        parsed = _tcp_payload(packet)
        if not parsed:
            continue
        start, size, key = parsed
        payload = bytes(packet[start:start + size])
        slices = _apdu_slices(payload)
        if len(slices) == 1 and slices[0][0] == 0 and len(slices[0][1]) == size:
            candidates.append((record_index, start, key, payload))
    selected = None
    for index, left in enumerate(candidates):
        for right in candidates[index + 1:]:
            if left[2] == right[2] and len(left[3]) == len(right[3]) and left[3] != right[3]:
                selected = (left, right)
                break
        if selected:
            break
    if selected is None:
        raise AssertionError("no same-direction equal-size APDU pair to swap")
    left, right = selected
    left_packet = records[left[0]][2]
    right_packet = records[right[0]][2]
    left_packet[left[1]:left[1] + len(left[3])] = right[3]
    right_packet[right[1]:right[1] + len(right[3])] = left[3]
    _rechecksum_tcp(left_packet)
    _rechecksum_tcp(right_packet)
    _write_pcap(target, header, byte_order, records)


def _report_for(
    baseline: Path,
    candidate: Path,
    allowlist: Path = ALLOWLIST,
) -> dict:
    return _run_comparison(
        baseline, candidate, allowlist, None, "/opt/homebrew/bin/tshark", 60, 1000
    )


def _all_differences(report: dict, comparison: str) -> list[dict]:
    return [
        difference
        for difference in report["differences"]
        if difference["comparison"] == comparison
    ]


class CaptureComparisonTests(unittest.TestCase):
    def test_each_committed_capture_compares_with_itself(self) -> None:
        captures = sorted(CAPTURE_DIR.glob("*.pcap"))
        self.assertEqual(len(captures), 4)
        for capture in captures:
            with self.subTest(capture=capture.name):
                report = _report_for(capture, capture)
                self.assertTrue(report["passed"], report["comparisons"]["C_decode_validity"])
                self.assertEqual(report["comparisons"]["A_message_level"]["difference_count"], 0)
                self.assertEqual(report["comparisons"]["B_apdu_level"]["difference_count"], 0)
                self.assertGreater(
                    sum(map(len, report["comparisons"]["A_message_level"]["records"]["baseline"].values())),
                    0,
                )

    def test_cot_mutation_fails_message_and_apdu_gates(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            mutated = Path(directory) / "cot.pcap"
            _rewrite(source, mutated, _flip_cot)
            report = _report_for(source, mutated)
        a_fields = {item["field"] for item in _all_differences(report, "A")}
        b_fields = {item["field"] for item in _all_differences(report, "B")}
        self.assertTrue(any("cot.cause" in field for field in a_fields), a_fields)
        self.assertTrue(any(".byte[8]" in field for field in b_fields), b_fields)
        self.assertTrue(report["passed"] is False)

    def test_quality_bit_mutation_names_quality_field(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            mutated = Path(directory) / "quality.pcap"
            _rewrite(source, mutated, _flip_quality)
            report = _report_for(source, mutated)
        fields = {item["field"] for item in _all_differences(report, "A")}
        self.assertIn("asdu.quality_bits[0].bits", fields)

    def test_dropping_an_apdu_fails(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            mutated = Path(directory) / "dropped.pcap"
            _drop_one_payload_frame(source, mutated)
            report = _report_for(source, mutated)
        self.assertFalse(report["passed"])
        self.assertTrue(
            _all_differences(report, "A")
            or _all_differences(report, "B")
            or report["comparisons"]["C_decode_validity"]["differences"]
        )

    def test_identical_tcp_retransmission_is_not_an_apdu_difference(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            retransmitted = Path(directory) / "retransmitted.pcap"
            _duplicate_one_payload_frame(source, retransmitted)
            report = _report_for(source, retransmitted)
        self.assertTrue(report["passed"], report["comparisons"]["C_decode_validity"])
        duplicate_counts = [
            direction["duplicate_payload_bytes"]
            for session in report["comparisons"]["D_tcp_report_only"]["sessions"]
            for direction in session["candidate"].values()
        ]
        self.assertGreater(max(duplicate_counts), 0)

    def test_swapping_apdus_preserves_order_difference(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            mutated = Path(directory) / "swapped.pcap"
            _swap_two_apdus(source, mutated)
            report = _report_for(source, mutated)
        self.assertTrue(_all_differences(report, "A"))
        self.assertTrue(_all_differences(report, "B"))

    def test_cp56_iv_change_fails_with_flag_named(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            mutated = Path(directory) / "invalid-time.pcap"
            _rewrite(source, mutated, _flip_cp56_iv)
            report = _report_for(source, mutated)
        a_fields = {item["field"] for item in _all_differences(report, "A")}
        b_fields = {item["field"] for item in _all_differences(report, "B")}
        self.assertTrue(any(".iv" in field for field in a_fields), a_fields)
        self.assertTrue(any(".byte[18]" in field for field in b_fields), b_fields)

    def test_uniform_cp56_shift_within_tolerance_passes(self) -> None:
        source = CAPTURE_DIR / "startup-gi-monitor.pcap"
        with tempfile.TemporaryDirectory() as directory:
            shifted = Path(directory) / "shifted.pcap"
            changed = _rewrite(source, shifted, _shift_cp56, first_only=False)
            self.assertGreater(changed, 0)
            report = _report_for(source, shifted)
        self.assertTrue(report["passed"], report["differences"])


class AllowlistTests(unittest.TestCase):
    @staticmethod
    def _entry(**overrides):
        entry = {
            "id": "reviewed-change",
            "scope": {
                "comparison": "A",
                "session": 0,
                "direction": "client_to_server",
                "record_range": [2, 2],
            },
            "field": "asdu.cot.cause",
            "baseline": 6,
            "candidate": 7,
            "reason": "reviewed variance",
            "approved_by": "Tony / approval",
            "max_matches": 1,
        }
        entry.update(overrides)
        return entry

    @staticmethod
    def _difference(index: int = 2) -> dict:
        return {
            "comparison": "A",
            "session": 0,
            "direction": "client_to_server",
            "index": index,
            "field": "asdu.cot.cause",
            "baseline": 6,
            "candidate": 7,
        }

    def test_exact_allowlist_suppresses_one_match(self) -> None:
        differences, issues = _apply_allowlist(
            [self._difference()], [self._entry()]
        )
        self.assertEqual(issues, [])
        self.assertTrue(differences[0]["allowed"])
        self.assertEqual(differences[0]["allowlist_id"], "reviewed-change")

    def test_stale_and_over_count_entries_fail(self) -> None:
        stale = self._entry(candidate=8)
        _, stale_issues = _apply_allowlist([self._difference()], [stale])
        self.assertTrue(any("stale" in issue for issue in stale_issues))
        _, over_issues = _apply_allowlist(
            [self._difference(), self._difference(index=3)],
            [self._entry(scope={
                "comparison": "A",
                "session": 0,
                "direction": "client_to_server",
            })],
        )
        self.assertTrue(any("over max_matches" in issue for issue in over_issues))

    def test_approved_by_is_required_and_wildcards_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "allowlist.json"
            entry = self._entry()
            del entry["approved_by"]
            path.write_text(json.dumps([entry]), encoding="utf-8")
            with self.assertRaises(InputError):
                load_allowlist(path)
            entry = self._entry(field="asdu.*")
            path.write_text(json.dumps([entry]), encoding="utf-8")
            with self.assertRaises(InputError):
                load_allowlist(path)

    def test_exact_array_indexes_are_not_wildcards(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "allowlist.json"
            for field in ("asdu.quality_bits[0].bits", "apdu[2].byte[4]"):
                path.write_text(json.dumps([self._entry(field=field)]))
                self.assertEqual(load_allowlist(path)[0]["field"], field)
            for field in ("apdu[*].byte[4]", "apdu[0-9]", "apdu[?]", "apdu[]"):
                path.write_text(json.dumps([self._entry(field=field)]))
                with self.assertRaises(InputError):
                    load_allowlist(path)


if __name__ == "__main__":
    unittest.main()
