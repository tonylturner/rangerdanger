"""PowerShell package installer behavior with fake Docker and HTTP clients."""
from pathlib import Path
import json
import socket
import tempfile
import unittest

from .test_setup_ps1_root import LOAD, ROOT, powershell

LOAD_PACKAGE = LOAD + """
. "$env:REPO/scripts/lib/setup-package.ps1"
. "$env:REPO/scripts/lib/setup-range.ps1"
"""


class WindowsPackageTests(unittest.TestCase):
    def test_models_use_explicit_projects_and_native_project_directory(self):
        for mode, suffix in (("release", "release"), ("offline", "release"), ("source", "source")):
            with self.subTest(mode=mode):
                result = powershell(LOAD_PACKAGE + """
$models = @(Get-SetupModels $env:REPO $env:MODE)
$commands = @(foreach ($m in $models) { ,@(Get-SetupComposeArgs $m @('config', '--images')) })
ConvertTo-Json -InputObject $commands -Compress
""", MODE=mode)
                if result.returncode:
                    self.fail(result.stderr)
                args = json.loads(result.stdout)
                self.assertEqual(len(args), 3)
                self.assertEqual(args[0][2], "rangerdanger-platform")
                for command in args:
                    self.assertEqual(command[4], str(ROOT))
                for command in args[1:]:
                    self.assertEqual(command[2], "rangerdanger")
                    self.assertTrue(command[6].endswith(f"compose.{suffix}.yml"))

    def test_env_is_idempotent_preserves_other_lines_and_has_no_bom(self):
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp) / ".env"
            file.write_text("# keep\nOTHER=é\nVERSION=old\nVERSION=duplicate\nRANGERDANGER_PACKAGE=eu\n")
            result = powershell(LOAD_PACKAGE + """
foreach ($i in 1,2) {
    Write-SetupEnv $env:ENVFILE VERSION v2
    Write-SetupEnv $env:ENVFILE RANGERDANGER_ROOT '/run/desktop/mnt/host/c/A B'
    Write-SetupEnv $env:ENVFILE RANGERDANGER_PACKAGE ''
}
""", ENVFILE=str(file))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(file.read_text().splitlines(), [
                "# keep", "OTHER=é", "VERSION=v2",
                "RANGERDANGER_ROOT=/run/desktop/mnt/host/c/A B",
            ])
            self.assertFalse(file.read_bytes().startswith(b"\xef\xbb\xbf"))

    def test_all_modes_acquire_every_model_and_verify_image_union(self):
        for mode in ("release", "source", "offline"):
            for fail in ("", "pull", "missing-image", "timeout"):
                if mode == "offline" and fail == "pull":
                    continue
                with self.subTest(mode=mode, fail=fail):
                    result = powershell(LOAD_PACKAGE + """
$script:calls = @()
function Start-Sleep { }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    $script:calls += ,@($DockerArgs)
    $failed = (($env:FAIL -eq 'pull' -and $DockerArgs -contains 'pull') -or
        ($env:FAIL -eq 'missing-image' -and $DockerArgs[0] -eq 'image'))
    $text = if ($DockerArgs -contains '--images') { "shared:1`npackage:2`n" } else { '' }
    [pscustomobject]@{ ExitCode=([int]$failed); TimedOut=($env:FAIL -eq 'timeout'); StdOut=$text; StdErr='fixture' }
}
$models = @(Get-SetupModels $env:REPO $env:MODE)
$failed = $false
try { Invoke-SetupAcquisition $models $env:MODE 'images.tar' } catch { $failed = $true }
if ($failed -ne [bool]$env:FAIL) { throw 'incorrect acquisition outcome' }
ConvertTo-Json -InputObject $script:calls -Compress
""", MODE=mode, FAIL=fail)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    calls = json.loads(result.stdout.splitlines()[-1])
                    if not fail:
                        self.assertEqual(sum(c[:2] == ["image", "inspect"] for c in calls), 2)
                        pulls = [c for c in calls if "pull" in c]
                        builds = [c for c in calls if "build" in c]
                        if mode == "offline":
                            self.assertEqual(calls[0], ["load", "-i", "images.tar"])
                            self.assertEqual(pulls, [])
                        else:
                            self.assertEqual(len(pulls), 3)
                            self.assertEqual(len(builds), 3 if mode == "source" else 0)
                            if mode == "source":
                                self.assertTrue(all("--ignore-buildable" in c for c in pulls))

    def test_owner_check_never_short_circuits_delivery_networks_on_migration(self):
        for foreign in ("", "rangerdanger_desk_vendor", "rangerdanger_mgmt_net"):
            with self.subTest(foreign=foreign):
                result = powershell(LOAD_PACKAGE + """
function Invoke-SetupDocker($DockerArgs, $TimeoutSec) {
    if ($DockerArgs[1] -eq 'ls') { return 'id' }
    $network = $DockerArgs[-1]
    if ($network -eq $env:FOREIGN) { return 'someone-else' }
    if ($network -eq 'rangerdanger_mgmt_net') { return 'rangerdanger' }
    return 'rangerdanger-platform'
}
try { $old = Assert-SetupNetworkOwners; if (-not $old) { throw 'migration lost' }; exit 0 }
catch { exit 1 }
""", FOREIGN=foreign)
                self.assertEqual(result.returncode, 1 if foreign else 0, result.stderr)

    def test_connect_probe_and_held_ports_cover_both_projects(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            port = listener.getsockname()[1]
            result = powershell(LOAD_PACKAGE + "Get-SetupPortState ([int]$env:PORT)", PORT=str(port))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), "busy")
        result = powershell(LOAD_PACKAGE + "Get-SetupPortState ([int]$env:PORT)", PORT=str(port))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "free")
        result = powershell(LOAD_PACKAGE + """
function Invoke-SetupDocker($DockerArgs, $TimeoutSec) {
    if ($DockerArgs -contains 'label=com.docker.compose.project=rangerdanger-platform') {
        return '127.0.0.1:8088->8080/tcp'
    }
    return '127.0.0.1:9080->80/tcp,127.0.0.1:2222->22/tcp'
}
@(Get-SetupHeldPorts) | ConvertTo-Json -Compress
""")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(set(json.loads(result.stdout)), {8088, 9080, 2222})

    def test_health_and_range_requests_are_bounded_and_fail_closed(self):
        for scenario in ("ready", "default", "degraded", "missing-receipt", "pending", "failed-receipt", "failed", "preflight-failed", "conflict", "wrong-then-ready", "unknown-then-ready", "timeout", "bad-post", "wrong-post"):
            with self.subTest(scenario=scenario):
                result = powershell(LOAD_PACKAGE + """
class RangeConflict : System.Exception {
    [object]$Response
    RangeConflict() : base('conflict') { $this.Response = [pscustomobject]@{ StatusCode=409 } }
}
$script:posts = 0
$script:gets = 0
function Start-Sleep { }
function Invoke-WebRequest($Uri, $Method, $ContentType, $Body, [switch]$UseBasicParsing, $TimeoutSec, $ErrorAction) {
    if ($Uri.EndsWith('/health')) {
        if ($TimeoutSec -ne 2) { throw 'unbounded health' }
        return [pscustomobject]@{ StatusCode=200 }
    }
    if ($TimeoutSec -ne 30 -or $Method -ne 'POST') { throw 'unbounded POST' }
    $script:posts++
    if ($env:SCENARIO -eq 'default') {
        if ($Body -ne '{}') { throw 'default package must use recorded/backend choice' }
    } elseif (($Body | ConvertFrom-Json).package -ne 'eu') { throw 'package lost' }
    if ($env:SCENARIO -eq 'conflict') { throw [RangeConflict]::new() }
    if ($env:SCENARIO -eq 'bad-post') { return [pscustomobject]@{ StatusCode=202; Content='{}' } }
    if ($env:SCENARIO -eq 'wrong-post') { return [pscustomobject]@{ StatusCode=202; Content='{"target":"eu-other"}' } }
    return [pscustomobject]@{ StatusCode=202; Content='{"target":"eu","phase":"preflight"}' }
}
function Get-RangeStatus($Api) {
    $script:gets++
    if ($env:SCENARIO -eq 'failed') { return [pscustomobject]@{phase='failed';error='fixture'} }
    if ($env:SCENARIO -eq 'preflight-failed') { return [pscustomobject]@{phase='ready';package='us';error='fixture'} }
    if ($env:SCENARIO -eq 'wrong-then-ready' -and $script:gets -eq 1) {
        return [pscustomobject]@{phase='ready';package='us'}
    }
    if ($env:SCENARIO -eq 'unknown-then-ready' -and $script:gets -eq 1) { return $null }
    $status = [pscustomobject]@{phase='ready';package='eu';containd=@{applyId='start-id';protection='protecting'}}
    if ($env:SCENARIO -eq 'degraded') { $status.containd.protection = 'degraded' }
    if ($env:SCENARIO -eq 'missing-receipt') { $status.containd = $null }
    if ($env:SCENARIO -eq 'pending') { $status.containd.protection = 'pending' }
    if ($env:SCENARIO -eq 'failed-receipt') { $status.containd.protection = 'failed' }
    return $status
}
Wait-SetupHealth 'http://fixture' 1
$failed = $false
$package = if ($env:SCENARIO -eq 'default') { '' } else { 'eu' }
$budget = if ($env:SCENARIO -eq 'timeout') { 0 } else { 1 }
try { $result = Start-SetupRange 'http://fixture' $package $budget }
catch { $failed = $true }
$expectedFailure = $env:SCENARIO -in @('degraded','missing-receipt','pending','failed-receipt','failed','preflight-failed','conflict','timeout','bad-post','wrong-post')
if ($failed -ne $expectedFailure) { throw 'incorrect range result' }
if (-not $failed -and $result -ne 'eu') { throw 'wrong package accepted' }
if ($env:SCENARIO -eq 'conflict' -and $script:posts -ne 3) { throw 'lost retry' }
if ($env:SCENARIO -in @('wrong-then-ready','unknown-then-ready') -and $script:gets -ne 2) { throw 'false readiness' }
""", SCENARIO=scenario)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_firewall_gate_requires_apply_and_reset_protection_receipts(self):
        source = (ROOT / "setup.ps1").read_text()
        start = source.index("if ($SkipFirewallGate) {")
        end = source.index("# OpenPLC is used by both", start)
        gate = source[start:end]
        for route in ("valid", "apply", "reset"):
            protections = ("protecting",) if route == "valid" else (
                "empty", "missing", "pending", "degraded", "failed")
            for protection in protections:
                with self.subTest(route=route, protection=protection):
                    result = powershell(LOAD_PACKAGE + r'''
function Say($Message) { }
function Warn($Message) { }
function Die($Message) { throw $Message }
$SkipFirewallGate = $false
function Invoke-WebRequest($Uri, $Method, $ContentType, $Body, [switch]$UseBasicParsing, $TimeoutSec, $ErrorAction) {
    if ($Uri.EndsWith('/api/firewall/health')) {
        if ($TimeoutSec -ne 10) { throw 'unbounded health request' }
        return [pscustomobject]@{StatusCode=200}
    }
    if ($Uri.EndsWith('/api/firewall/apply')) {
        if ($TimeoutSec -ne 60 -or $Method -ne 'POST') { throw 'apply timeout or method changed' }
        $protection = if ($env:ROUTE -eq 'apply') { $env:PROTECTION } else { 'protecting' }
        if ($protection -eq 'empty') { return [pscustomobject]@{Content=''} }
        $receipt = if ($protection -eq 'missing') { '' } else {
            '"containd":{"applyId":"apply-id","protection":"' + $protection + '"}'
        }
        return [pscustomobject]@{Content=('{' + $receipt + '}')}
    }
    if ($Uri.EndsWith('/api/workshop/reset')) {
        if ($TimeoutSec -ne 60 -or $Method -ne 'POST') { throw 'reset timeout or method changed' }
        $protection = if ($env:ROUTE -eq 'reset') { $env:PROTECTION } else { 'protecting' }
        if ($protection -eq 'empty') { return [pscustomobject]@{Content=''} }
        $receipt = if ($protection -eq 'missing') { '' } else {
            ',"containd":{"applyId":"reset-id","protection":"' + $protection + '"}'
        }
        return [pscustomobject]@{Content=('{"success":true' + $receipt + '}')}
    }
    throw "unexpected URL: $Uri"
}
$failed = $false
try {
''' + gate + r'''
} catch { $failed = $true }
if ($failed -ne ($env:ROUTE -ne 'valid')) { throw 'incorrect installer gate result' }
''', ROUTE=route, PROTECTION=protection)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_setup_orders_proof_and_acquisition_before_migration_and_start(self):
        source = (ROOT / "setup.ps1").read_text()
        offsets = [source.index(text) for text in (
            "Invoke-SetupAcquisition $Models", "Assert-DesktopRootBind $RootDir",
            '& "$PSScriptRoot/scripts/dev-down.ps1" -RangeOnly',
            "Banner 'Starting the platform'", "Wait-SetupHealth 'http",
            "$RangePackage = Start-SetupRange",
        )]
        self.assertEqual(offsets, sorted(offsets))
        self.assertLess(source.index("if ($CheckOnly)"), offsets[0])
        self.assertIn("$up += @('--pull', 'never')", source)
        self.assertNotIn("docker-compose.offline.yml", source)
        self.assertLess(len(source.splitlines()), 1000)
        for file in ("setup.ps1", "scripts/dev-up.ps1", "scripts/lib/windows-root.ps1",
                     "scripts/lib/setup-package.ps1", "scripts/lib/setup-range.ps1",
                     "scripts/install-wsl-kernel.ps1"):
            self.assertTrue((ROOT / file).read_bytes().isascii(), file)
