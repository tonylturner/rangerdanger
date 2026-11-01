<#
.SYNOPSIS
Reviewed lab-command semantic smoke on a disposable range. Never use a student stack.
.DESCRIPTION
Uses the same Python inventory, positive controls, semantic/state oracles and
finally/Weak cleanup as the shell entry point. No embedded parser or exit-code
success heuristics. Python 3 + PyYAML are required. EU additionally uses the shared
EU lifecycle helpers: Bash, curl, jq and python3 must be on that Bash PATH.
Real Windows/Docker acceptance is separate from the offline semantic tests.
#>
[CmdletBinding()]
param(
    [string]$Package = 'us-dnp3-substation',
    [string]$Scenario = '',
    [int]$ProbeTimeoutSec = $(if ($env:LAB_CMD_TIMEOUT) { [int]$env:LAB_CMD_TIMEOUT } else { 10 })
)
$ErrorActionPreference = 'Stop'

function Resolve-Python3 {
    if (Get-Command py -ErrorAction SilentlyContinue) {
        & py -3 -c "import yaml" 2>$null
        if ($LASTEXITCODE -eq 0) { return ,@('py', '-3') }
    }
    foreach ($name in 'python3', 'python') {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if (-not $cmd -or $cmd.Source -like '*\WindowsApps\*') { continue }
        & $cmd.Source -c "import yaml" 2>$null
        if ($LASTEXITCODE -eq 0) { return ,@($cmd.Source) }
    }
    throw 'Python 3 with PyYAML is required. Install with: py -3 -m pip install pyyaml'
}

function Invoke-LabCommandWorker([string[]]$WorkerArgs) {
    # Keep both streams visible; only the shared worker decides semantic success.
    & $PythonExe @PythonPrefix @WorkerArgs | Out-Host
    return $LASTEXITCODE
}

try {
    $Python = Resolve-Python3
    $PythonExe = $Python[0]
    $PythonPrefix = @()
    if ($Python.Count -gt 1) { $PythonPrefix = @($Python[1..($Python.Count - 1)]) }
    $root = Split-Path -Parent $PSScriptRoot
    $worker = Join-Path $PSScriptRoot 'lib/lab_commands_smoke.py'
    # Resolve evidence against the checkout, without changing the caller's cwd.
    $output = if ($env:LAB_CMD_LOG_DIR) { $env:LAB_CMD_LOG_DIR } else { Join-Path $root 'build/lab-commands' }
    $workerArgs = @($worker, '--package', $Package, '--timeout', "$ProbeTimeoutSec", '--output', $output)
    if ($Scenario) { $workerArgs += $Scenario }
    $code = Invoke-LabCommandWorker $workerArgs
    exit $code
} catch {
    Write-Error $_
    exit 1
}
