# SPDX-License-Identifier: Apache-2.0
"""Connection admission uses protocol barriers, not observation sleeps."""

import time
import unittest
from unittest.mock import Mock, patch

from driver import Peer
from scenario import HERE, scenario


class ConnectionLimitTests(unittest.TestCase):
    def run_limit(self, ninth_reply=b""):
        events = []
        root = Mock()
        root.getpeername.return_value = ("rtu", 2404)
        held = []

        def connect(*_args, **_kwargs):
            index = len(held) + 2
            sock = Mock()
            sock.__enter__ = Mock(return_value=sock)
            sock.__exit__ = Mock(return_value=False)
            sock.recv.side_effect = lambda _n: events.append(("eof", index)) or ninth_reply
            held.append(sock)
            events.append(("connect", index))
            return sock

        with patch("driver.socket.create_connection", side_effect=connect), \
                patch.object(Peer, "u", lambda self, act, con: events.append(("u", act))):
            Peer(root, time.monotonic() + 1).connection_limit()
        return events, held

    def test_ninth_waits_for_eight_starts_and_checks_incumbents(self):
        events, held = self.run_limit()
        self.assertEqual(events[:14], [item for n in range(2, 9)
                                      for item in (("connect", n), ("u", 7))])
        self.assertEqual(events[14:16], [("connect", 9), ("eof", 9)])
        self.assertEqual(events[16:24], [("u", 67)] * 8)
        self.assertEqual(events[24:], [("u", 19)] * 7)
        for sock in held:
            sock.__exit__.assert_called_once()

    def test_apci_on_ninth_is_not_silently_ignored(self):
        with self.assertRaisesRegex(AssertionError, "APCI data"):
            self.run_limit(b"\x68")

    def test_catalog_counts_rejected_tcp_session_too(self):
        case = scenario(HERE / "scenarios/ninth-connection.json")
        self.assertEqual(case["expected_sessions"], 9)
        self.assertEqual([a["op"] for a in case["actions"]],
                         ["start", "connection-limit", "stop"])


if __name__ == "__main__":
    unittest.main()
