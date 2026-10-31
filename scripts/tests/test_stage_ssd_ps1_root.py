"""Execute both stagers' actual enumeration blocks with bounded Docker mocked."""
import unittest

from .test_setup_ps1_root import LOAD, ROOT, powershell


ENUMERATE = r"""
$source = [IO.File]::ReadAllText("$env:REPO/$env:STAGER")
$start = $source.IndexOf('$hadComposeVersion =')
$end = if ($env:STAGER -eq 'stage-ssd.ps1') {
    $source.IndexOf('# Compose interpolation is')
} else { $source.IndexOf('function Get-ImageRepository') }
if ($start -lt 0 -or $end -le $start) { throw 'enumeration block not found' }
$RootDir = $env:SAMPLE
$Version = 'v-test'
$New = 'v-test'
$releaseFiles = @("$RootDir\docker-compose.release.yml",
    "$RootDir\lab-definitions\packages\eu-iec104\compose.release.yml",
    "$RootDir\lab-definitions\packages\us-dnp3-substation\compose.release.yml")
$script:calls = 0
function Die($Message) { throw $Message }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($TimeoutSec -ne 30 -or $DockerArgs.Count -ne 7 -or
        $DockerArgs[0] -ne 'compose' -or $DockerArgs[1] -ne '--project-directory' -or
        $DockerArgs[2] -cne $RootDir -or $DockerArgs[3] -ne '-f' -or
        $DockerArgs[4] -cne $releaseFiles[$script:calls] -or
        $DockerArgs[5] -ne 'config' -or $DockerArgs[6] -ne '--images') { throw 'enumeration CLI changed' }
    $script:calls++
    if ($env:RANGERDANGER_ROOT -cne $env:EXPECTED_ROOT -or
        $env:VERSION -cne 'v-test') { throw 'daemon root/version not converted' }
    if ($env:MODE -eq 'throw') { throw 'fixture launch error' }
    if ($env:MODE -eq 'missing') { return $null }
    $code = if ($env:MODE -eq 'error') { 17 } else { 0 }
    $text = if ($env:MODE -eq 'empty') { '' } else { "image-$script:calls`r`nshared`r`n" }
    [pscustomobject]@{ExitCode=$code; TimedOut=($env:MODE -eq 'timeout'); StdOut=$text}
}
if ($env:PREEXIST -eq '1') {
    $env:RANGERDANGER_ROOT = 'prior root'
    $env:VERSION = 'prior-version'
} else {
    Remove-Item Env:RANGERDANGER_ROOT -ErrorAction SilentlyContinue
    Remove-Item Env:VERSION -ErrorAction SilentlyContinue
}
$failed = $false
try { Invoke-Expression $source.Substring($start, $end - $start) }
catch { $failed = $true; Write-Host "FAIL: $_" }
if ($env:PREEXIST -eq '1') {
    if ($env:RANGERDANGER_ROOT -cne 'prior root' -or $env:VERSION -cne 'prior-version') { throw 'environment not restored' }
} elseif ((Test-Path Env:RANGERDANGER_ROOT) -or (Test-Path Env:VERSION)) { throw 'environment leaked' }
if ($failed -ne ($env:MODE -ne 'ok')) { throw 'incorrect outcome' }
$expectedCalls = if ($env:MODE -eq 'badpath') { 0 } elseif ($env:MODE -in @('error', 'timeout', 'missing', 'throw')) { 1 } else { 3 }
if ($script:calls -ne $expectedCalls) { throw 'enumeration did not visit expected models' }
if (-not $failed -and ($allImages -join ',') -cne 'image-1,image-2,image-3,shared') { throw 'union/deduplication changed' }
"""


class StageSsdRootTests(unittest.TestCase):
    def test_both_stagers_convert_native_root_preserve_cli_paths_and_restore_environment(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            source = (ROOT / stager).read_text()
            self.assertIn('. "$PSScriptRoot/scripts/lib/windows-root.ps1"', source)
            self.assertIn('. "$PSScriptRoot/scripts/lib/docker-bounded.ps1"', source)
            for mode in ("ok", "error", "timeout", "missing", "throw", "empty", "badpath"):
                for preexisting in ("0", "1"):
                    with self.subTest(stager=stager, mode=mode, preexisting=preexisting):
                        result = powershell(
                            LOAD + ENUMERATE, STAGER=stager, MODE=mode, PREEXIST=preexisting,
                            SAMPLE=r"\\server\share\rd" if mode == "badpath" else r"C:\Users\A B\rd",
                            # Simulate Windows explicitly, even when pwsh runs on a POSIX host.
                            OS="Windows_NT", EXPECTED_ROOT="/run/desktop/mnt/host/c/Users/A B/rd",
                        )
                        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
