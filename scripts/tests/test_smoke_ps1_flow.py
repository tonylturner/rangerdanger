"""Smoke wiring/order/failure propagation and encoding, without stack lifecycle."""
import json
import unittest

from .test_setup_ps1_root import ROOT, powershell


TOUCHED_PS1 = (
    "scripts/lib/containd-ui-probe.ps1", "scripts/substation-smoke.ps1",
    "scripts/smoke-test.ps1", "stage-ssd.ps1", "stage-ssd-delta.ps1",
)

FLOW = r"""
$ErrorActionPreference = 'Stop'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/scripts/smoke-test.ps1", [ref]$tokens, [ref]$errors)
foreach ($f in $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]}, $true)) {
    Invoke-Expression $f.Extent.Text
}
$main = @($ast.EndBlock.Statements | Where-Object { $_ -is [Management.Automation.Language.TryStatementAst] })
if ($main.Count -ne 1) { throw 'smoke main block changed' }
$RootDir = $env:REPO
$ExistingStack = $true
$Keep = $false
$script:fail = 0
$script:events = @()
$Expected = @(
    @{order='1.2'; id='baseline-assessment'}, @{order='1.3'; id='segmentation-requirements'},
    @{order='1.4'; id='remediation-planning'}, @{order='2.2'; id='firewall-implementation'},
    @{order='2.3'; id='hardening-configurations'}, @{order='2.3-bonus'; id='vendor-rdp-compromise'},
    @{order='2.4'; id='validation-evidence'}
)
function Get-ChildItem { return @() }
function ConvertTo-DesktopRoot($Path) { return '/daemon/root' }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($DockerArgs[-1] -ne '-q' -or $TimeoutSec -ne 30) { throw 'unexpected Docker call' }
    [pscustomobject]@{ExitCode=0; TimedOut=$false}
}
function Invoke-RestMethod($Uri, $TimeoutSec, $ErrorAction) {
    $path = ([uri]$Uri).AbsolutePath
    $script:events += $path
    switch ($path) {
        '/api/range' {
            if ($env:MODE -eq 'wrong-range') { return @{package='eu-iec104'; phase='ready'} }
            $state = if ($env:MODE -eq 'degraded') { 'degraded' } else { 'protecting' }
            return @{package='us-dnp3-substation'; phase='ready';containd=@{applyId='range-id';protection=$state}}
        }
        '/api/scenarios' {
            return @{scenarios=@($Expected | ForEach-Object {
                @{order=$_.order; id=$_.id; steps='{"description":1,"description":2,"description":3}'}
            })}
        }
        default { return @{ok=$true} }
    }
}
function Invoke-ContaindUiProbe($Origin) {
    if ($Origin -ne 'http://localhost:8088') { throw 'probe origin lost' }
    $script:events += 'probe'
    if ($env:MODE -eq 'probe') { throw 'fixture bundle failure' }
}
function Invoke-SubstationSmoke($Api) {
    if ($Api -ne 'http://localhost:8088') { throw 'substation origin lost' }
    $script:events += 'substation'
    if ($env:MODE -eq 'substation') { throw 'fixture physics failure' }
}
function Invoke-ComposePs($Format) {
    $script:events += 'health'
    return @('service (healthy)') * 8
}
function Invoke-Cleanup {
    Write-Output ('EVENTS=' + (ConvertTo-Json -Compress -InputObject $script:events))
}
Invoke-Expression $main[0].Extent.Text
"""


class SmokeFlowTests(unittest.TestCase):
    def test_new_checks_match_bash_order_and_failures_propagate_without_skipping_health(self):
        source = (ROOT / "scripts/smoke-test.ps1").read_text()
        self.assertIn('. "$PSScriptRoot/lib/containd-ui-probe.ps1"', source)
        self.assertIn('. "$PSScriptRoot/substation-smoke.ps1"', source)
        for mode in ("ok", "probe", "substation", "wrong-range", "degraded"):
            with self.subTest(mode=mode):
                result = powershell(FLOW, MODE=mode)
                self.assertEqual(result.returncode, 0 if mode == "ok" else 1, result.stdout + result.stderr)
                trace = next(line[7:] for line in result.stdout.splitlines() if line.startswith("EVENTS="))
                events = json.loads(trace)
                if mode in ("wrong-range", "degraded"):
                    self.assertEqual(events, ["/api/range"])
                else:
                    self.assertEqual(events, ["/api/range", "/api/health", "/api/build", "probe",
                                              "/api/scenarios", "substation", "health", "health"])

    def test_touched_powershell_is_ascii_bom_free_and_parses(self):
        for path in TOUCHED_PS1:
            with self.subTest(path=path):
                data = (ROOT / path).read_bytes()
                self.assertFalse(data.startswith(b"\xef\xbb\xbf"))
                data.decode("ascii")
                result = powershell(r"""
$tokens = $null; $errors = $null
$null = [Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:PSFILE", [ref]$tokens, [ref]$errors)
if ($errors.Count) { $errors | Out-String | Write-Output; exit 1 }
""", PSFILE=path)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
