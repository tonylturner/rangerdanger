"""Docker floor parity, using pwsh and bash only; never real Docker."""
from pathlib import Path
import shutil
import tempfile
import unittest

from .test_setup_bounds import run_bash
from .test_setup_ps1_root import ROOT, powershell

LOAD_FLOOR = """
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/lib/docker-floor.ps1"
"""


class WindowsDockerFloorTests(unittest.TestCase):
    def test_floor_result_and_refusal_match_bash(self):
        cases = (
            ("2.36.0", "1.49", True), ("v2.36.0", "1.49", True),
            ("2.36.0-desktop.1", "1.49", True), ("5.5.1", "1.56", True),
            ("2.35.1", "1.49", False), ("2.36.0", "1.48", False),
            ("2.40.0", "1.9", False), ("", "1.49", False),
            ("2.36.0", "", False), ("abc", "1.49", False),
            ("2.36", "1.49.0", True), ("2", "1.49", False),
            ("2.36.0+build.1", "v1.49", True),
            ("02.036.000", "01.049", True), ("V2.36.0", "1.49", False),
            ("2.36.nope", "1.49", False),
        )
        for compose, api, accepted in cases:
            with self.subTest(compose=compose, api=api):
                bash = run_bash(
                    '. "$ROOT/scripts/lib/docker-floor.sh"\n'
                    'docker_floor_check "$COMPOSE" "$API"',
                    ROOT=str(ROOT), COMPOSE=compose, API=api,
                )
                ps = powershell(LOAD_FLOOR + """
$problem = Get-DockerFloorProblem $env:COMPOSE $env:API
if ($problem) { Write-Output $problem; exit 1 }
""", COMPOSE=compose, API=api)
                self.assertEqual(bash.returncode, 0 if accepted else 1, bash.stderr)
                self.assertEqual(ps.returncode, bash.returncode, ps.stderr)
                self.assertEqual(ps.stdout, bash.stdout)
                self.assertEqual(ps.stderr, "")

    def test_comparison_semantics_match_bash(self):
        cases = (
            ("1.49", "1.49.0", True), ("1.49.1", "1.49", True),
            ("1.9", "1.49", False), ("v2.36.0", "v2.36", True),
            ("2.36.0-desktop.1", "2.36.0+build", True),
            ("", "0", False), ("abc", "1", False),
            ("1.bad", "1.0", False), ("1.0", "1.bad", False),
            ("2..0", "2.0", True), ("2.36.0.1", "2.36.0", True),
            # The bash helper stops at a difference, not at later malformed parts.
            ("3.bad", "2.36.0", True), ("1.bad", "2.36.0", False),
        )
        for have, want, accepted in cases:
            with self.subTest(have=have, want=want):
                bash = run_bash(
                    '. "$ROOT/scripts/lib/docker-floor.sh"\n'
                    'version_at_least "$HAVE" "$WANT"',
                    ROOT=str(ROOT), HAVE=have, WANT=want,
                )
                ps = powershell(LOAD_FLOOR + """
if (Test-VersionAtLeast $env:HAVE $env:WANT) { exit 0 } else { exit 1 }
""", HAVE=have, WANT=want)
                self.assertEqual(bash.returncode, 0 if accepted else 1, bash.stderr)
                self.assertEqual(ps.returncode, bash.returncode, ps.stderr)

    def test_setup_refuses_before_other_preflight_or_writes_including_check_only(self):
        cases = (
            ("2.35.0", "1.51", False, True), ("2.38.2", "1.48", False, True),
            ("", "1.49", False, True), ("2.36.0", "", False, True),
            ("2.36.0", "1.49", True, True), ("v2.40.0-desktop.1", "1.56", True, True),
            ("2.35.0", "1.51", False, False), ("2.38.2", "1.48", False, False),
        )
        for compose, api, accepted, check_only in cases:
            with self.subTest(compose=compose, api=api, check_only=check_only), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                lib = root / "scripts/lib"
                lib.mkdir(parents=True)
                shutil.copyfile(ROOT / "setup.ps1", root / "setup.ps1")
                for name in ("docker-bounded.ps1", "docker-floor.ps1", "setup-range.ps1", "range.ps1"):
                    shutil.copyfile(ROOT / "scripts/lib" / name, lib / name)
                # Mock only Windows host/path discovery and external dependencies.
                # The whole installer and its bounded Docker wrapper run unchanged.
                (lib / "windows-root.ps1").write_text("""
. "$env:REPO/scripts/lib/windows-root.ps1"
function ConvertTo-DesktopRoot($WindowsPath) { return '/run/desktop/mnt/host/c/fixture' }
function Assert-LocalRepoDrive($WindowsPath) { }
""")
                (lib / "setup-package.ps1").write_text("""
. "$env:REPO/scripts/lib/setup-package.ps1"
function Get-SetupModels($HostRoot, $Mode) {
    [pscustomobject]@{Project='rangerdanger-platform'; Package=''; Root=$HostRoot}
}
function Get-SetupPortState($Port) { return 'free' }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($TimeoutSec -le 0 -or $TimeoutSec -gt 30) { throw 'unbounded preflight' }
    $command = $DockerArgs -join ' '
    Add-Content -LiteralPath $env:CALLS -Value $command
    $text = switch ($command) {
        'info --format {{json .}}' {
            '{"OperatingSystem":"Docker Desktop","KernelVersion":"6.6-WSL2","OSType":"linux","Architecture":"amd64","MemTotal":8589934592}'
        }
        'compose version --short' { $env:COMPOSE }
        'version --format {{.Server.APIVersion}}' { $env:API }
        default {
            if ($DockerArgs[0] -ne 'ps') { throw "Unexpected Docker call: $command" }
            ''
        }
    }
    [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut=$text; StdErr=''}
}
""")
                calls_file = root / "calls"
                before = {p.relative_to(root): p.read_bytes() for p in root.rglob("*") if p.is_file()}
                ps = powershell(
                    '& "$env:FIXTURE/setup.ps1" -CheckOnly:($env:CHECKONLY -eq "1") -SkipKernelFix',
                    FIXTURE=tmp, CALLS=str(calls_file), OS="Windows_NT",
                    COMPOSE=compose, API=api, CHECKONLY=str(int(check_only)),
                )
                self.assertEqual(ps.returncode, 0 if accepted else 1, ps.stdout + ps.stderr)
                self.assertTrue(calls_file.exists(), ps.stdout + ps.stderr)
                calls = calls_file.read_text().splitlines()
                self.assertEqual(calls[:3], [
                    "info --format {{json .}}", "compose version --short",
                    "version --format {{.Server.APIVersion}}",
                ])
                if accepted:
                    self.assertIn(f"Compose {compose} on Engine API {api}", ps.stdout)
                    self.assertIn("Pre-flight passed", ps.stdout)
                    self.assertTrue(all(call.startswith("ps ") for call in calls[3:]))
                else:
                    self.assertEqual(len(calls), 3)
                    bash = run_bash(
                        '. "$ROOT/scripts/lib/docker-floor.sh"\n'
                        'docker_floor_check "$COMPOSE" "$API"',
                        ROOT=str(ROOT), COMPOSE=compose, API=api,
                    )
                    self.assertEqual(bash.returncode, 1, bash.stderr)
                    self.assertIn("[x] " + bash.stdout.strip(), ps.stdout)
                    self.assertNotIn("Architecture:", ps.stdout)
                after = {p.relative_to(root): p.read_bytes() for p in root.rglob("*")
                         if p.is_file() and p != calls_file}
                self.assertEqual(before, after)

    def test_touched_powershell_is_ascii_bom_free_and_parses(self):
        for path in ("setup.ps1", "scripts/lib/docker-floor.ps1"):
            with self.subTest(path=path):
                data = (ROOT / path).read_bytes()
                self.assertTrue(data.isascii())
                self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
                ps = powershell("""
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:PSFILE", [ref]$tokens, [ref]$errors)
if ($errors.Count) { $errors | Out-String | Write-Output; exit 1 }
""", PSFILE=path)
                self.assertEqual(ps.returncode, 0, ps.stdout + ps.stderr)
