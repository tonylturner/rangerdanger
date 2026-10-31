"""Windows path contract and read-only bind proof; all Docker calls are mocked."""
from pathlib import Path
import json
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def powershell(script, **env):
    return subprocess.run(
        ["pwsh", "-NoProfile", "-NonInteractive", "-Command", script],
        env=dict(os.environ, REPO=str(ROOT), **env),
        capture_output=True, text=True, timeout=30,
    )


LOAD = """
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/docker-bounded.ps1"
. "$env:REPO/scripts/lib/windows-root.ps1"
"""


class WindowsRootTests(unittest.TestCase):
    def test_drive_case_spaces_and_root(self):
        for path, expected in (
            (r"C:\Users\A B\rd", "/run/desktop/mnt/host/c/Users/A B/rd"),
            (r"d:\x", "/run/desktop/mnt/host/d/x"),
            ("E:/Mixed Case/rd/", "/run/desktop/mnt/host/e/Mixed Case/rd"),
            ("C:\\", "/run/desktop/mnt/host/c"),
        ):
            with self.subTest(path=path):
                result = powershell(LOAD + "ConvertTo-DesktopRoot $env:SAMPLE", SAMPLE=path)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.strip(), expected)

    def test_unc_relative_and_ambiguous_bind_or_dotenv_paths_rejected(self):
        for path in (r"\\wsl$\Ubuntu\home\x", r"\\server\share\x", r"C:relative",
                     "/mnt/c/rd", r"C:\x\..\rd", r"C:\x#tag", r"C:\x$d",
                     r"C:\x,readonly", r"C:\x:stream", "C:\\x\nr"):
            with self.subTest(path=path):
                result = powershell(LOAD + """
try { ConvertTo-DesktopRoot $env:SAMPLE; exit 99 } catch { exit 0 }
""", SAMPLE=path)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_only_desktop_linux_wsl2_is_accepted(self):
        for os_name, desktop, kernel, engine, accepted in (
            ("Windows_NT", "Docker Desktop", "6.6-microsoft-standard-WSL2", "linux", True),
            ("Windows_NT", "Docker Desktop", "linuxkit", "linux", False),
            ("Windows_NT", "Ubuntu", "6.6-WSL2", "linux", False),
            ("Windows_NT", "Docker Desktop", "6.6-WSL2", "windows", False),
            ("Linux", "Docker Desktop", "6.6-WSL2", "linux", False),
        ):
            with self.subTest(kernel=kernel, desktop=desktop, engine=engine):
                info = json.dumps(dict(OperatingSystem=desktop, KernelVersion=kernel, OSType=engine))
                result = powershell(LOAD + """
try { Assert-DesktopWsl2 ($env:INFO | ConvertFrom-Json); exit 0 } catch { exit 1 }
""", OS=os_name, INFO=info)
                self.assertEqual(result.returncode, 0 if accepted else 1, result.stderr)

    def test_probe_requires_exact_sentinel_and_cleans_up_on_every_result(self):
        for mode in ("ok", "wrong", "timeout", "error", "throw", "cleanup"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                result = powershell(LOAD + """
$script:calls = @()
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    $script:calls += ,@($DockerArgs)
    if ($DockerArgs[0] -eq 'rm') {
        $code = if ($env:MODE -eq 'cleanup') { 1 } else { 0 }
        return [pscustomobject]@{ExitCode=$code; TimedOut=$false; StdErr='fixture'}
    }
    if ($TimeoutSec -ne 30) { throw 'unbounded probe' }
    if ($env:MODE -eq 'throw') { throw 'fixture' }
    $file = Get-ChildItem -LiteralPath $env:TESTROOT -Filter '.setup-root-*' -Force
    $text = [IO.File]::ReadAllText($file.FullName)
    if ($env:MODE -eq 'wrong') { $text = "wrong-$text" }
    $code = if ($env:MODE -eq 'error') { 1 } else { 0 }
    [pscustomobject]@{ExitCode=$code; TimedOut=($env:MODE -eq 'timeout'); StdOut=$text}
}
$failed = $false
try { Assert-DesktopRootBind $env:TESTROOT '/run/desktop/mnt/host/c/A B' 'nginx:test' }
catch { $failed = $true }
if ($failed -ne ($env:MODE -ne 'ok')) { throw 'incorrect outcome' }
if (@(Get-ChildItem -LiteralPath $env:TESTROOT -Filter '.setup-root-*' -Force).Count) { throw 'sentinel leaked' }
if ($script:calls.Count -ne 2 -or $script:calls[1][0] -ne 'rm') { throw 'probe cleanup missing' }
$argsJson = ConvertTo-Json -InputObject $script:calls[0] -Compress
$argsJson
""", TESTROOT=tmp, MODE=mode)
                self.assertEqual(result.returncode, 0, result.stderr)
                args = json.loads(result.stdout)
                self.assertEqual(args[args.index("--pull") + 1], "never")
                self.assertIn("--read-only", args)
                self.assertEqual(args[args.index("--network") + 1], "none")
                self.assertIn("type=bind,source=/run/desktop/mnt/host/c/A B,target=/run/desktop/mnt/host/c/A B,readonly", args)
                self.assertIn("/bin/cat", args)
                self.assertEqual(list(Path(tmp).iterdir()), [])

    def test_native_argument_quoting_keeps_spaces_and_trailing_backslash(self):
        result = powershell(LOAD + """
Join-NativeArguments @('plain', 'A B', 'C:\\A B\\', 'a"b', '')
""")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), 'plain "A B" "C:\\A B\\\\" "a\\"b" ""')
