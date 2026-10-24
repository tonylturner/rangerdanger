# SPDX-License-Identifier: Apache-2.0
"""Offline scenario, ownership, capture, framing and deadline regressions."""

import json
from pathlib import Path
import socket
import tempfile
import time
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch

from driver import Peer
from runtime import Docker, unused_subnet
from scenario import HERE, load, point_map, roles, scenario
from process_utils import run
import run as runner


class ScenarioTests(unittest.TestCase):
    def invalid(self, change):
        value = load(HERE / "scenarios/startup-session.json")
        change(value)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bad.json"
            path.write_text(json.dumps(value))
            with self.assertRaises(ValueError):
                scenario(path)

    def test_entire_catalog_is_strict_and_covers_four_role_rows(self):
        cases = [scenario(path) for path in (HERE / "scenarios").glob("*.json")]
        self.assertEqual(len(cases), 10)
        self.assertEqual({case["kind"] for case in cases},
                         {"raw-rtu", "cmd-rtu", "cc-rtu", "raw-cc"})
        self.assertEqual(len({case["id"] for case in cases}), len(cases))

    def test_unknown_keys_are_rejected(self):
        self.invalid(lambda value: value.update(typo=True))

    def test_boolean_deadline_is_rejected(self):
        self.invalid(lambda value: value.update(timeout_s=True))

    def test_unknown_action_arguments_are_rejected(self):
        self.invalid(lambda value: value["actions"][0]["args"].update(typo=1))

    def test_unhashable_kind_and_action_are_rejected_as_input_errors(self):
        self.invalid(lambda value: value.update(kind=[]))
        self.invalid(lambda value: value["actions"][0].update(op={}))

    def test_unhashable_fixture_query_is_rejected_as_input_error(self):
        self.invalid(lambda value: value["actions"][0].update(
            op="fixture", args={"query": []}))

    def test_action_must_match_role_kind(self):
        self.invalid(lambda value: value.update(kind="raw-cc"))

    def test_duplicate_keys_and_nonfinite_numbers_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bad.json"
            for text in ('{"x":1,"x":2}', '{"x":NaN}'):
                path.write_text(text)
                with self.assertRaises(ValueError):
                    load(path)

    def test_forbidden_containd_role_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "roles.json"
            path.write_text(json.dumps(dict.fromkeys(("rtu", "cc", "cmd", "peer"), "containd:test")))
            with self.assertRaises(ValueError):
                roles(path)

    def test_point_map_rewrites_only_owned_profile_addresses(self):
        data = point_map("172.29.150.0/24")
        self.assertEqual(len(data["points"]), 2)
        self.assertEqual(len(data["commands"]), 1)
        self.assertEqual(data["devices"][0]["host"], "172.29.150.20")
        self.assertIn("172.29.150.20", data["clock"]["url"])


class CatalogTests(unittest.TestCase):
    def test_failed_scenario_keeps_evidence_and_does_not_skip_later_cases(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            with patch.object(runner, "run_case", side_effect=[RuntimeError("failed peer"), 1, 0]) as call:
                status = runner.run_cases(
                    [Path("one.json"), Path("two.json"), Path("three.json")],
                    None, {}, {}, Path("reference"), out, "tshark")
            self.assertEqual(status, 2)
            self.assertEqual(call.call_count, 3)
            results = load(out / "results.json")
            self.assertEqual([case["exit_code"] for case in results], [2, 1, 0])
            self.assertEqual(results[0]["error"], "failed peer")


class OwnershipTests(unittest.TestCase):
    def test_failed_process_keeps_observed_exit_code_in_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            result = subprocess.CompletedProcess(["docker", "version"], 7, "", "broken")
            with patch("runtime.run", return_value=result):
                with self.assertRaises(RuntimeError):
                    docker.call("version")
            self.assertEqual(load(Path(directory) / "commands.json")[0]["returncode"], 7)

    def test_capture_uses_immediate_delivery_full_snaplen_and_bounded_ring(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=["owned", "", "listening on eth0"])
            docker.capture("endpoint", "tools", True)
            args = docker.call.call_args_list[0].args
            self.assertIn("--immediate-mode", args)
            self.assertEqual(args[args.index("-B") + 1], "65536")
            self.assertEqual(args[args.index("-s") + 1], "0")

    def test_subnet_selection_rejects_overlapping_supernet(self):
        network = {"IPAM": {"Config": [{"Subnet": "172.29.0.0/16"}]}}
        with self.assertRaises(RuntimeError):
            unused_subnet([network])

    def test_network_rechecks_overlap_before_creation(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.inventory = Mock(return_value=[{"Name": "external", "IPAM": {
                "Config": [{"Subnet": "172.29.110.0/24"}]}}])
            docker.call = Mock()
            with self.assertRaises(RuntimeError):
                docker.network("172.29.110.0/24")
            docker.call.assert_not_called()

    def test_cleanup_selects_only_exact_owner_label(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=["owned-container", "", "owned-network", ""])
            docker.cleanup()
            calls = [call.args for call in docker.call.call_args_list]
            self.assertEqual(calls, [
                ("ps", "-aq", "--filter", "label=rd.iec104.differential=unit"),
                ("rm", "-f", "owned-container"),
                ("network", "ls", "-q", "--filter", "label=rd.iec104.differential=unit"),
                ("network", "rm", "owned-network"),
            ])

    def test_capture_drops_fail_but_keep_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=["", "0", "8 packets captured\n1 packets dropped by kernel\n", ""])
            with self.assertRaises(RuntimeError):
                docker.finish_capture("owned")
            self.assertTrue(any(call.args[0] == "cp" for call in docker.call.call_args_list))

    def test_cleanup_failure_is_not_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=RuntimeError("failed"))
            with self.assertRaisesRegex(RuntimeError, "cleanup failed"):
                docker.cleanup()

    def test_exited_capture_is_an_error_but_pcap_is_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=[
                RuntimeError("exited"), "1 packets captured\n0 packets dropped by kernel\n", "",
            ])
            with self.assertRaisesRegex(RuntimeError, "exited"):
                docker.finish_capture("owned")
            self.assertTrue(any(call.args[0] == "cp" for call in docker.call.call_args_list))


class PeerTests(unittest.TestCase):
    def test_receive_sequence_wrap_and_ack(self):
        left, right = socket.socketpair()
        with left, right:
            peer = Peer(left, time.monotonic() + 1)
            peer.rx = 32767
            right.sendall(bytes.fromhex("6804feff0000"))
            peer.read()
            self.assertEqual(peer.rx, 0)
            self.assertEqual(right.recv(6), bytes.fromhex("680401000000"))

    def test_expired_action_does_not_read_or_send(self):
        sock = Mock()
        peer = Peer(sock, time.monotonic() - 1)
        with self.assertRaises(TimeoutError):
            peer.read()
        sock.recv.assert_not_called()
        sock.sendall.assert_not_called()

    def test_receive_gap_fails(self):
        left, right = socket.socketpair()
        with left, right:
            peer = Peer(left, time.monotonic() + 1)
            right.sendall(bytes.fromhex("680402000000"))
            with self.assertRaises(AssertionError):
                peer.read()


class ProcessTests(unittest.TestCase):
    def test_subprocess_deadline_propagates(self):
        with self.assertRaises(subprocess.TimeoutExpired):
            run([sys.executable, "-c", "import time; time.sleep(10)"], timeout=0.05)

    def test_sealed_output_is_not_returned(self):
        result = run([sys.executable, "-c", "print('withheld')"], timeout=2, sealed=True)
        self.assertEqual(result.returncode, 0)
        self.assertIsNone(result.stdout)
        self.assertIsNone(result.stderr)


if __name__ == "__main__":
    unittest.main()
