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

# RD_STAGE_PULL_ATTEMPTS and RD_STAGE_PULL_DELAY_S override retry limits for tests.
function Invoke-StagePull([string]$Image, [string]$Ref, [int]$TimeoutSec = 3600, [switch]$StreamOutput) {
    $attempts = 3
    if ($env:RD_STAGE_PULL_ATTEMPTS -match '^[1-3]$') {
        $attempts = [int]$env:RD_STAGE_PULL_ATTEMPTS
    }
    $delayOverride = $null
    if ($env:RD_STAGE_PULL_DELAY_S -match '^[0-9]$') {
        $delayOverride = [int]$env:RD_STAGE_PULL_DELAY_S
    }

    if ($Image -match 'rangerdanger-(corp-ws|eng-ws|vendor-jump)') {
        Write-Host "[+]     large image (~2-3 GB desktop) -- a few minutes is normal" -ForegroundColor Green
    } elseif ($Image -match 'rangerdanger-openplc') {
        Write-Host "[+]     large image (~1 GB) -- give it a minute" -ForegroundColor Green
    }

    $operation = "docker pull $Ref"
    for ($attempt = 1; $attempt -le $attempts; $attempt++) {
        $run = Invoke-DockerBounded @('pull', $Ref) $TimeoutSec -StreamOutput:$StreamOutput
        if (-not $run -or $run.TimedOut) {
            Get-StageRunOutput $run $operation $TimeoutSec
        }
        if ($run.ExitCode -eq 0) { return $run.StdOut }
        if ($attempt -eq $attempts) {
            Get-StageRunOutput $run $operation $TimeoutSec
        }
        if ($null -ne $delayOverride) {
            $delay = $delayOverride
        } elseif ($attempt -eq 1) {
            $delay = 5
        } else {
            $delay = 15
        }
        Write-Host "[!] docker pull attempt $attempt/$attempts failed for $Ref; retrying in ${delay}s after registry/pull error" -ForegroundColor Yellow
        Start-Sleep -Seconds $delay
    }
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
