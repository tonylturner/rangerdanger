# SPDX-License-Identifier: Apache-2.0
"""Declared diagnostics never waive byte, semantic or independent parser checks."""

import copy
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import malformed
from compare import _run_comparison
from decode import CaptureResult

C2S = "client_to_server"


def capture(frame=8, diagnostic="_ws.malformed"):
    apdu = SimpleNamespace(frame=frame)
    messages = [f"frame {frame}: {diagnostic}"] if diagnostic else []
    return SimpleNamespace(apdus={(0, C2S): [apdu]}, records={(0, C2S): [{"frame": frame}]},
                           frame_diagnostics={frame: messages}, validity_errors=list(messages))


def declaration():
    return [{"step": 1, "message": "input", "baseline": {"session": 0, "direction": C2S, "index": 0},
             "candidate": {"session": 0, "direction": C2S, "index": 0}}]


class MalformedTests(unittest.TestCase):
    def test_exact_diagnostics_normalise_packet_identity_only(self):
        remaining, issues, audit = malformed.compare(declaration(), capture(), capture(19))
        self.assertEqual(remaining, {"baseline": [], "candidate": []})
        self.assertFalse(issues)
        self.assertEqual(audit[0]["diagnostics"]["baseline"], ["_ws.malformed"])

    def test_no_declaration_rejects_equal_errors(self):
        remaining, issues, _ = malformed.compare([], capture(), capture())
        self.assertTrue(remaining["baseline"])
        self.assertFalse(issues)

    def test_absent_diagnostics_on_either_or_both_sides_fail(self):
        for left, right in ((None, None), (None, "_ws.malformed"), ("_ws.malformed", None)):
            with self.subTest(left=left, right=right):
                self.assertTrue(malformed.compare(declaration(), capture(diagnostic=left),
                                                 capture(diagnostic=right))[1])

    def test_different_text_and_multiplicity_fail(self):
        other = capture(diagnostic="different error")
        self.assertTrue(malformed.compare(declaration(), capture(), other)[1])
        other = capture()
        other.frame_diagnostics[8] *= 2
        self.assertTrue(malformed.compare(declaration(), capture(), other)[1])

    def test_unrelated_and_independent_failures_remain(self):
        left = capture()
        left.validity_errors += ["frame 9: _ws.malformed", "session 0: coverage mismatch"]
        remaining, issues, _ = malformed.compare(declaration(), left, capture())
        self.assertFalse(issues)
        self.assertEqual(remaining["baseline"], left.validity_errors[1:])

    def test_missing_duplicate_and_coalesced_associations_fail_closed(self):
        for change in (lambda c: c.apdus.clear(),
                       lambda c: c.apdus[(0, C2S)].append(SimpleNamespace(frame=8)),
                       lambda c: c.records[(0, C2S)].append({"frame": 8})):
            left = capture()
            change(left)
            self.assertTrue(malformed.compare(declaration(), left, capture())[1])
        repeated = declaration() + [{**declaration()[0], "step": 2}]
        self.assertTrue(malformed.compare(repeated, capture(), capture())[1])

    def test_each_expert_keeps_its_own_message(self):
        import xml.etree.ElementTree as ET
        from decode import _expert_errors
        packet = ET.fromstring('<packet><proto name="iec60870_asdu">' + ''.join(
            '<field name="_ws.expert"><field name="_ws.expert.severity" show="8388608" showname="Severity level: Error"/>'
            f'<field name="_ws.expert.message" show="{message}"/></field>'
            for message in ("first", "second")) + '</proto></packet>')
        self.assertEqual(_expert_errors(packet, 8, True),
                         ["frame 8: expert error: first", "frame 8: expert error: second"])

    def test_malformed_protocol_marker_is_diagnostic_even_without_fields(self):
        import xml.etree.ElementTree as ET
        from decode import _expert_errors
        packet = ET.fromstring('<packet><proto name="_ws.malformed" showname="Malformed ASDU"/></packet>')
        self.assertEqual(_expert_errors(packet, 8, True),
                         ["frame 8: _ws.malformed: Malformed ASDU"])
        warning = ET.fromstring('<packet><proto name="iec60870_asdu"><field name="_ws.expert">'
                               '<field name="_ws.expert.severity" show="6291456" showname="Severity level: Warning"/>'
                               '<field name="_ws.expert.message" show="warning"/></field></proto></packet>')
        self.assertEqual(_expert_errors(warning, 8, True), [])

    def test_schema_and_resolution(self):
        associations = [{"step": 1, "message": "input", "session": 0, "direction": C2S, "index": 0}]
        declared = [{"step": 1, "message": "input"}]
        self.assertEqual(malformed.resolve(declared, associations, associations), declaration())
        for bad in ([{**declared[0], "step": True}], declared * 2, {}, [{**declared[0], "frame": 8}]):
            with self.assertRaises(ValueError):
                malformed.validate(bad)
        with self.assertRaises(ValueError):
            malformed.resolve(declared, associations, [])

    def test_a_b_still_compare_declared_messages(self):
        from apdu import APDU
        from test_compare import ALLOWLIST
        left = CaptureResult(Path(__file__), "4.6.7", {"0": 0}, {"0": 8},
                             {(0, C2S): [{"frame": 8, "session": 0, "direction": C2S,
                                          "frame_kind": "I", "u_function": None,
                                          "asdu": {"type": 46, "time_tags": []}}]},
                             {(0, C2S): [APDU(0, 8, 0, bytes.fromhex("680a000000002e0006000100"), "I", None, 0, 0)]},
                             {}, {}, ["frame 8: _ws.malformed"], [], {8: ["frame 8: _ws.malformed"]})
        right = copy.deepcopy(left)
        right.records[(0, C2S)][0]["asdu"]["type"] = 45
        right.apdus[(0, C2S)][0].data = bytes.fromhex("680a000000002d0006000100")
        with patch("compare.decode_capture", side_effect=[left, right]):
            report = _run_comparison(Path(__file__), Path(__file__), ALLOWLIST, None,
                                     "tshark", 30, 1000, declared_malformed=declaration())
        self.assertFalse(report["passed"])
        self.assertEqual(report["comparisons"]["C_decode_validity"]["status"], "pass")
        for gate in ("A_message_level", "B_apdu_level"):
            self.assertEqual(report["comparisons"][gate]["status"], "fail")
