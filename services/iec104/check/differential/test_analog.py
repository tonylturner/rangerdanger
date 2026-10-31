# SPDX-License-Identifier: Apache-2.0
"""Analog cycles use exact fixture values and one wire barrier per cycle."""

import json
from pathlib import Path
import struct
import tempfile
import time
import unittest
from unittest.mock import Mock

from driver import Peer
from scenario import HERE, point_map, scenario


class AnalogTests(unittest.TestCase):
    def test_profile_retains_real_cyclic_float_points(self):
        data = point_map("172.29.110.0/24", "feeder-analog-v1")
        self.assertEqual([point["ioa"] for point in data["points"]], [1001, 1002, 2001, 2002])
        self.assertEqual([point["cyclic_ms"] for point in data["points"][2:]], [2000, 2000])
        case = scenario(HERE / "scenarios/analog-cyclic.json")
        self.assertEqual([action["op"] for action in case["actions"]], ["start", "cycles", "stop"])

    def run_cycles(self, value=28.9):
        peer = Peer(Mock(), time.monotonic() + 1)
        asdu = bytes.fromhex("0d0201000100")
        asdu += (2001).to_bytes(3, "little") + struct.pack("<fB", value, 0)
        asdu += (2002).to_bytes(3, "little") + struct.pack("<fB", 20, 0)
        peer.read = Mock(return_value=b"\x00\x00\x00\x00" + asdu)
        peer.action({"op": "cycles", "args": {"count": 3}, "timeout_s": 10}, "unused")
        return peer

    def test_every_cycle_is_checked_without_sleep(self):
        self.assertEqual(self.run_cycles().read.call_count, 3)

    def test_wrong_register_value_fails_at_cycle_barrier(self):
        with self.assertRaises(AssertionError):
            self.run_cycles(29.9)

    def test_incompatible_profile_actions_fail_before_docker(self):
        case = json.loads((HERE / "scenarios/analog-cyclic.json").read_text())
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bad.json"
            case["point_map"] = "feeder-status-v1"
            path.write_text(json.dumps(case))
            with self.assertRaises(ValueError):
                scenario(path)
            case["point_map"] = "feeder-analog-v1"
            case["actions"][1] = {"op": "gi", "args": {}, "timeout_s": 5}
            path.write_text(json.dumps(case))
            with self.assertRaises(ValueError):
                scenario(path)


if __name__ == "__main__":
    unittest.main()
