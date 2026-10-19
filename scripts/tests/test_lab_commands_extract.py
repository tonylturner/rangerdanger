import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
HELPER = ROOT / "scripts/lib/lab-commands-extract.py"
spec = importlib.util.spec_from_file_location("extract", HELPER)
extract = importlib.util.module_from_spec(spec)
spec.loader.exec_module(extract)


class ExtractTests(unittest.TestCase):
    def test_inventory_is_reviewed_and_pinned(self):
        rows = extract.extract(ROOT)
        data = json.loads(extract.EXPECTATIONS.read_text())
        extract.validate_inventory(rows, data)
        self.assertEqual(sum(r["action"] == "RUN" for r in rows), data["minimum_executed"])
        self.assertTrue(any(r["command"].startswith("export config") for r in rows))
        self.assertTrue(any(r["prompt"] == "cli" for r in rows))

    def test_cli_and_continuations(self):
        commands = extract.extract_step_commands(
            "    show audit\n    shell # leave CLI\n"
            "    dnp3poll -a 2 \\\n      10.40.40.21:20000\n"
            "    export config > /tmp/policy.json\n")
        self.assertEqual([c for c, _ in commands], [
            "show audit", "shell # leave CLI",
            "dnp3poll -a 2 10.40.40.21:20000",
            "export config > /tmp/policy.json"])

    def test_error_and_empty_inventory_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                [sys.executable, str(HELPER), "--root", directory],
                capture_output=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
        rows = extract.extract(ROOT)
        data = json.loads(extract.EXPECTATIONS.read_text())
        for bad in ([], rows[:-1], rows + rows[:1]):
            with self.subTest(length=len(bad)), self.assertRaises(ValueError):
                extract.validate_inventory(bad, data)

    def test_unknown_or_changed_row_fails(self):
        rows = extract.extract(ROOT)
        rows[0]["command"] += " --changed"
        with self.assertRaises(ValueError):
            extract.validate_inventory(rows, json.loads(extract.EXPECTATIONS.read_text()))

    def test_positive_oracle_rejects_rc1_timeout_and_protocol_errors(self):
        oracle = {"intent": "read", "match": r"\[1\]:\s+[01]"}
        for rc, out in [(1, "[1]: 1"), (124, "[1]: 1"),
                        (0, "Illegal function"), (0, "")]:
            with self.subTest(rc=rc, out=out):
                self.assertFalse(extract.verdict(oracle, rc, out, ""))
        self.assertTrue(extract.verdict(oracle, 0, "[1]: 1", ""))
        self.assertFalse(extract.verdict(oracle, 0, "[1]: 1", "Read failed"))

    def test_deny_requires_control_and_real_denial(self):
        oracle = {"intent": "deny", "match": "timed out|refused"}
        self.assertFalse(extract.verdict(oracle, 1, "", "timed out"))
        self.assertTrue(extract.verdict(oracle, 1, "", "timed out", control=True))
        self.assertFalse(extract.verdict(oracle, 127, "", "not found", control=True))
        self.assertFalse(extract.verdict(oracle, 1, "", "Illegal function", control=True))
        self.assertFalse(extract.verdict(oracle, 0, "success", "", control=True))

    def test_nmap_zero_rc_needs_filtered_ports_and_control(self):
        oracle = {"intent": "deny", "match": r"5900/tcp\s+filtered", "nmap": True}
        self.assertFalse(extract.verdict(oracle, 0, "5900/tcp open", "", control=True))
        self.assertFalse(extract.verdict(oracle, 0, "5900/tcp filtered", ""))
        self.assertTrue(extract.verdict(oracle, 0, "5900/tcp filtered", "", control=True))
        data = json.loads(extract.EXPECTATIONS.read_text())
        row = next(v for k, v in data["commands"].items()
                   if k.startswith("vendor-rdp-compromise|6|nmap "))
        self.assertEqual(row["oracle"]["intent"], "deny")
        summary = ("All 2 scanned ports on 10.20.20.10 are in ignored states.\n"
                   "Not shown: 2 filtered tcp ports (no-response)")
        self.assertTrue(extract.verdict(row["oracle"], 0, summary, "", control=True))
        self.assertFalse(extract.verdict(
            row["oracle"], 0, summary.replace("filtered", "closed"), "", control=True))

    def test_capture_requires_decodable_frames(self):
        oracle = {"intent": "capture"}
        self.assertFalse(extract.verdict(oracle, 124, "", ""))
        self.assertFalse(extract.verdict(oracle, 124, "", "", frames=0))
        self.assertTrue(extract.verdict(oracle, 124, "", "", frames=2))

    def test_cli_error_is_not_masked_by_rc_zero(self):
        oracle = {"intent": "cli", "match": "firewall"}
        self.assertFalse(extract.verdict(oracle, 0, "firewall", "Unknown command"))
        self.assertTrue(extract.verdict(oracle, 0, '{"firewall": {}}', ""))

    def test_real_prompt_no_silent_shell_bypass(self):
        row = {"container": "rangerdanger-firewall", "command": "tcpdump -i any",
               "prompt": "cli", "entry": ""}
        argv, stdin = extract.invocation(row, 5)
        self.assertEqual(argv[4:8], ["timeout", "-s", "INT", "5"])
        self.assertEqual(argv[-2:], ["containd", "cli"])
        self.assertEqual(stdin, "tcpdump -i any\nexit\n")
        row.update(prompt="shell", entry="shell")
        argv, stdin = extract.invocation(row, 5)
        self.assertIn("containd cli", argv[-1])
        self.assertEqual(stdin, "shell\ntcpdump -i any\nexit\n")

    def test_execute_preserves_both_streams(self):
        row = {"scenario": "fixture", "step": 1, "container": "fixture",
               "command": "mbpoll fixture", "prompt": "shell", "entry": ""}
        result = subprocess.CompletedProcess([], 0, "[1]: 1\n", "tool diagnostic\n")
        with tempfile.TemporaryDirectory() as directory, patch.object(
                extract.subprocess, "run", return_value=result) as run:
            self.assertTrue(extract.execute(
                row, {"intent": "read", "match": r"\[1\]: 1"}, Path(directory), 5))
            self.assertEqual((Path(directory) / "command.stdout").read_text(), result.stdout)
            self.assertEqual((Path(directory) / "command.stderr").read_text(), result.stderr)
            self.assertEqual(run.call_args.args[0][:2], ["docker", "exec"])

    def test_export_does_not_pass_on_printed_json_without_file(self):
        row = {"scenario": "fixture", "step": 1, "container": "rangerdanger-firewall",
               "command": "export config > /tmp/policy.json", "prompt": "cli", "entry": ""}
        responses = [subprocess.CompletedProcess([], 0, '{"firewall": {}}', ""),
                     subprocess.CompletedProcess([], 1, "", "No such file")]
        with tempfile.TemporaryDirectory() as directory, patch.object(
                extract.subprocess, "run", side_effect=responses):
            self.assertFalse(extract.execute(
                row, {"intent": "cli", "match": "firewall", "file": "/tmp/policy.json"},
                Path(directory), 5))

    def test_staged_transition_preserves_shell_input(self):
        fixture = (
            "import sys\n"
            "assert sys.stdin.readline().strip() == 'shell'\n"
            "print('__RD_SHELL_READY__', flush=True)\n"
            "assert sys.stdin.readline().strip() == 'echo evidence'\n"
            "print('evidence', flush=True)\n")
        result = extract.run_staged_shell(
            [sys.executable, "-c", fixture], {"entry": "shell", "command": "echo evidence"}, 2)
        self.assertEqual(result.returncode, 0)
        self.assertIn("evidence", result.stdout)

    def test_no_ack_requires_weak_state_change_and_denied_state_preservation(self):
        row = {"scenario": "fixture", "step": 1, "command": "dnp3cmd fixture -no-ack"}
        state = {"field": "reclose_enabled", "initial": True, "allowed": False, "denied": True}
        oracle = {"intent": "deny", "state": state}
        for positive, after, expected in ((True, False, True), (True, True, False),
                                          (False, True, True), (False, False, False)):
            with self.subTest(positive=positive, after=after), tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                (path / ("control.json" if positive else "result.json")).write_text('{"passed":true}')
                observed = [{}, {"reclose_enabled": True}, {"reclose_enabled": after},
                            {}, {"reclose_enabled": True}]
                with patch.object(extract, "device_request", side_effect=observed) as request, \
                        patch.object(extract, "execute_command", return_value=True), \
                        patch.object(extract.time, "sleep"):
                    self.assertEqual(extract.execute(
                        row, oracle, path, 5, positive=positive, control=True), expected)
                    self.assertTrue(request.call_args_list[-2].kwargs["reset"])

    def test_no_ack_cleanup_failure_is_fatal(self):
        row = {"scenario": "fixture", "step": 1, "command": "dnp3cmd fixture -no-ack"}
        state = {"field": "reclose_enabled", "initial": True, "allowed": False, "denied": True}
        observed = [{}, {"reclose_enabled": True}, {"reclose_enabled": True},
                    {}, {"reclose_enabled": False}]
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(extract, "device_request", side_effect=observed), \
                patch.object(extract, "execute_command", return_value=True), \
                patch.object(extract.time, "sleep"), self.assertRaisesRegex(ValueError, "cleanup"):
            extract.execute(row, {"intent": "deny", "state": state}, Path(directory), 5)


if __name__ == "__main__":
    unittest.main()
