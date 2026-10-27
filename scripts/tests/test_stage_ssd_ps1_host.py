"""Staging supports POSIX pwsh hosts without Windows Desktop translation."""
import tempfile
import unittest
from pathlib import Path

from .test_setup_ps1_root import LOAD, powershell
from .test_stage_ssd_ps1_root import ENUMERATE


class StageSsdHostTests(unittest.TestCase):
    def test_tar_discovery_without_systemroot_and_windows_preference(self):
        for mode in ("unset", "missing", "native"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                native_tar = Path(tmp) / "System32" / "tar.exe"
                if mode == "native":
                    native_tar.parent.mkdir()
                    native_tar.touch()
                result = powershell(LOAD + r"""
$source = [IO.File]::ReadAllText("$env:REPO/stage-ssd.ps1")
$start = $source.IndexOf('$TarExe =')
$end = $source.IndexOf('# --- Enumerate')
if ($start -lt 0 -or $end -le $start) { throw 'tar discovery block not found' }
if ($env:MODE -eq 'unset') {
    Remove-Item Env:SystemRoot -ErrorAction SilentlyContinue
} else { $env:SystemRoot = $env:WORK }
$expected = if ($env:MODE -eq 'native') {
    Join-Path $env:WORK 'System32/tar.exe'
} else {
    (Get-Command tar -CommandType Application | Select-Object -First 1).Source
}
if ($env:MODE -eq 'native') {
    function Get-Command { throw 'PATH fallback used despite native Windows tar' }
}
function Die($message) { throw $message }
Invoke-Expression $source.Substring($start, $end - $start)
if ($TarExe -cne $expected) { throw "incorrect tar: $TarExe; expected $expected" }
""", MODE=mode, WORK=tmp)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

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
