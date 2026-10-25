# Shared staging failure handling. Dot-source after docker-bounded.ps1.
# Call-site budgets are seconds:
#   config 30; manifest 120; pull 3600; tag 60; save 7200;
#   git archive 600; git metadata 30; tar verification 1800.
# Registry metadata is small; pulls allow a 3 GB image over a slow link.
# Saves allow two hours for full multi-GB bundles on slow USB/antivirus.
# Archive creation allows compression overhead; tar may scan the whole SSD
# archive before finding manifest.json. Metadata/tag calls are local and small.
# HTTP: HEAD 10; release JSON/checksum 120; ~25 MB kernel download 600.
# ASCII-only, BOM-free; Windows PowerShell 5.1 compatible.

function Get-StageRunOutput($Run, [string]$Operation, [int]$TimeoutSec) {
    if (-not $Run) { throw "$Operation failed: executable not found" }
    if ($Run.TimedOut) { throw "$Operation timed out after ${TimeoutSec}s" }
    if ($Run.ExitCode -ne 0) { throw "$Operation failed (exit $($Run.ExitCode)): $($Run.StdErr)" }
    return $Run.StdOut
}

function Invoke-StageDocker([string[]]$DockerArgs, [int]$TimeoutSec, [switch]$StreamOutput) {
    $run = Invoke-DockerBounded $DockerArgs $TimeoutSec -StreamOutput:$StreamOutput
    Get-StageRunOutput $run ("docker " + ($DockerArgs -join ' ')) $TimeoutSec
}

function Invoke-StageNative([string]$Executable, [string[]]$ArgList, [int]$TimeoutSec) {
    $run = Invoke-NativeBounded $Executable $ArgList $TimeoutSec
    Get-StageRunOutput $run ($Executable + ' ' + ($ArgList -join ' ')) $TimeoutSec
}

function Remove-StagePartial([string]$Path) {
    # Never hide a locked/undeletable partial archive behind the original
    # transfer error. Missing files are normal when a CLI could not launch.
    if (Test-Path -LiteralPath $Path) { Remove-Item -LiteralPath $Path -Force -ErrorAction Stop }
}
