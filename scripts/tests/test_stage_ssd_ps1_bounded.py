"""Bounded staging calls; native fixtures and mocked Docker, never a daemon."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

from .test_setup_ps1_root import ROOT, powershell


LOAD = r"""
$ErrorActionPreference='Stop'
. "$env:REPO/scripts/lib/docker-bounded.ps1"
. "$env:REPO/scripts/lib/stage-bounded.ps1"
"""


class StageBoundedTests(unittest.TestCase):
    def test_native_capture_streaming_exit_timeout_missing_and_argument_quoting(self):
        fixture = """
import json, os, subprocess, sys, time
mode = sys.argv[1]
if mode == "sleep":
    time.sleep(20)
elif mode == "pipes":
    subprocess.Popen([sys.executable, "-c", "import time; time.sleep(3)"])
else:
    print("STDOUT=" + json.dumps(dict(cwd=os.getcwd(), args=sys.argv[2:])), flush=True)
    print("fixture stderr", file=sys.stderr, flush=True)
    sys.exit(7 if mode == "error" else 0)
"""
        with tempfile.TemporaryDirectory(prefix="stage native ") as tmp:
            script = Path(tmp) / "native fixture.py"
            script.write_text(fixture)
            for mode in ("ok", "error", "sleep", "pipes", "stream", "missing"):
                with self.subTest(mode=mode):
                    result = powershell(LOAD + r"""
$exe=if ($env:MODE -eq 'missing') { Join-Path $env:WORK 'absent-executable' } else { $env:PYTHON }
$mode=if ($env:MODE -eq 'stream') { 'ok' } else { $env:MODE }
$clock=[Diagnostics.Stopwatch]::StartNew()
$run=Invoke-NativeBounded $exe @($env:FIXTURE, $mode, 'A B', 'a"b', 'C:\A B\', '') 1 $env:WORK -StreamOutput:($env:MODE -eq 'stream')
Write-Output ('RESULT=' + (ConvertTo-Json -Compress -InputObject @{run=$run; elapsed=$clock.Elapsed.TotalSeconds}))
""", MODE=mode, WORK=tmp, PYTHON=sys.executable, FIXTURE=str(script))
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    answer = json.loads(next(line[7:] for line in result.stdout.splitlines() if line.startswith("RESULT=")))
                    run = answer["run"]
                    if mode == "missing":
                        self.assertIsNone(run)
                    elif mode in ("sleep", "pipes"):
                        self.assertTrue(run["TimedOut"])
                        self.assertEqual(run["ExitCode"], -1)
                        self.assertLess(answer["elapsed"], 2.5)
                    else:
                        self.assertFalse(run["TimedOut"])
                        self.assertEqual(run["ExitCode"], 7 if mode == "error" else 0)
                        if mode == "stream":
                            self.assertEqual(run["StdOut"], "")
                            self.assertIn("STDOUT=", result.stdout)
                            self.assertIn("fixture stderr", result.stderr)
                        else:
                            payload = json.loads(run["StdOut"].strip()[7:])
                            self.assertEqual(Path(payload["cwd"]).resolve(), Path(tmp).resolve())
                            self.assertEqual(payload["args"], ["A B", 'a"b', "C:\\A B\\", ""])
                            self.assertIn("fixture stderr", run["StdErr"])

    def test_stage_wrappers_reject_missing_timeout_exit_error_and_preserve_stream_flag(self):
        for mode in ("ok", "missing", "timeout", "error", "throw"):
            with self.subTest(mode=mode):
                result = powershell(LOAD + r"""
function Invoke-NativeBounded($Executable, $ArgList, $TimeoutSec) {
    if ($Executable -ne 'git' -or $TimeoutSec -ne 600) { throw 'native budget lost' }
    if ($env:MODE -eq 'throw') { throw 'fixture launch failure' }
    if ($env:MODE -eq 'missing') { return $null }
    [pscustomobject]@{ExitCode=([int]($env:MODE -eq 'error')); TimedOut=($env:MODE -eq 'timeout'); StdOut='payload'; StdErr='detail'}
}
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, [switch]$StreamOutput) {
    if ($DockerArgs[0] -ne 'save' -or $TimeoutSec -ne 7200 -or -not $StreamOutput) { throw 'save budget/progress lost' }
    Invoke-NativeBounded 'git' @() 600
}
foreach ($operation in 'native', 'docker') {
    $failed=$false
    try {
        $text=if ($operation -eq 'native') { Invoke-StageNative 'git' @('archive') 600 }
              else { Invoke-StageDocker @('save') 7200 -StreamOutput }
        if ($text -ne 'payload') { throw 'stdout lost' }
    } catch { $failed=$true }
    if ($failed -ne ($env:MODE -ne 'ok')) { throw "incorrect $operation outcome" }
}
""", MODE=mode)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_both_stage_functions_budget_each_operation_and_remove_partial_saves(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            for arch in ("amd64", "arm64"):
                for mode in ("ok", "pull-error", "tag-error", "save-error", "save-timeout"):
                    with self.subTest(stager=stager, arch=arch, mode=mode), tempfile.TemporaryDirectory() as tmp:
                        result = powershell(LOAD + r"""
$tokens=$null; $errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:STAGER", [ref]$tokens, [ref]$errors)
$function=$ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Invoke-StageArch'}, $true)[0]
Invoke-Expression $function.Extent.Text
function Say($Message) {}
function Banner($Message) {}
function Die($Message) { throw $Message }
$OutDir=$env:WORK
$sample='ghcr.io/tonylturner/rangerdanger-backend:v2'
$resolved=@($sample)
$changed=New-Object 'System.Collections.Generic.List[string]'
$changed.Add($sample)
$BINFMT_IMAGE='binfmt:fixture'
$preflightRefs=@{}
foreach ($image in $sample, $BINFMT_IMAGE) {
    $preflightRefs[$image]=@{amd64='image@sha256:amd'; arm64='image@sha256:arm'; arm64Cross=$false}
}
$script:calls=@()
function Invoke-DockerBounded($DockerArgs, $TimeoutSec, [switch]$StreamOutput) {
    $verb=$DockerArgs[0]
    $script:calls += $verb
    $budget=switch ($verb) { 'pull' {3600} 'tag' {60} 'save' {7200} default {throw 'unexpected call'} }
    if ($TimeoutSec -ne $budget -or [bool]$StreamOutput -ne ($verb -ne 'tag')) { throw 'budget/progress lost' }
    if ($verb -eq 'save') {
        [IO.File]::WriteAllText($DockerArgs[2], 'fixture archive')
        if ($DockerArgs -notcontains $sample -or ($env:ARCH -eq 'arm64' -and $DockerArgs -notcontains $BINFMT_IMAGE)) { throw 'save image union lost' }
    }
    $bad=$env:MODE -eq "$verb-error" -or ($verb -eq 'save' -and $env:MODE -eq 'save-timeout')
    [pscustomobject]@{ExitCode=([int]$bad); TimedOut=($env:MODE -eq 'save-timeout' -and $verb -eq 'save'); StdOut=''; StdErr='fixture'}
}
$failed=$false
try { Invoke-StageArch $env:ARCH } catch { $failed=$true }
if ($failed -ne ($env:MODE -ne 'ok')) { throw 'incorrect outcome' }
$files=@(Get-ChildItem -LiteralPath $OutDir -File)
if ($files.Count -ne [int]($env:MODE -eq 'ok')) { throw 'partial archive left behind or successful save lost' }
$expected=if ($env:MODE -eq 'pull-error') { 'pull' }
    elseif ($env:MODE -eq 'tag-error') { 'pull,tag' }
    elseif ($env:ARCH -eq 'arm64') { 'pull,tag,pull,tag,save' } else { 'pull,tag,save' }
if (($script:calls -join ',') -cne $expected) { throw 'operation ordering changed or failure ignored' }
""", STAGER=stager, ARCH=arch, MODE=mode, WORK=tmp,
                       RD_STAGE_PULL_ATTEMPTS="1", RD_STAGE_PULL_DELAY_S="0")
                        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_stager_command_inventory_is_bounded_and_powershell_is_ascii_parseable(self):
        for name in ("stage-ssd.ps1", "stage-ssd-delta.ps1",
                     "scripts/lib/docker-bounded.ps1", "scripts/lib/stage-bounded.ps1"):
            with self.subTest(name=name):
                (ROOT / name).read_bytes().decode("ascii")
                result = powershell(r"""
$tokens=$null; $errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:FILE", [ref]$tokens, [ref]$errors)
if ($errors.Count) { $errors | Out-String | Write-Output; exit 1 }
$commands=@($ast.FindAll({param($n) $n -is [Management.Automation.Language.CommandAst]}, $true))
if ($env:FILE -like 'stage-ssd*') {
    foreach ($command in $commands) {
        if ($command.InvocationOperator -eq [Management.Automation.Language.TokenKind]::Ampersand) { throw "unbounded native call: $command" }
    }
}
ConvertTo-Json -Compress -InputObject @($commands | Where-Object {
    $_.GetCommandName() -in @('Invoke-WebRequest', 'Invoke-StageDocker', 'Invoke-StagePull', 'Invoke-StageNative', 'Invoke-DockerBounded')
} | ForEach-Object { @{name=$_.GetCommandName(); text=$_.Extent.Text} })
""", FILE=name)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                if name.startswith("stage-ssd"):
                    commands = json.loads(result.stdout)
                    expected_count = 12 if name == "stage-ssd.ps1" else 10
                    self.assertEqual(len(commands), expected_count)
                    for command in commands:
                        text = command["text"]
                        if command["name"] == "Invoke-WebRequest":
                            budget = 10 if "-Method Head" in text else 600 if "$kernelUrl" in text else 120
                            self.assertIn(f"-TimeoutSec {budget}", text)
                            self.assertIn("-UseBasicParsing", text)
                        else:
                            budget = (
                                3600 if command["name"] == "Invoke-StagePull" else
                                30 if "'compose'" in text else 120 if "'buildx'" in text else
                                3600 if "'pull'" in text else 60 if "'tag'" in text else
                                7200 if "'save'" in text else 600 if "'archive'" in text else
                                1800 if "'-xf'" in text else 30
                            )
                            self.assertRegex(text, rf"\s{budget}(?:\s+-StreamOutput)?$")

    def test_manifest_bounds_preserve_optional_old_reference_and_rate_limit_semantics(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            roles = ("required",) if stager == "stage-ssd.ps1" else ("required", "optional", "new")
            for role in roles:
                for mode in ("ok", "error", "rate", "missing", "timeout", "empty", "invalid-json"):
                    with self.subTest(stager=stager, role=role, mode=mode):
                        result = powershell(LOAD + r"""
$tokens=$null; $errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:STAGER", [ref]$tokens, [ref]$errors)
foreach ($f in $ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst]}, $true)) {
    Invoke-Expression $f.Extent.Text
}
function Die($Message) { throw $Message }
function Invoke-DockerBounded($DockerArgs, $TimeoutSec) {
    if ($TimeoutSec -ne 120 -or ($DockerArgs -join '|') -cne 'buildx|imagetools|inspect|alpine:fixture|--format|{{json .}}') { throw 'manifest call/budget changed' }
    if ($env:MODE -eq 'missing') { return $null }
    $body='{"Manifest":{"Digest":"sha256:root","Manifests":[{"Platform":{"OS":"linux","Architecture":"amd64"},"Digest":"sha256:amd"},{"Platform":{"OS":"linux","Architecture":"arm64"},"Digest":"sha256:arm"}]}}'
    if ($env:MODE -eq 'empty') { $body='' }
    if ($env:MODE -eq 'invalid-json') { $body='not-json' }
    $detail=if ($env:MODE -eq 'rate') { '429 Too Many Requests' } else { 'fixture' }
    [pscustomobject]@{ExitCode=([int]($env:MODE -in @('rate', 'error'))); TimedOut=($env:MODE -eq 'timeout'); StdOut=$body; StdErr=$detail}
}
$failed=$false
try {
    $refs=if ($env:STAGER -eq 'stage-ssd.ps1') { Get-PlatformRefs 'alpine:fixture' }
        else {
            $manifest=Get-PlatformManifest 'alpine:fixture' -Optional:($env:ROLE -eq 'optional') -NewVersion:($env:ROLE -eq 'new')
            if ($manifest) { $manifest.Refs } else { $null }
        }
} catch { $failed=$true }
$shouldFail=$env:MODE -ne 'ok' -and ($env:ROLE -ne 'optional' -or $env:MODE -eq 'rate')
if ($failed -ne $shouldFail) { throw 'manifest failure/optional/rate-limit behavior changed' }
if ($env:MODE -eq 'ok') {
    if ($refs.amd64 -cne 'alpine@sha256:amd' -or $refs.arm64 -cne 'alpine@sha256:arm') { throw 'platform refs changed' }
} elseif (-not $failed -and $refs) { throw 'failed optional inspection produced a manifest' }
""", STAGER=stager, ROLE=role, MODE=mode)
                        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_git_archive_partial_cleanup_and_tar_verification_cleanup(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            for mode in ("ok", "error", "timeout", "missing"):
                with self.subTest(stager=stager, mode=mode), tempfile.TemporaryDirectory() as tmp:
                    result = powershell(LOAD + r"""
$tokens=$null; $errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile("$env:REPO/$env:STAGER", [ref]$tokens, [ref]$errors)
$archive=@($ast.EndBlock.Statements | Where-Object {
    $_ -is [Management.Automation.Language.TryStatementAst] -and $_.Extent.Text -like "*Invoke-StageNative 'git'*"
})
if ($archive.Count -ne 1) { throw 'archive invocation not found' }
$RootDir='C:\Users\A B\rd'
$tgzPath=Join-Path $env:WORK 'rangerdanger.tgz'
$OutDir=$env:WORK; $TarExe='tar-fixture'
$verPath=Join-Path $env:WORK '.version'
Set-Content -LiteralPath $verPath -Value 'v2'
$script:extract=''
function Die($Message) { throw $Message }
function Invoke-NativeBounded($Executable, $ArgList, $TimeoutSec) {
    if ($Executable -eq 'git') {
        if ($TimeoutSec -ne 600 -or $ArgList[1] -cne $RootDir -or $ArgList[6] -cne $tgzPath) { throw 'archive args/budget changed' }
        [IO.File]::WriteAllText($tgzPath, 'fixture partial archive')
    } elseif ($Executable -eq 'tar-fixture') {
        if ($TimeoutSec -ne 1800 -or $ArgList[-1] -ne 'manifest.json') { throw 'tar args/budget changed' }
        $script:extract=$ArgList[3]
        [IO.File]::WriteAllText((Join-Path $script:extract 'manifest.json'), '[{"RepoTags":["fixture:v2"]}]')
    } else { throw 'unexpected native call' }
    if ($env:MODE -eq 'missing') { return $null }
    [pscustomobject]@{ExitCode=([int]($env:MODE -eq 'error')); TimedOut=($env:MODE -eq 'timeout'); StdOut=''; StdErr='fixture'}
}
$failed=$false
try { Invoke-Expression $archive[0].Extent.Text } catch { $failed=$true }
if ($failed -ne ($env:MODE -ne 'ok') -or (Test-Path $tgzPath) -ne ($env:MODE -eq 'ok')) { throw 'archive outcome/partial cleanup changed' }
if ($env:STAGER -eq 'stage-ssd.ps1') {
    $f=$ast.FindAll({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Verify-Archive'}, $true)[0]
    Invoke-Expression $f.Extent.Text
    $failed=$false
    try { Verify-Archive 'amd64' @('fixture:v2') } catch { $failed=$true }
    if ($failed -ne ($env:MODE -ne 'ok')) { throw 'tar verification outcome changed' }
    if (-not $script:extract -or (Test-Path $script:extract)) { throw 'tar verification temp directory leaked' }
}
""", STAGER=stager, MODE=mode, WORK=tmp)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_http_budgets_kernel_checksum_retries_and_failed_download_cleanup(self):
        for stager in ("stage-ssd.ps1", "stage-ssd-delta.ps1"):
            for mode in ("ok", "head-timeout", "kernel-timeout", "checksum-timeout"):
                with self.subTest(stager=stager, mode=mode), tempfile.TemporaryDirectory() as tmp:
                    result = powershell(LOAD + r"""
$OutDir=$env:WORK; $Version='v2'; $New='v2'
$kernelUrl='https://fixture/kernel'; $kernelShaUrl="$kernelUrl.sha256"
$kernelPath=Join-Path $OutDir 'kernel'; $shaPath="$kernelPath.sha256"
$kernelReadmeRow=''; $ProgressPreference='Continue'
$script:calls=@()
function Say($Message) {}
function Warn($Message) {}
function Die($Message) { throw $Message }
function Start-Sleep($Seconds) { if ($Seconds -ne 2) { throw 'retry delay changed' } }
function Invoke-WebRequest($Uri, $Method, $OutFile, [switch]$UseBasicParsing, $TimeoutSec, $ErrorAction) {
    $kind=if ($Method -eq 'Head') {'head'} elseif ($Uri -eq $kernelUrl) {'kernel'} else {'checksum'}
    $script:calls += $kind
    $budget=switch ($kind) {'head' {10} 'kernel' {600} 'checksum' {120}}
    if ($TimeoutSec -ne $budget -or -not $UseBasicParsing -or $ErrorAction -ne 'Stop') { throw 'HTTP budget/parsing changed' }
    if ($kind -eq 'head') {
        if ($env:MODE -eq 'head-timeout') { throw 'fixture timeout' }
        return [pscustomobject]@{StatusCode=200}
    }
    $body=if ($kind -eq 'kernel') {'kernel-fixture'} else {(Get-FileHash -LiteralPath $kernelPath -Algorithm SHA256).Hash}
    [IO.File]::WriteAllText($OutFile, $body)
    if ($env:MODE -eq "$kind-timeout") { throw 'fixture timeout' }
}
$source=[IO.File]::ReadAllText("$env:REPO/$env:STAGER")
$start=$source.IndexOf('$kernelPublished =')
$end=$source.IndexOf('Banner "Write ')
$failed=$false
try { Invoke-Expression $source.Substring($start, $end-$start) } catch { $failed=$true }
if ($failed -ne ($env:MODE -in @('kernel-timeout', 'checksum-timeout') -or ($env:STAGER -eq 'stage-ssd.ps1' -and $env:MODE -eq 'head-timeout'))) { throw 'HTTP failure outcome changed' }
if ($ProgressPreference -ne 'Continue') { throw 'progress preference leaked' }
$files=@(Get-ChildItem -LiteralPath $OutDir -File)
if ($files.Count -ne $(if ($env:MODE -eq 'ok') {2} else {0})) { throw 'unverified kernel/checksum left behind' }
$expected=switch ($env:MODE) {
    'head-timeout' {'head'} 'kernel-timeout' {'head,kernel'}
    'checksum-timeout' {'head,kernel,checksum,checksum'} default {'head,kernel,checksum'}
}
if (($script:calls -join ',') -ne $expected) { throw 'retry/request sequence changed' }
""", STAGER=stager, MODE=mode, WORK=tmp)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_partial_cleanup_handles_absence_and_does_not_ignore_removal_failure(self):
        result = powershell(LOAD + r"""
foreach ($mode in 'missing', 'ok', 'error') {
    $script:removed=$false
    function Test-Path { return $mode -ne 'missing' }
    function Remove-Item {
        param($LiteralPath, [switch]$Force, $ErrorAction)
        if ($ErrorAction -ne 'Stop' -or -not $Force) { throw 'cleanup failure could be hidden' }
        $script:removed=$true
        if ($mode -eq 'error') { throw 'fixture locked file' }
    }
    $failed=$false
    try { Remove-StagePartial 'partial.tar' } catch { $failed=$true }
    if ($failed -ne ($mode -eq 'error') -or $script:removed -ne ($mode -ne 'missing')) { throw 'incorrect cleanup outcome' }
}
""")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
