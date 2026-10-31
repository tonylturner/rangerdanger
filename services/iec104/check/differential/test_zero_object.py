# SPDX-License-Identifier: Apache-2.0
"""Zero-object peer barriers and scenario-to-message associations."""

import json
from pathlib import Path
import socket
import tempfile
import time
import unittest

from driver import Peer
from malformed import resolve
from scenario import HERE, load, scenario


class ZeroObjectTests(unittest.TestCase):
    def test_command_associates_input_and_direct_reply_not_s_ack(self):
        left, right = socket.socketpair()
        with left, right:
            peer = Peer(left, time.monotonic() + 2)
            # STARTDT and its confirmation have already consumed one APDU
            # in each direction. No capture frame identities are involved.
            peer.sent_count = peer.read_count = 1
            right.sendall(bytes.fromhex("680401000200680a000002002e006f000100"))
            peer.zero_object(46, 1)
            self.assertEqual(right.recv(18), bytes.fromhex("680a000000002e0006000100680401000200"))
            self.assertEqual(peer.associations, [
                {"step": 1, "message": "input", "session": 0, "direction": "client_to_server", "index": 1},
                {"step": 1, "message": "reply", "session": 0, "direction": "server_to_client", "index": 2},
            ])
            case = scenario(HERE / "scenarios/zero-command.json")
            resolved = resolve(case["malformed"], peer.associations, peer.associations)
            self.assertEqual(resolved[1]["baseline"]["index"], 2)

    def test_gi_requires_s_before_eof_and_never_an_asdu_reply(self):
        for response, passes in (("680401000200", True), ("680a0000020064006f000100", False)):
            with self.subTest(response=response):
                left, right = socket.socketpair()
                with left, right:
                    peer = Peer(left, time.monotonic() + 2)
                    right.sendall(bytes.fromhex(response))
                    right.shutdown(socket.SHUT_WR)
                    if passes:
                        peer.zero_object(100, 1)
                        self.assertEqual(len(peer.associations), 1)
                    else:
                        with self.assertRaises(AssertionError):
                            peer.zero_object(100, 1)

    def test_monitor_uses_real_cc_ack_and_testfr_not_cli_gi_barrier(self):
        left, right = socket.socketpair()
        with left, right:
            peer = Peer(left, time.monotonic() + 2, server=True)
            peer.sent_count = 9
            right.sendall(bytes.fromhex("680401000200680483000000"))
            peer.zero_object(3, 0)
            self.assertEqual(right.recv(18), bytes.fromhex("680a00000000030003000100680443000000"))
            self.assertEqual(peer.associations, [{"step": 0, "message": "input", "session": 0,
                                                "direction": "server_to_client", "index": 9}])

    def test_bad_declarations_and_role_mismatches_rejected(self):
        base = load(HERE / "scenarios/zero-gi.json")
        changes = [lambda v: v["malformed"][0].update(step=20),
                   lambda v: v["malformed"][0].update(step=0),
                   lambda v: v["malformed"][0].update(message="reply"),
                   lambda v: v["malformed"][0].update(frame=8),
                   lambda v: v["malformed"].append(v["malformed"][0]),
                   lambda v: v["actions"][1]["args"].update(type=3)]
        for change in changes:
            with tempfile.TemporaryDirectory() as directory:
                value = json.loads(json.dumps(base))
                change(value)
                path = Path(directory) / "bad.json"
                path.write_text(json.dumps(value))
                with self.assertRaises(ValueError):
                    scenario(path)
