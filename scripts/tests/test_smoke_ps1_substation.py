"""US substation state-machine fixtures; no sleeps, containers or network."""
import json
import re
import subprocess
import unittest

from .test_setup_ps1_root import ROOT, powershell


SUBSTATION = r"""
$ErrorActionPreference = 'Stop'
. "$env:REPO/scripts/substation-smoke.ps1"
$script:commands = @()
$script:dockerCalls = @()
$script:sleeps = @()
$script:reads = 0
$script:cap = $true
$script:auto = $false
$script:tap = 0
$script:regAuto = $true
$script:paused = $false
$script:resumed = $false
function Start-Sleep($Seconds) {
    $script:sleeps += $Seconds
    if ($env:MODE -eq 'exception-paused' -and $script:paused) { throw 'fixture exception while paused' }
}
function Invoke-RestMethod {
    param($Uri, $TimeoutSec, [switch]$UseBasicParsing, $ErrorAction, $Method, $ContentType, $Body)
    $budget = if ($Uri -eq 'http://fixture/api/range') { 10 } else { 5 }
    if ($TimeoutSec -ne $budget -or -not $UseBasicParsing -or $ErrorAction -ne 'Stop') { throw 'HTTP bounds lost' }
    if ($Uri -eq 'http://fixture/api/range') {
        if ($env:MODE -eq 'range-http') { throw 'fixture range unreachable' }
        $package = if ($env:MODE -eq 'package') { 'eu-iec104' } else { 'us-dnp3-substation' }
        $phase = if ($env:MODE -eq 'phase') { 'starting' } else { 'ready' }
        return [pscustomobject]@{package=$package; phase=$phase}
    }
    if ($Uri -match '/command/([^/]+)$') {
        if ($Method -ne 'Post' -or $ContentType -ne 'application/json') { throw 'POST lost' }
        $device = $Matches[1]
        $cmd = $Body | ConvertFrom-Json
        $script:commands += "$device/$($cmd.command)"
        if ($env:MODE -eq 'command-http') { throw 'fixture command failed' }
        if ($device -eq 'capbank') {
            if ($cmd.command -eq 'switch_in') { $script:cap = $true }
            if ($cmd.command -eq 'switch_out') { $script:cap = $false }
            if ($cmd.command -eq 'set_manual') { $script:auto = $false }
            if ($cmd.command -eq 'set_auto') { $script:auto = $true }
        }
        if ($device -eq 'regulator') {
            if ($cmd.command -eq 'set_tap' -and $cmd.value -ne 0) { throw 'baseline tap lost' }
            if ($cmd.command -eq 'set_manual') { $script:regAuto = $false }
            if ($cmd.command -eq 'set_auto') { $script:regAuto = $true }
            if ($cmd.command -eq 'lower_tap' -and $env:MODE -ne 'sag') { $script:tap-- }
        }
        return @{}
    }
    if ($Uri -ne 'http://fixture/api/substation/state') { throw "unexpected URI $Uri" }
    $script:reads++
    if ($env:MODE -eq 'initial-http' -or ($env:MODE -eq 'baseline-http' -and $script:reads -eq 2)) {
        throw 'fixture state unreachable'
    }
    if ($env:MODE -eq 'poll-http' -and $script:auto) { throw 'fixture poll unreachable' }
    if ($script:auto -and $env:MODE -ne 'auto') { $script:cap = $true }
    if ($script:regAuto -and $script:tap -lt 0 -and $env:MODE -ne 'avr') { $script:tap++ }
    $stamp = [DateTimeOffset]::UtcNow.ToString("yyyy-MM-dd'T'HH:mm:ss.fff'Z'")
    if ($env:MODE -eq 'native-date') { $stamp = ('{"solved_at":"' + $stamp + '"}' | ConvertFrom-Json).solved_at }
    if ($env:MODE -eq 'old') { $stamp = [DateTimeOffset]::UtcNow.AddSeconds(-11).ToString("yyyy-MM-dd'T'HH:mm:ss'Z'") }
    if ($env:MODE -eq 'future') { $stamp = [DateTimeOffset]::UtcNow.AddSeconds(12).ToString("yyyy-MM-dd'T'HH:mm:ss'Z'") }
    if ($env:MODE -eq 'invalid-time') { $stamp = 'not-a-date' }
    if ($env:MODE -eq 'missing-time') { $stamp = $null }
    $stale = $script:paused
    if ($env:MODE -eq 'fresh') { $stale = $true }
    if ($env:MODE -in @('stale-transition', 'exception-paused')) { $stale = $false }
    if ($env:MODE -eq 'recover' -and $script:resumed) { $stale = $true }
    if ($env:MODE -eq 'stale-type') { $stale = 'False' }
    $converged = $env:MODE -ne 'converged'
    if ($env:MODE -eq 'converged-type') { $converged = 'True' }
    $criticalEnergized = $env:MODE -notin @('critical-deenergized', 'critical-missing', 'critical-type')
    $generalEnergized = $env:MODE -notin @('general-deenergized', 'general-missing', 'general-type')
    if ($env:MODE -eq 'critical-missing') { $criticalEnergized = $null }
    if ($env:MODE -eq 'general-missing') { $generalEnergized = $null }
    if ($env:MODE -eq 'critical-type') { $criticalEnergized = 'True' }
    if ($env:MODE -eq 'general-type') { $generalEnergized = 'True' }
    $cap = $script:cap
    if ($env:MODE -eq 'cap-out') { $cap = $true }
    if ($env:MODE -eq 'cap-in') { $cap = $false }
    $echo = $script:cap -and $env:MODE -ne 'echo'
    $pf = if ($script:cap) { 0.99 } else { 0.85 }
    if ($env:MODE -eq 'pf') { $pf = 0.85 }
    if ($env:MODE -eq 'pf-missing') { $pf = $null }
    if ($env:MODE -eq 'pf-invalid') { $pf = 'invalid' }
    $devices = @{ relay=@{}; recloser=@{}; regulator=@{}; capbank=@{switched_in=$cap} }
    foreach ($device in 'relay', 'recloser', 'regulator', 'capbank') {
        if ($env:MODE -eq "missing-$device") { $devices.Remove($device) }
    }
    [pscustomobject]@{
        devices=$devices
        physics=@{stale=$stale}
        electrical=@{converged=$converged; solved_at=$stamp; power_factor=$pf;
                     critical_load_energized=$criticalEnergized; general_load_energized=$generalEnergized;
                     capbank_switched_in=$echo; regulator_tap=$script:tap}
    }
}
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($TimeoutSec -ne 5 -or $DockerArgs[-1] -ne 'rangerdanger-opendss-sim') { throw 'Docker scope/bounds lost' }
    $verb = $DockerArgs[0]
    $script:dockerCalls += $verb
    if ($verb -eq 'pause' -and $env:MODE -eq 'pause-missing') { return $null }
    if ($verb -eq 'pause' -and $env:MODE -in @('pause', 'pause-timeout')) {
        return [pscustomobject]@{ExitCode=1; TimedOut=($env:MODE -eq 'pause-timeout'); StdOut=''}
    }
    if ($verb -eq 'unpause' -and $env:MODE -in @('unpause', 'cleanup-health', 'cleanup-unpause')) {
        $unpauses = @($script:dockerCalls | Where-Object { $_ -eq 'unpause' }).Count
        if ($env:MODE -ne 'cleanup-health' -or $unpauses -eq 1) {
            return [pscustomobject]@{ExitCode=1; TimedOut=$false; StdOut=''}
        }
    }
    if ($verb -eq 'pause') { $script:paused = $true }
    if ($verb -eq 'unpause') { $script:paused = $false; $script:resumed = $true }
    $text = ''
    if ($verb -eq 'inspect') {
        $text = if ($env:MODE -in @('health', 'cleanup-health')) { 'unhealthy' } else { 'healthy' }
        if ($env:MODE -eq 'inspect-timeout') { return [pscustomobject]@{ExitCode=-1; TimedOut=$true; StdOut=''} }
    }
    [pscustomobject]@{ExitCode=0; TimedOut=$false; StdOut=$text}
}
$code = 0
try { Invoke-SubstationSmoke 'http://fixture' }
catch { Write-Host "FAIL: $_"; $code = 1 }
Write-Output ('TRACE=' + (ConvertTo-Json -Compress -Depth 8 -InputObject @{
    commands=$script:commands; docker=$script:dockerCalls; sleeps=$script:sleeps; paused=$script:paused
}))
exit $code
"""


class SubstationSmokeTests(unittest.TestCase):
    def run_smoke(self, mode):
        result = powershell(SUBSTATION, MODE=mode)
        trace = next((line[6:] for line in result.stdout.splitlines() if line.startswith("TRACE=")), None)
        self.assertIsNotNone(trace, result.stdout + result.stderr)
        return result, json.loads(trace)

    def test_pass_baseline_commands_ab_auto_avr_and_cleanup(self):
        result, trace = self.run_smoke("ok")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("substation-smoke PASS", result.stdout)
        self.assertEqual(trace["commands"], [
            "relay/clear_fault", "relay/unlock", "relay/close",
            "recloser/clear_fault", "recloser/reset_lockout", "recloser/enable_reclose", "recloser/close",
            "regulator/set_tap", "regulator/set_auto", "capbank/reset_lockout",
            "capbank/set_manual", "capbank/switch_out", "capbank/switch_in",
            "capbank/switch_out", "capbank/set_auto", "regulator/set_manual",
            "regulator/lower_tap", "regulator/lower_tap", "regulator/lower_tap", "regulator/set_auto",
        ])
        self.assertEqual(trace["sleeps"], [5, 4, 4, 2])
        self.assertEqual(trace["docker"], ["pause", "unpause", "inspect", "unpause"])
        self.assertFalse(trace["paused"])

    def test_native_json_timestamp_deserialization(self):
        result, _ = self.run_smoke("native-date")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_range_gate_has_no_mutations(self):
        for mode, message in (("package", "not us-dnp3-substation ready"),
                              ("phase", "not us-dnp3-substation ready"),
                              ("range-http", "fixture range unreachable")):
            with self.subTest(mode=mode):
                result, trace = self.run_smoke(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(message, result.stdout)
                self.assertEqual(trace["commands"], [])
                self.assertEqual(trace["docker"], [])

    def test_each_state_assertion_fails_closed(self):
        cases = {
            **{f"missing-{d}": f"devices.{d} missing" for d in ("relay", "recloser", "regulator", "capbank")},
            "fresh": "physics is stale after baseline reset",
            "stale-type": "physics is stale after baseline reset",
            "converged": "electrical solve is not converged",
            "converged-type": "electrical solve is not converged",
            "critical-deenergized": "solver does not report both critical and general loads energized",
            "critical-missing": "solver does not report both critical and general loads energized",
            "critical-type": "solver does not report both critical and general loads energized",
            "general-deenergized": "solver does not report both critical and general loads energized",
            "general-missing": "solver does not report both critical and general loads energized",
            "general-type": "solver does not report both critical and general loads energized",
            "old": "timestamp is missing, invalid, or older",
            "future": "timestamp is missing, invalid, or older",
            "invalid-time": "timestamp is missing, invalid, or older",
            "missing-time": "timestamp is missing, invalid, or older",
            "cap-out": "cap stayed in after manual switch_out",
            "cap-in": "switch_in not reflected",
            "echo": "switch_in not reflected",
            "pf": "power factor did not rise",
            "pf-missing": "power factor did not rise",
            "pf-invalid": "power factor did not rise",
        }
        for mode, message in cases.items():
            with self.subTest(mode=mode):
                result, trace = self.run_smoke(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(message, result.stdout)
                self.assertEqual(trace["docker"][-1], "unpause")

    def test_bash_baseline_requires_solver_loads_energized(self):
        source = (ROOT / "scripts/substation-smoke.sh").read_text()
        expressions = re.findall(
            r'if echo "\$state" \| jq -e \'(.*?)\' >/dev/null 2>&1; then',
            source, re.DOTALL,
        )
        expression = next(
            (item for item in expressions if "critical_load_energized" in item), None)
        self.assertIsNotNone(expression, "missing solver-energized assertion in Bash smoke")
        fixtures = (
            ({"electrical": {"critical_load_energized": True,
                             "general_load_energized": True}}, 0),
            ({"electrical": {"critical_load_energized": False,
                             "general_load_energized": True}}, 1),
            ({"electrical": {"critical_load_energized": True,
                             "general_load_energized": False}}, 1),
            ({"electrical": {"critical_load_energized": True}}, 1),
            ({"electrical": {"critical_load_energized": "true",
                             "general_load_energized": True}}, 1),
        )
        for state, expected in fixtures:
            with self.subTest(state=state):
                result = subprocess.run(
                    ["jq", "-e", expression], input=json.dumps(state), text=True,
                    capture_output=True, timeout=5,
                )
                self.assertEqual(result.returncode, expected, result.stderr)

    def test_poll_failure_branches_and_iteration_budgets(self):
        for mode, message, seconds in (
            ("auto", "cap AUTO did not switch in within 35s", 35),
            ("sag", "manual tap sag did not register", 30),  # no sag also leaves AVR with no tap rise
            ("avr", "regulator AVR did not correct the sag", 20),
            ("stale-transition", "physics did not become stale within 15s", 15),
            ("recover", "physics did not recover within 15s", 15),
            ("health", "container did not become healthy within 30s after", 30),
            ("inspect-timeout", "container did not become healthy within 30s after", 30),
        ):
            with self.subTest(mode=mode):
                result, trace = self.run_smoke(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(message, result.stdout)
                self.assertEqual(trace["sleeps"].count(1), seconds)
                self.assertFalse(trace["paused"])

    def test_docker_failure_timeout_and_cleanup_branches(self):
        for mode, message in (
            ("pause", "Could not pause"), ("pause-missing", "Could not pause"),
            ("pause-timeout", "Could not pause"), ("unpause", "Could not unpause"),
            ("cleanup-health", "container did not become healthy within 30s during cleanup"),
            ("cleanup-unpause", "Could not unpause"),
            ("exception-paused", "fixture exception while paused"),
        ):
            with self.subTest(mode=mode):
                result, trace = self.run_smoke(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(message, result.stdout)
                self.assertGreaterEqual(trace["docker"].count("unpause"), 1)
                self.assertEqual(trace["paused"], mode in ("unpause", "cleanup-unpause"))
                if mode == "cleanup-health":
                    self.assertEqual(trace["sleeps"].count(1), 75)  # 30 health + 15 stale + 30 cleanup health

    def test_http_errors_are_failures_and_always_attempt_cleanup(self):
        for mode in ("initial-http", "baseline-http", "command-http", "poll-http"):
            with self.subTest(mode=mode):
                result, trace = self.run_smoke(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertEqual(trace["docker"][-1], "unpause")
