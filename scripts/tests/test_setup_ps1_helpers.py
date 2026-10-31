"""Host-helper lifecycle parity without running a range or Docker."""
from pathlib import Path
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


class WindowsHostHelperTests(unittest.TestCase):
    def test_kernel_probe_uses_local_image_and_never_falls_back_online_when_offline(self):
        for offline, cached in ((True, True), (True, False), (False, False)):
            with self.subTest(offline=offline, cached=cached):
                result = powershell("""
$ErrorActionPreference = 'Stop'
$t = $null; $e = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/scripts/install-wsl-kernel.ps1", [ref]$t, [ref]$e)
foreach ($f in $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]}, $true)) {
    Invoke-Expression $f.Extent.Text
}
function Get-RangeFirewallContainer { return $null }
$Offline = $env:OFFLINE -eq '1'
$FirewallImage = 'built-firewall'
$AlpineImage = 'alpine:test'
$NftProbeScript = 'fixture'
$script:ran = $false
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($DockerArgs[0] -eq 'image') {
        return [pscustomobject]@{ExitCode=([int]($env:CACHED -ne '1')); TimedOut=$false}
    }
    $script:ran = $true
    if ($env:CACHED -eq '1') {
        if ($DockerArgs -notcontains 'built-firewall' -or $DockerArgs -notcontains 'never') { throw 'local image/pull policy lost' }
    } elseif ($Offline) { throw 'offline network fallback' }
    return [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut='rdprobe-rc=0'; StdErr=''}
}
$probe = Test-NftQueueSupported
if ($Offline -and $env:CACHED -ne '1') {
    if ($script:ran -or $probe.Result -ne 'Unknown') { throw 'missing image accepted' }
} elseif (-not $script:ran -or $probe.Result -ne 'Supported') { throw 'local probe not used' }
""", OFFLINE=str(int(offline)), CACHED=str(int(cached)))
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_existing_stack_never_changes_database_or_lifecycle(self):
        for existing in (True, False):
            with self.subTest(existing=existing), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / "scripts").mkdir()
                (root / "backend/data").mkdir(parents=True)
                db = root / "backend/data/rangerdanger.db"
                db.write_text("keep")
                work = root / "work"
                work.mkdir()
                (root / "scripts/dev-up.ps1").write_text(
                    "param([string]$Package)\n"
                    "if ($Package -ne 'us-dnp3-substation') { throw 'US package lost' }\n"
                    "Set-Content -LiteralPath $env:UPCALLED -Value $Package\nexit 0\n"
                )
                (root / "scripts/dev-down.ps1").write_text(
                    "Set-Content -LiteralPath $env:DOWNCALLED -Value called\nexit 0\n"
                )
                result = powershell("""
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/scripts/smoke-test.ps1", [ref]$tokens, [ref]$errors)
$functions = $ast.FindAll({ param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] }, $true)
foreach ($f in $functions) { Invoke-Expression $f.Extent.Text }
$RootDir = $env:TESTROOT
$WorkDir = Join-Path $RootDir 'work'
$UpLog = Join-Path $RootDir 'up.log'
$ExistingStack = $env:EXISTING -eq '1'
$Keep = $false
$script:fail = 0
function Invoke-SetupDocker($DockerArgs, $TimeoutSec) {
    if ($DockerArgs[0] -eq 'stop') { Set-Content -LiteralPath $env:STOPCALLED -Value stopped; return '' }
    return 'backend'
}
$source = [IO.File]::ReadAllText("$env:REPO/scripts/smoke-test.ps1")
$branch = ($source -split "`n" | Where-Object { $_ -match '^\\s*if \\(-not \\$ExistingStack\\) \\{ Initialize-SmokeStack \\}' })
if (@($branch).Count -ne 1) { throw 'lifecycle branch missing' }
Invoke-Expression $branch
Invoke-Cleanup
if ($script:fail) { throw 'smoke helper reported failure' }
""", TESTROOT=tmp, EXISTING=str(int(existing)),
                    UPCALLED=str(root / "up.called"), DOWNCALLED=str(root / "down.called"),
                    STOPCALLED=str(root / "stop.called"))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(db.exists(), existing)
                for marker in ("up.called", "down.called", "stop.called"):
                    self.assertEqual((root / marker).exists(), not existing, marker)

    def test_smoke_ps_uses_both_label_projects_in_empty_cwd(self):
        result = powershell("""
$ErrorActionPreference = 'Stop'
$t = $null; $e = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/scripts/smoke-test.ps1", [ref]$t, [ref]$e)
$f = $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Invoke-ComposePs'}, $true)[0]
Invoke-Expression $f.Extent.Text
$WorkDir = '/empty fixture'
$env:COMPOSE_FILE = 'foreign.yml'
$script:calls = @()
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, $WorkingDirectory) {
    if ($WorkingDirectory -ne $WorkDir -or $env:COMPOSE_FILE) { throw 'unsafe label-only ps' }
    $script:calls += ,@($DockerArgs)
    [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut='service (healthy)'}
}
$lines = @(Invoke-ComposePs '{{.Status}}')
if ($lines.Count -ne 2 -or $script:calls[0][2] -ne 'rangerdanger-platform' -or
    $script:calls[1][2] -ne 'rangerdanger' -or $env:COMPOSE_FILE -ne 'foreign.yml') { throw 'project/restore mismatch' }
""")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_dev_up_forwards_named_setup_parameters_without_policy_reset(self):
        source = (ROOT / "scripts/dev-up.ps1").read_text()
        self.assertIn("-FromSource -SkipFirewallGate @PSBoundParameters", source)
        self.assertNotIn("docker compose", source)
        uninstall = (ROOT / "scripts/uninstall-rangerdanger.ps1").read_text()
        self.assertIn("$DaemonRoot = ConvertTo-DesktopRoot $RootDir", uninstall)
        self.assertIn("$env:RANGERDANGER_ROOT = $DaemonRoot", uninstall)
        self.assertIn("lab-definitions\\packages\\*\\compose.source.yml", uninstall)
