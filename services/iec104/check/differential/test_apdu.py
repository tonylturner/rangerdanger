# SPDX-License-Identifier: Apache-2.0
"""Tests for TCP reassembly, APCI parsing, and CP56 time normalization."""

from __future__ import annotations

import unittest

from apdu import (
    APDU,
    Segment,
    cp56_datetime,
    cp56_offsets,
    normalize_apdus,
    reassemble,
    session_first_cp56,
)


class ReassemblyTests(unittest.TestCase):
    def test_fragmented_and_coalesced_payloads_reassemble_in_order(self) -> None:
        first = bytes.fromhex("680407000000")
        second = bytes.fromhex("68040b000000")
        result = reassemble(
            [
                Segment(100, first[:2], 1),
                Segment(102, first[2:] + second, 2),
            ]
        )
        self.assertEqual([apdu.data for apdu in result.apdus], [first, second])
        self.assertEqual(result.issues, [])
        self.assertEqual(result.covered_bytes, len(first) + len(second))

    def test_identical_retransmission_is_counted_once(self) -> None:
        frame = bytes.fromhex("680407000000")
        result = reassemble(
            [
                Segment(700, frame, 1),
                Segment(700, frame, 2),
            ]
        )
        self.assertEqual([apdu.data for apdu in result.apdus], [frame])
        self.assertEqual(result.duplicate_bytes, len(frame))
        self.assertEqual(result.issues, [])

    def test_sequence_wrap_is_unwrapped(self) -> None:
        first = bytes.fromhex("680407000000")
        result = reassemble(
            [
                Segment(0xFFFFFFFD, first[:3], 1),
                Segment(0, first[3:], 2),
            ]
        )
        self.assertEqual([apdu.data for apdu in result.apdus], [first])
        self.assertEqual(result.issues, [])

    def test_gap_is_not_silently_reassembled(self) -> None:
        frame = bytes.fromhex("680407000000")
        result = reassemble(
            [
                Segment(100, frame[:3], 1),
                Segment(104, frame[4:], 2),
            ]
        )
        self.assertTrue(any("TCP sequence gap" in issue for issue in result.issues))
        self.assertEqual(result.apdus, [])


class CP56Tests(unittest.TestCase):
    def test_type_31_timestamp_offset_and_relative_normalization(self) -> None:
        # IEC 104 I-frame with one type-31 object, IOA 1, DIQ 0 and
        # CP56 2026-10-09 14:55:55.687 (little-endian milliseconds).
        raw = bytes.fromhex("6815000000001f01030001000100000087d9370e090a1a")
        apdu = APDU(0, 1, 0, raw, "I", None, 0, 0)
        offsets, issue = cp56_offsets(apdu)
        self.assertIsNone(issue)
        self.assertEqual(offsets, [16])
        instant = cp56_datetime(raw[16:23])
        self.assertIsNotNone(instant)
        self.assertEqual(instant.isoformat(), "2026-10-09T14:55:55.687000")
        normalized, audit, issues = normalize_apdus(
            [apdu], instant, 1000, 0, "client_to_server"
        )
        self.assertEqual(issues, [])
        self.assertEqual(
            sum(entry["normalization"] == "cp56-relative" for entry in audit), 7
        )
        self.assertNotEqual(normalized[0][16:23], raw[16:23])

    def test_invalid_cp56_is_not_used_as_the_session_origin(self) -> None:
        invalid = bytearray.fromhex("6815000000001f01030001000100000087d9b70e090a1a")
        valid = bytearray(invalid)
        valid[18] &= 0x7F
        first = APDU(0, 1, 0, bytes(invalid), "I", None, 0, 0)
        second = APDU(1, 2, 0, bytes(valid), "I", None, 1, 1)
        self.assertEqual(session_first_cp56([first, second]), cp56_datetime(valid[16:23]))
        normalized, _, issues = normalize_apdus(
            [first, second], session_first_cp56([first, second]), 1000, 0, "client_to_server"
        )
        self.assertEqual(issues, [])
        self.assertEqual(normalized[0][18] & 0x80, 0x80)

    def test_other_supported_cp56_object_offsets(self) -> None:
        stamp = bytes.fromhex("00000000000100")
        for type_id, object_size, timestamp_offset in (
            (35, 10, 3),
            (36, 12, 5),
            (64, 12, 5),
        ):
            with self.subTest(type_id=type_id):
                object_data = bytes(timestamp_offset) + stamp
                body = b"\0\0\0" + object_data
                content = (
                    b"\0\0\0\0"
                    + bytes((type_id, 1, 3, 0, 1, 0))
                    + body
                )
                raw = bytes((0x68, len(content))) + content
                apdu = APDU(0, 1, 0, raw, "I", None, 0, 0)
                offsets, issue = cp56_offsets(apdu)
                self.assertIsNone(issue)
                self.assertEqual(offsets, [15 + timestamp_offset])
                self.assertEqual(
                    apdu.data[offsets[0]:offsets[0] + 7], stamp
                )
                self.assertEqual(len(object_data), object_size)


if __name__ == "__main__":
    unittest.main()
