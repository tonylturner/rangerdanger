"""Label-only Windows teardown, with fake inventory and native clients."""
import json
from pathlib import Path
import tempfile
import unittest

from test_setup_ps1_root import LOAD, powershell

LOAD_DOWN = LOAD + '. "$env:REPO/scripts/lib/compose-down.ps1"\n'


class WindowsDownTests(unittest.TestCase):
    def test_down_errors_cannot_be_hidden_by_empty_post_inventory(self):
        for scenario in ("ok", "down-error", "down-timeout", "inventory-error", "volume-query-error", "volume-leak", "container-leak", "network-leak"):
            with self.subTest(scenario=scenario):
                result = powershell(LOAD_DOWN + """
$script:calls = @()
$script:down = $false
$script:polls = 0
$script:realWait = ${function:Wait-ProjectVolumesRemoved}
function Wait-ProjectVolumesRemoved($Volumes) { & $script:realWait $Volumes 0 }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, $WorkingDirectory) {
    $script:calls += ,@($DockerArgs)
    $text = ''
    $failed = $false
    $timedOut = $false
    if ($DockerArgs[0] -eq 'compose') {
        if ($WorkingDirectory -ne '/empty fixture' -or $TimeoutSec -ne 180) { throw 'wrong teardown directory/bound' }
        if ($DockerArgs -contains '-f' -or $DockerArgs -contains '--project-directory') { throw 'model-based teardown' }
        $script:down = $true
        $failed = $env:SCENARIO -eq 'down-error'
        $timedOut = $env:SCENARIO -eq 'down-timeout'
    } elseif ($DockerArgs[0] -eq 'ps') {
        if (-not $script:down -or $env:SCENARIO -eq 'container-leak') { $text = 'container' }
        $failed = $env:SCENARIO -eq 'inventory-error'
    } elseif ($DockerArgs[0] -eq 'inspect') {
        $text = "volume`nvolume`n"
    } elseif ($DockerArgs[0] -eq 'network') {
        if (-not $script:down -or $env:SCENARIO -eq 'network-leak') { $text = 'network' }
    } elseif ($DockerArgs[0] -eq 'volume') {
        $script:polls++
        if ($env:SCENARIO -eq 'volume-leak') { $text = 'volume' }
        $failed = $env:SCENARIO -eq 'volume-query-error'
    }
    [pscustomobject]@{ ExitCode=([int]$failed); TimedOut=$timedOut; StdOut=$text; StdErr='fixture' }
}
$failed = $false
try {
    Remove-ComposeProject 'rangerdanger' '/empty fixture'
    Remove-ComposeProject 'rangerdanger-platform' '/empty fixture'
} catch { $failed = $true }
if ($failed -ne ($env:SCENARIO -ne 'ok')) { throw 'incorrect teardown result' }
ConvertTo-Json -InputObject $script:calls -Compress
""", SCENARIO=scenario)
                self.assertEqual(result.returncode, 0, result.stderr)
                calls = json.loads(result.stdout.splitlines()[-1])
                downs = [c for c in calls if c[0] == "compose"]
                if scenario == "inventory-error":
                    self.assertEqual(downs, [])
                else:
                    self.assertEqual(len(downs), 1)  # platform empty on success; never reached on failure
                    self.assertIn("-v", downs[0])
                self.assertFalse(any(c[0] == "compose" and c[2] == "rangerdanger-platform" for c in calls))

    def test_platform_down_never_removes_volumes(self):
        result = powershell(LOAD_DOWN + """
$script:down = $false
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, $WorkingDirectory) {
    if ($DockerArgs[0] -eq 'compose') {
        if ($DockerArgs -contains '-v') { throw 'platform volume deletion' }
        $script:down = $true
    }
    $text = if (-not $script:down) { 'id' } else { '' }
    [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut=$text}
}
Remove-ComposeProject 'rangerdanger-platform' '/empty fixture'
if (-not $script:down) { throw 'platform not stopped' }
""")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_delayed_removal_is_polled_but_persistent_leak_is_returned(self):
        result = powershell(LOAD_DOWN + """
$script:polls = 0
function Start-Sleep { }
function Get-DownOutput($DockerArgs) {
    $script:polls++
    if ($script:polls -eq 1) { return 'volume' }
}
$left = @(Wait-ProjectVolumesRemoved @('volume') 1)
if ($left.Count -or $script:polls -ne 2) { throw 'removal race not handled' }
function Get-DownOutput($DockerArgs) { return 'volume' }
$left = @(Wait-ProjectVolumesRemoved @('volume') 0)
if ($left.Count -ne 1 -or $left[0] -ne 'volume') { throw 'leak hidden' }
""")
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_bounded_native_helper_uses_explicit_cwd_and_times_out(self):
        with tempfile.TemporaryDirectory(prefix="rd cwd ") as tmp:
            result = powershell(LOAD + """
function Get-Command { [pscustomobject]@{ Source='/bin/pwd' } }
$run = Invoke-DockerBounded @() 2 $env:WORKDIR
if ($run.ExitCode -ne 0 -or $run.StdOut.Trim() -ne $env:WORKDIR) { throw 'native cwd ignored' }
Push-Location $env:WORKDIR
try { $run = Invoke-DockerBounded @() 2 } finally { Pop-Location }
if ($run.ExitCode -ne 0 -or $run.StdOut.Trim() -ne $env:WORKDIR) { throw 'caller filesystem cwd ignored' }
function Get-Command { [pscustomobject]@{ Source='/bin/sleep' } }
$run = Invoke-DockerBounded @('20') 1
if (-not $run.TimedOut -or $run.ExitCode -ne -1) { throw 'native timeout ignored' }
""", WORKDIR=str(Path(tmp).resolve()))
            self.assertEqual(result.returncode, 0, result.stderr)
