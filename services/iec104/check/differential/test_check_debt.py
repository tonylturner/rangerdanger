# SPDX-License-Identifier: Apache-2.0
"""Offline regressions for first-party check helpers; never invokes Docker."""

import ast
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest.mock import Mock


CHECK = Path(__file__).resolve().parent.parent


def helper(filename, name, **namespace):
    """Load only the helper, not the check script's live orchestration."""
    tree = ast.parse((CHECK / filename).read_text())
    function = next(
        node for node in tree.body
        if isinstance(node, ast.FunctionDef) and node.name == name
    )
    module = ast.Module(body=[function], type_ignores=[])
    exec(compile(module, str(CHECK / filename), "exec"), namespace)
    return namespace[name]


class FixtureControlTests(unittest.TestCase):
    def test_control_failure_aborts_instead_of_continuing(self):
        text = (CHECK / "run.sh").read_text()
        control = text.split("ctl() {", 1)[1].split("\n}", 1)[0]
        with tempfile.TemporaryDirectory() as output:
            for status in (0, 17):
                with self.subTest(status=status):
                    # The shell function shadows docker; no Docker process runs.
                    script = (
                        'OUT="$1"; NET=unused; IMG=unused\n'
                        f'docker() {{ echo fixture-response; return {status}; }}\n'
                        'fail() { echo "FAIL $*"; }\n'
                        'ctl() {' + control + '\n}\n'
                        'ctl http://fixture/set\n'
                        'echo CONTINUED\n'
                    )
                    result = subprocess.run(
                        ["bash", "-c", script, "test", output],
                        capture_output=True, text=True, timeout=5,
                    )
                    self.assertEqual(result.returncode, 0 if status == 0 else 1)
                    self.assertEqual("CONTINUED" in result.stdout, status == 0)
                    self.assertEqual("FAIL fixture control failed:" in result.stdout, status != 0)
            self.assertIn(
                "fixture-response",
                (Path(output) / "fixture-control.log").read_text(),
            )


class LoaderDeadlineTests(unittest.TestCase):
    def test_loader_subprocess_is_bounded(self):
        process = Mock(return_value=types.SimpleNamespace(
            returncode=1, stdout="config error:", stderr="",
        ))
        run = helper(
            "loader_checks.py", "run",
            json=__import__("json"), tempfile=tempfile,
            subprocess=types.SimpleNamespace(run=process),
        )
        run({})
        self.assertEqual(process.call_args.kwargs["timeout"], 5)

    def test_timeout_propagates_as_failure(self):
        process = Mock(side_effect=subprocess.TimeoutExpired("iec104-cc", 5))
        run = helper(
            "loader_checks.py", "run",
            json=__import__("json"), tempfile=tempfile,
            subprocess=types.SimpleNamespace(run=process),
        )
        with self.assertRaises(subprocess.TimeoutExpired):
            run({})


class NegativeConfirmationTests(unittest.TestCase):
    def select(self, cot, expected_cot=7):
        ioa = 3501
        asdu = bytes([46, 1, cot, 0, 1, 0]) + ioa.to_bytes(3, "little") + b"\x81"
        incoming = bytearray.fromhex("68040b000000")
        incoming += bytes([104, 4 + len(asdu), 0, 0, 2, 0]) + asdu
        sock = Mock()
        sock.__enter__ = Mock(return_value=sock)
        sock.__exit__ = Mock(return_value=False)

        def recv(count):
            result = bytes(incoming[:count])
            del incoming[:count]
            return result

        sock.recv.side_effect = recv
        raw_select = helper(
            "inc4_probe.py", "raw_select",
            socket=types.SimpleNamespace(create_connection=Mock(return_value=sock)),
        )
        raw_select(ioa, 46, 1, expected_cot=expected_cot)

    def test_negative_activation_confirmation(self):
        self.select(64 | 7)

    def test_unknown_ioa_requires_exact_cause(self):
        self.select(64 | 47, expected_cot=47)
        with self.assertRaises(AssertionError):
            self.select(64 | 7, expected_cot=47)

    def test_wrong_cause_is_not_a_negative_activation_confirmation(self):
        for cause in (6, 10, 44, 47):
            with self.subTest(cause=cause), self.assertRaises(AssertionError):
                self.select(64 | cause)

    def test_positive_confirmation_is_rejected(self):
        with self.assertRaises(AssertionError):
            self.select(7)


if __name__ == "__main__":
    unittest.main()
