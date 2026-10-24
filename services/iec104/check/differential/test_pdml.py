# SPDX-License-Identifier: Apache-2.0
"""Packet-stream parsing and coalesced S/I sibling association regressions."""

from pathlib import Path
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET

from decode import _apci_pairs
from pdml import PDMLError, decode_file, packets


class PDMLTests(unittest.TestCase):
    def test_packet_stream_retains_hierarchy_and_order(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "test.pdml"
            path.write_text("<pdml>" + "".join(
                f'<packet><proto name="test"><field name="number" show="{i}"/></proto></packet>'
                for i in range(1000)
            ) + "</pdml>")
            self.assertEqual(
                [int(packet.find("./proto/field").get("show")) for packet in packets(path)],
                list(range(1000)),
            )

    def test_truncated_pdml_is_not_accepted(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "test.pdml"
            path.write_text("<pdml><packet>")
            with self.assertRaises(PDMLError):
                list(packets(path))

    def test_decoder_failure_is_not_accepted(self):
        with self.assertRaises(PDMLError):
            with decode_file([sys.executable, "-c", "raise SystemExit(7)"], 2):
                self.fail("failed decoder yielded a file")

    def test_decoder_process_is_bounded(self):
        with self.assertRaises(PDMLError):
            with decode_file([sys.executable, "-c", "import time; time.sleep(10)"], 0.05):
                self.fail("timed-out decoder yielded a file")

    def test_coalesced_s_s_i_attaches_asdu_only_to_i(self):
        packet = ET.fromstring("""<packet>
          <proto name="iec60870_104"><field name="iec60870_104.type" show="1"/></proto>
          <proto name="iec60870_104"><field name="iec60870_104.type" show="1"/></proto>
          <proto name="iec60870_104"><field name="iec60870_104.type" show="0"/></proto>
          <proto name="iec60870_asdu"><field name="iec60870_asdu.typeid" show="46"/></proto>
        </packet>""")
        errors = []
        pairs = _apci_pairs(packet, 1, errors)
        self.assertEqual(errors, [])
        self.assertEqual(len(pairs), 3)
        self.assertIsNone(pairs[0][1])
        self.assertIsNone(pairs[1][1])
        self.assertEqual(pairs[2][1].get("name"), "iec60870_asdu")

    def test_asdu_after_s_is_invalid_not_assigned_to_later_i(self):
        packet = ET.fromstring("""<packet>
          <proto name="iec60870_104"><field name="iec60870_104.type" show="1"/></proto>
          <proto name="iec60870_asdu"/>
          <proto name="iec60870_104"><field name="iec60870_104.type" show="0"/></proto>
        </packet>""")
        errors = []
        pairs = _apci_pairs(packet, 1, errors)
        self.assertEqual(len(errors), 1)
        self.assertIsNone(pairs[1][1])


if __name__ == "__main__":
    unittest.main()
