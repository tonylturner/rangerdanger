"""Staging supports POSIX pwsh hosts without Windows Desktop translation."""
import unittest

from .test_setup_ps1_root import LOAD, powershell
from .test_stage_ssd_ps1_root import ENUMERATE


class StageSsdHostTests(unittest.TestCase):
    def test_non_windows_stagers_keep_native_root_and_restore_environment(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            for host_os in ("", "Linux", "Darwin"):
                for mode in ("ok", "error", "timeout", "missing", "throw", "empty"):
                    for preexisting in ("0", "1"):
                        with self.subTest(
                            stager=stager, host_os=host_os, mode=mode, preexisting=preexisting
                        ):
                            result = powershell(
                                LOAD + r"""
function ConvertTo-DesktopRoot { throw 'Windows conversion called on a POSIX host' }
""" + ENUMERATE,
                                STAGER=stager, OS=host_os, MODE=mode, PREEXIST=preexisting,
                                SAMPLE="/Users/A B/rd", EXPECTED_ROOT="/Users/A B/rd",
                            )
                            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
