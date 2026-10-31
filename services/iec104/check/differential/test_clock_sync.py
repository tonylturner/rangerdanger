# SPDX-License-Identifier: Apache-2.0
"""Independent C_CS_NA_1 wire-layout and exact status-retention regressions.

Public IEC 60870-5-101/-104 layout: type103 has IOA followed immediately by
CP56Time2a (seven octets), without a value/qualifier octet. Literal fixtures
below do not use the layout table or a protocol implementation to build bytes.
"""

import unittest

from apdu import APDU, cp56_datetime, cp56_offsets, normalize_apdus, session_first_cp56


class ClockSyncTests(unittest.TestCase):
    def frame(self, stamp=bytes.fromhex("87d9370e090a1a"), cause=6):
        raw = bytearray.fromhex("681400000000670106000100000000") + stamp
        raw[1] = len(raw) - 2
        raw[8] = cause
        return APDU(0, 1, 0, bytes(raw), "I", None, 0, 0)

    def test_literal_request_and_negative_mirror_have_timestamp_after_ioa(self):
        for cause in (6, 0x6c):
            with self.subTest(cause=cause):
                frame = self.frame(cause=cause)
                self.assertEqual(cp56_offsets(frame), ([15], None))
                self.assertEqual(len(frame.data), 22)
                self.assertEqual(frame.data[12:15], b"\0\0\0")
                self.assertEqual(session_first_cp56([frame]).isoformat(),
                                 "2026-10-09T14:55:55.687000")

    def test_short_clock_value_fails_closed(self):
        for size in range(7):
            with self.subTest(size=size):
                offsets, issue = cp56_offsets(self.frame(bytes(size)))
                self.assertEqual(offsets, [])
                self.assertEqual(issue, "truncated CP56 object for ASDU type 103")

    def test_relative_time_preserves_envelope_and_elapsed_time(self):
        first = self.frame()
        second = self.frame(bytes.fromhex("6fdd370e090a1a"), cause=0x6c)
        origin = session_first_cp56([first, second])
        normalized, audit, issues = normalize_apdus(
            [first, second], origin, 1, 0, "client_to_server")
        self.assertEqual(issues, [])
        self.assertEqual(normalized[0][:15], first.data[:15])
        self.assertEqual(normalized[1][:15], second.data[:15])
        self.assertEqual(normalized[0][15:], bytes.fromhex("00000000010100"))
        self.assertEqual(normalized[1][15:], bytes.fromhex("e8030000010100"))
        self.assertEqual(sum(item["normalization"] == "cp56-relative"
                             for item in audit), 14)

    def test_every_status_reserved_and_weekday_bit_remains_exact(self):
        baseline = self.frame()
        origin = cp56_datetime(baseline.data[15:])
        normal, _, _ = normalize_apdus([baseline], origin, 1000, 0, "client_to_server")
        for byte, mask in ((2, 0x40), (2, 0x80), (3, 0x20), (3, 0x40),
                           (3, 0x80), (4, 0x20), (4, 0x40), (4, 0x80),
                           (5, 0x10), (5, 0x20), (5, 0x40), (5, 0x80), (6, 0x80)):
            with self.subTest(byte=byte, mask=mask):
                stamp = bytearray(baseline.data[15:])
                stamp[byte] ^= mask
                changed = self.frame(stamp)
                actual, _, issues = normalize_apdus(
                    [changed], origin, 1000, 0, "client_to_server")
                self.assertEqual(issues, [])
                self.assertEqual(actual[0][15 + byte] & mask, stamp[byte] & mask)
                self.assertNotEqual(actual[0], normal[0])

    def test_invalid_clock_values_are_neither_origins_nor_normalized(self):
        for stamp in ("60ea370e090a1a", "87d93c0e090a1a", "87d93718090a1a",
                      "87d9370e000a1a", "87d9370e090d1a", "87d9b70e090a1a"):
            with self.subTest(stamp=stamp):
                frame = self.frame(bytes.fromhex(stamp))
                self.assertIsNone(session_first_cp56([frame]))
                actual, audit, issues = normalize_apdus(
                    [frame], cp56_datetime(self.frame().data[15:]), 1000, 0, "client_to_server")
                self.assertEqual(issues, [])
                self.assertEqual(actual, [frame.data])
                self.assertFalse(any(item["normalization"] == "cp56-relative" for item in audit))


if __name__ == "__main__":
    unittest.main()
