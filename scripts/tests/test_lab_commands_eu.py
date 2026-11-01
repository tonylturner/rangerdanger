"""EU application oracles and shared worker lifecycle, without a live range."""
import copy
import importlib
import io
import json
import sys
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch

from .test_lab_commands_extract import ROOT

sys.path.insert(0, str(ROOT / "scripts/lib"))
eu = importlib.import_module("lab_commands_eu")
worker = importlib.import_module("lab_commands_smoke")


class EUOracleTests(unittest.TestCase):
    def test_all_eu_port_probes_skip_discovery_and_require_port_evidence(self):
        rows = [r for r in worker.extract(package_id=eu.EU)
                if r["command"].startswith("nmap ")]
        data = json.loads((ROOT / "scripts/lab-commands-expectations-eu-iec104-substation.json").read_text())
        self.assertEqual(len(rows), 9)
        for row in rows:
            with self.subTest(command=worker.gate.key(row)):
                self.assertRegex(row["command"], r"^nmap -Pn -p [\d,]+ \S+ --max-retries 1 -T4$")
                oracle = data["commands"][worker.gate.key(row)]["oracle"]
                down = "Note: Host seems down\nNmap done: 0 hosts up"
                self.assertFalse(worker.gate.verdict(oracle, 0, down, "", control=True))
                ports = row["command"].split()[3].split(",")
                def output(state):
                    return "\n".join(f"{port}/tcp {state}" for port in ports)
                denied = oracle["intent"] == "deny"
                self.assertTrue(worker.gate.verdict(
                    oracle, 0, output("filtered" if denied else "open"), "", control=True))
                self.assertFalse(worker.gate.verdict(
                    oracle, 0, output("open" if denied else "filtered"), "", control=True))
                self.assertFalse(worker.gate.verdict(
                    oracle, 0, output("closed"), "", control=True))
                if denied:
                    control = {"intent": "read", "match": oracle["control_match"]}
                    self.assertTrue(worker.gate.verdict(control, 0, output("open"), ""))
                    self.assertFalse(worker.gate.verdict(control, 0, output("filtered"), ""))

    def test_gi_requires_completed_one_shot_not_connection_or_failure(self):
        self.assertTrue(eu.semantic("gi", '{"event":"result","op":"gi","status":"ok"}'))
        for out in ('{"event":"connected"}', '{"event":"result","op":"gi","status":"failed"}',
                    '{"event":"result","op":"dc","status":"ok"}', "not json"):
            self.assertFalse(eu.semantic("gi", out))

    def test_eleven_registers_must_all_be_present_in_order(self):
        out = "\n".join(f"[{i}]: {i}" for i in range(11))
        self.assertTrue(eu.semantic("registers", out))
        self.assertFalse(eu.semantic("registers", out.replace("[5]: 5", "")))
        self.assertFalse(eu.semantic("registers", "Connected successfully"))

    def test_registers_accept_mbpoll_signed_annotations_without_losing_completeness(self):
        lines = [
            "[0]: \t2", "[1]: \t1", "[2]: \t1", "[3]: \t16903",
            "[4]: \t45050 (-20486)", "[5]: \t16801", "[6]: \t22093",
            "[7]: \t0", "[8]: \t1", "[9]: \t37382 (-28154)", "[10]: \t26260",
        ]
        out = "-- Polling slave 1...\n" + "\n".join(lines) + "\n"
        self.assertTrue(eu.semantic("registers", out))
        for bad in (
                out.replace(lines[4], ""),
                out.replace(lines[4], "[4]: \t45050 (invalid)"),
                out.replace(lines[4], "[4]: \t45050 (-20486) trailing garbage"),
                out + lines[4],
                "\n".join(lines[:4] + [lines[5], lines[4]] + lines[6:])):
            with self.subTest(output=bad):
                self.assertFalse(eu.semantic("registers", bad))

    def test_statistics_and_historian_need_data(self):
        self.assertTrue(eu.semantic("io-stat", "| 0.000 <> 12.000 | 4 | 500 |"))
        self.assertFalse(eu.semantic("io-stat", "| 0.000 <> 12.000 | 0 | 0 |"))
        self.assertFalse(eu.semantic("historian", "[]"))
        self.assertTrue(eu.semantic("historian", '[{"id":"point","ioa":1001,"last":{"value":"closed"}}]'))
        self.assertFalse(eu.semantic("reconnect", '{"trigger":"operator"}'))

    def audit(self, allowed):
        before = {"generation": 2, "latest_seq": 5, "oldest_seq": 1, "entries": []}
        initial = {"generation": 2, "revision": 4, "position": "closed"}
        final = {**initial, "revision": 5, "position": "open"} if allowed else dict(initial)
        entries = [{"seq": 6, "operation": "open", "result": "accepted",
                    "after": {"position": "open"},
                    "context": {"protocol": "modbus", "peer": "10.20.20.10:1234"}}] if allowed else []
        after = {**before, "latest_seq": 6 if allowed else 5, "entries": entries}
        return [before, after, initial, final]

    def test_state_and_fresh_source_attributed_audit_are_both_required(self):
        peer = "10.20.20.10:"
        for allowed in (True, False):
            values = self.audit(allowed)
            self.assertTrue(eu.audit_verdict(*values, peer, allowed))
            corrupt = copy.deepcopy(values)
            corrupt[3]["generation"] += 1
            self.assertFalse(eu.audit_verdict(*corrupt, peer, allowed))
            corrupt = copy.deepcopy(values)
            corrupt[1]["oldest_seq"] = 7
            self.assertFalse(eu.audit_verdict(*corrupt, peer, allowed))
        values = self.audit(True)
        self.assertFalse(eu.audit_verdict(*values, "10.10.10.50:", True))
        values[3]["position"] = "closed"
        self.assertFalse(eu.audit_verdict(*values, peer, True))
        values = self.audit(False)
        values[3]["revision"] += 1
        self.assertFalse(eu.audit_verdict(*values, peer, False))
        values = self.audit(True)
        self.assertFalse(eu.audit_verdict(*values, peer, False))

    def test_eu_exec_uses_node_api_and_retains_both_streams(self):
        row = {"scenario": "fixture", "step": 1, "command": "iec104cmd station gi --json",
               "node": "kali-1"}
        response = dict(stdout='{"event":"result","op":"gi","status":"ok"}', stderr="diagnostic",
                        exit_code=0, duration_ms=1)
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(eu, "request", return_value=response) as request, redirect_stdout(io.StringIO()):
            path = Path(directory)
            adapter = eu.EUAdapter("http://fixture", path)
            self.assertTrue(adapter.execute(worker.gate, row, {
                "intent": "read", "match": '"status":"ok"', "semantic": "gi"}, path, 10))
            self.assertEqual(request.call_args.args[1], "/api/workshop/nodes/kali-1/exec")
            self.assertEqual(request.call_args.args[2]["command"], row["command"])
            self.assertEqual((path / "command.stderr").read_text(), "diagnostic")
            self.assertEqual((path / "command.stdout").read_text(), response["stdout"])


class LifecycleTests(unittest.TestCase):
    def run_fixture(self, mode):
        row = next(r for r in worker.extract(package_id=eu.EU)
                   if r["scenario"] == "eu-vendor-rdp-compromise" and r["step"] == 6
                   and r["command"].startswith("mbpoll"))
        lifecycle = Mock()
        lifecycle.execute.side_effect = ([False] if mode == "control-fail" else
                                         [True, ValueError("tool failed")] if mode == "throw" else
                                         [True, mode == "pass"])
        if mode == "cleanup-fail":
            lifecycle.policy.side_effect = [None, None, ValueError("cleanup failed")]
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(worker, "extract", return_value=[row]), \
                patch.object(worker.gate, "validate_inventory", return_value=1), \
                patch.object(worker, "preflight"), \
                patch.object(worker, "Lifecycle", return_value=lifecycle), redirect_stdout(io.StringIO()):
            if mode in ("throw", "cleanup-fail"):
                with self.assertRaisesRegex(ValueError, "tool failed|cleanup failed"):
                    worker.run(eu.EU, "", "http://fixture", directory, 5)
            else:
                rc = worker.run(eu.EU, "", "http://fixture", directory, 5)
                self.assertEqual(rc, 0 if mode == "pass" else 1)
            summary = json.loads((Path(directory) / "summary.json").read_text())
        self.assertEqual(lifecycle.policy.call_args.args, ("weak",))
        # Reset-before-policy is essential: reset itself applies Weak.
        calls = [c[0] for c in lifecycle.method_calls]
        self.assertEqual(calls[:4], ["reset", "reset", "policy", "execute"])
        self.assertEqual(calls[-2:], ["reset", "policy"])
        self.assertEqual(summary["cleanup_passed"], mode != "cleanup-fail")
        return lifecycle, summary

    def test_controls_cleanup_and_shortfall_are_not_exit_only_verdicts(self):
        for mode in ("pass", "fail", "control-fail", "throw", "cleanup-fail"):
            with self.subTest(mode=mode):
                lifecycle, summary = self.run_fixture(mode)
                if mode == "control-fail":
                    self.assertEqual(lifecycle.execute.call_count, 1)
                    self.assertEqual(summary["unverified"], 1)

    def test_preflight_rejects_wrong_package_without_mutations(self):
        with patch.object(worker.shutil, "which", return_value="/fixture"), \
                patch.object(worker, "request", side_effect=[
                    {}, {"package": "student", "phase": "ready"}]), \
                self.assertRaisesRegex(ValueError, "refusing wrong/non-ready"):
            worker.preflight(eu.EU, "http://fixture")


class EUShellAdapterTests(unittest.TestCase):
    def test_real_dispatch_uses_existing_helpers_and_propagates_failure(self):
        source = (ROOT / "scripts/lib/lab-commands-eu.sh").read_text()
        dispatch = 'case "$1" in' + source.split('case "$1" in', 1)[1]
        stubs = r'''
set -euo pipefail
eu_reset_all() { echo reset; return "${RESULT:-0}"; }
eu_apply_named() { echo "policy:$1"; return "${RESULT:-0}"; }
eu_management() { echo "$*" >&2; HTTP_STATUS=200; API_BODY='{"generation":2}'; }
eu_api_post() { echo "$*"; HTTP_STATUS=200; }
eu_station_shape() { echo '.points | length > 0'; }
eu_wait_for_operator() { echo "$*"; }
'''
        for action, args, output, rc in (
                ("reset", [], "reset", 0), ("policy", ["improved"], "policy:improved", 0),
                ("reset", [], "reset", 42), ("policy", ["weak"], "policy:weak", 42),
                ("reconnect-check", ["2026-10-31T12:00:00Z"], '.link.gi.state == "complete"', 0),
                ("unknown", [], "", 2)):
            with self.subTest(action=action, rc=rc):
                result = subprocess.run(
                    ["bash", "-c", f"RESULT={42 if rc == 42 else 0}\n" + stubs + dispatch,
                     "fixture", action, *args], capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, rc, result.stderr)
                self.assertIn(output, result.stdout)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "audit.json"
            result = subprocess.run(
                ["bash", "-c", stubs + dispatch, "fixture", "management",
                 "rmu-1", "/api/audit?after=5", str(output)],
                capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(output.read_text()), {"generation": 2})


if __name__ == "__main__":
    unittest.main()
