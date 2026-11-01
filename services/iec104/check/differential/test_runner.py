# SPDX-License-Identifier: Apache-2.0
"""Offline scenario, ownership, capture, framing and deadline regressions."""

import json
from pathlib import Path
import socket
import struct
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
        self.assertEqual(len(cases), 15)
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
    def test_nonempty_allowlist_without_role_set_fails_before_docker(self):
        entry = {
            "id": "test", "scope": {"scenarios": ["startup-session"], "role_set": "all-go",
                                   "comparison": "A", "session": 0, "direction": "session"},
            "field": "frame_kind", "baseline": "S", "candidate": None,
            "reason": "test approval", "approved_by": "test", "max_matches": 1,
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "allowlist.json"
            path.write_text(json.dumps([entry]))
            argv = ["run.py", str(HERE / "scenarios/startup-session.json"),
                    "--baseline", "baseline", "--candidate", "candidate",
                    "--reference", "reference", "--out", str(Path(directory) / "evidence"),
                    "--allowlist", str(path)]
            with patch.object(sys, "argv", argv), patch.object(runner, "roles", return_value={}), \
                    patch.object(runner, "load", return_value={"image_id": "sha256:test"}), \
                    patch.object(runner, "Docker") as docker, \
                    patch.object(runner, "run_cases") as cases:
                self.assertEqual(runner.main(), 2)
                docker.assert_not_called()
                cases.assert_not_called()
            self.assertFalse((Path(directory) / "evidence").exists())

    def test_catalog_forwards_same_role_and_allowlist_to_each_case(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(runner, "run_case", return_value=0) as call:
                status = runner.run_cases(
                    [Path("one.json"), Path("two.json")], None, {}, {},
                    Path("reference"), Path(directory), "tshark",
                    role_set="all-go", allowlist=Path("reviewed.json"))
            self.assertEqual(status, 0)
            self.assertEqual(call.call_count, 2)
            for invocation in call.call_args_list:
                self.assertEqual(invocation.kwargs,
                                 {"role_set": "all-go", "allowlist": Path("reviewed.json")})

    def test_comparator_receives_scenario_id_not_filename_and_explicit_role(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            (out / "startup-session").mkdir()
            path = out / "not-the-scenario-id.json"
            path.write_text((HERE / "scenarios/startup-session.json").read_text())
            with patch.object(runner, "Docker"), patch.object(runner, "unused_subnet"), \
                    patch.object(runner, "side"), patch.object(runner.shutil, "copyfile"), \
                    patch.object(runner, "run_owned", return_value=subprocess.CompletedProcess(
                        [], 2, "", "test input failure")) as call:
                status = runner.run_case(
                    path, None, {}, {}, Path("reference"), out, "tshark",
                    role_set="go-rtu", allowlist=Path("reviewed.json"))
            self.assertEqual(status, 2)
            command = call.call_args.args[0]
            self.assertEqual(command[command.index("--scenario-id") + 1], "startup-session")
            self.assertEqual(command[command.index("--role-set") + 1], "go-rtu")
            self.assertEqual(command[command.index("--allowlist") + 1], "reviewed.json")

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


class ImageResolutionTests(unittest.TestCase):
    def test_missing_local_role_or_changed_seal_fails_without_pulling(self):
        for missing in (True, False):
            with self.subTest(missing=missing), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                reference = root / "reference.json"
                reference.write_text(json.dumps({
                    "image": "reference:local", "image_id": "sha256:sealed"}))
                docker = Mock()
                docker.call.side_effect = RuntimeError("missing image") if missing else None
                docker.call.return_value = "sha256:changed"
                with patch.object(runner, "Docker", return_value=docker):
                    with self.assertRaisesRegex(
                            RuntimeError if missing else ValueError,
                            "missing image" if missing else "no longer matches its sealed ID"):
                        runner.side(HERE / "scenarios/startup-session.json",
                                    dict.fromkeys(("rtu", "cc", "cmd", "peer"), "reference:local"),
                                    reference, root / "side", "172.29.110.0/24")
                self.assertTrue(all(call.args[:2] == ("image", "inspect")
                                    for call in docker.call.call_args_list))
                docker.network.assert_not_called()
                docker.cleanup.assert_called_once_with()
                self.assertTrue((root / "side/error.json").exists())


class OwnershipTests(unittest.TestCase):
    def test_failed_process_keeps_observed_exit_code_in_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            result = subprocess.CompletedProcess(["docker", "version"], 7, "", "broken")
            with patch("runtime.run", return_value=result):
                with self.assertRaises(RuntimeError):
                    docker.call("version")
            self.assertEqual(load(Path(directory) / "commands.json")[0]["returncode"], 7)

    def test_capture_uses_immediate_delivery_mtu_snaplen_and_packet_count_ring(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            docker.call = Mock(side_effect=["owned", "", "listening on eth0"])
            docker.capture("endpoint", "tools", True)
            args = docker.call.call_args_list[0].args
            self.assertIn("--immediate-mode", args)
            self.assertEqual(args[args.index("-B") + 1], "262144")
            self.assertEqual(args[args.index("-s") + 1], "2048")
            self.assertIn("-U", args)

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

    def test_truncated_capture_record_fails_loudly(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = Docker(directory, "unit")
            pcap = (
                b"\xd4\xc3\xb2\xa1" + struct.pack("<HHIIII", 2, 4, 0, 0, 2048, 1)
                + struct.pack("<IIII", 1, 0, 3, 4) + b"\x00\x01\x02"
            )

            def call(*args, **kwargs):
                if args[0] == "wait":
                    return "0"
                if args[0] == "logs":
                    return "1 packets captured\n0 packets dropped by kernel\n"
                if args[0] == "cp":
                    (Path(directory) / "capture.pcap").write_bytes(pcap)
                return ""

            docker.call = Mock(side_effect=call)
            with self.assertRaisesRegex(
                    RuntimeError, r"packet 1 is truncated: incl_len 3 < orig_len 4"):
                docker.finish_capture("owned")

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
    def test_unhandled_types_have_valid_request_layouts(self):
        for type_id, cot, payload in ((102, 5, b""),
                                      (103, 6, bytes.fromhex("0000000c170a1a"))):
            with self.subTest(type_id=type_id):
                peer = Peer(Mock(), time.monotonic() + 1)
                peer.asdu = Mock()
                peer.expect = Mock()
                peer.action({"op": "negative", "timeout_s": 1, "args": {
                    "type": type_id, "cot": cot, "ca": 1, "ioa": 0,
                    "value": 0, "expected_cot": 44}}, "unused")
                peer.asdu.assert_called_once_with(type_id, cot, 0, payload, 1)
                peer.expect.assert_called_once_with(type_id, 64 | 44, 0)

    def test_read_mirror_needs_no_payload_byte(self):
        left, right = socket.socketpair()
        with left, right:
            peer = Peer(left, time.monotonic() + 1)
            right.sendall(bytes.fromhex("680d0000020066016c000100000000"))
            self.assertEqual(len(peer.expect(102, 64 | 44, 0)), 13)
            self.assertEqual(right.recv(6), bytes.fromhex("680401000200"))

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
