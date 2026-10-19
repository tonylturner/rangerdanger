#Requires -Version 5.1
<#
.SYNOPSIS
Apply an already-checked-out repo-only hotfix to an installed release range.
.PARAMETER FromTarballs
SSD directory, as in setup.ps1. Selects the offline Compose overlay only.
Setup persists no install-mode marker, so SSD users must pass this parameter.
#>
[CmdletBinding()]
param([string]$FromTarballs = "")

$ErrorActionPreference = "Stop"
function Die([string]$Message) {
    Write-Host "Repo update refused: $Message" -ForegroundColor Red
    exit 1
}

$RootDir = Split-Path -Parent $PSScriptRoot
$ComposeArgs = @("--project-directory", $RootDir, "-f", (Join-Path $RootDir "docker-compose.release.yml"))
if ($FromTarballs) {
    if (-not (Test-Path -LiteralPath $FromTarballs -PathType Container)) {
        Die "SSD directory not found: $FromTarballs"
    }
    $Overlay = Join-Path $RootDir "docker-compose.offline.yml"
    if (-not (Test-Path -LiteralPath $Overlay)) { Die "Offline overlay is missing." }
    $ComposeArgs += @("-f", $Overlay)
}

# Exclude untracked/ignored files (.env and student evidence/data).
$GitRoot = & git -C $RootDir rev-parse --show-toplevel
if ($LASTEXITCODE -ne 0) { Die "A Git checkout is required." }
if ([IO.Path]::GetFullPath($GitRoot) -ne [IO.Path]::GetFullPath($RootDir)) {
    Die "Run this helper from its own Git checkout."
}
$Edits = @(& git -C $RootDir status --porcelain --untracked-files=no)
if ($LASTEXITCODE -ne 0) { Die "Cannot inspect tracked files." }
if ($Edits.Count -gt 0) {
    Die ("Tracked files have local edits; back them up before updating:`n" + ($Edits -join "`n"))
}

# setup.ps1 checks for Compose, but does not enforce a numeric minimum.
$UpHelp = & docker compose @ComposeArgs up --help
if ($LASTEXITCODE -ne 0) { Die "Cannot read Compose up options." }
if (($UpHelp -join "`n") -notmatch '--pull\s') {
    Die "Compose lacks up --pull never. Update Docker Compose before applying this hotfix."
}

$Images = @(& docker compose @ComposeArgs config --images)
if ($LASTEXITCODE -ne 0 -or $Images.Count -eq 0) { Die "Cannot resolve release Compose images." }
$Missing = $false
foreach ($Image in $Images) {
    if (-not $Image.Trim()) { continue }
    # Suppress native stderr without PowerShell 5.1 turning it into an exception.
    $PreviousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        & docker image inspect $Image *> $null
        $InspectExit = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $PreviousPreference
    }
    if ($InspectExit -ne 0) {
        Write-Host "Required image is not available locally: $Image" -ForegroundColor Red
        $Missing = $true
    }
}
if ($Missing) {
    Die "Restore the installed v0.1.35 images from your SSD/instructor and check Docker is running. No images were downloaded; no services changed."
}

& docker compose @ComposeArgs config -q
if ($LASTEXITCODE -ne 0) { Die "Compose configuration is invalid." }
& docker compose @ComposeArgs restart backend proxy
if ($LASTEXITCODE -ne 0) { Die "Backend/proxy restart failed; update is incomplete." }
& docker compose @ComposeArgs up -d --no-deps --no-build --pull never --force-recreate vendor_jump
if ($LASTEXITCODE -ne 0) { Die "Vendor recreate failed; update is incomplete." }

Write-Host "Repo update applied; .env and data kept. Open http://localhost:8088 (vendor desktop: http://localhost:8088/apps/vendor-jump/). Allow about 20 seconds for the desktop."
