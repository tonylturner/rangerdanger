<#
.SYNOPSIS
Fully uninstall RangerDanger from this Windows machine.

.DESCRIPTION
The "I'm done with the workshop" button. Brings the stack down, removes
its persistent state, optionally removes the lab images, and reverts
the custom WSL2 kernel that setup.ps1 installed for ICS DPI labs.

By default, this script:
  - Stops + removes all RangerDanger containers and Docker networks.
  - Removes the named volumes the lab creates (delete labs DB,
    captures, simulator persistent state).
  - Removes the .env file setup.ps1 wrote.
  - Reverts the custom WSL2 kernel (restores .wslconfig.bak or removes
    our kernel= line) and runs `wsl --shutdown` so the change takes
    effect.

It does NOT remove the Docker images by default, because they are
large (~6 GB total) and you may want to keep them for a later run.
Pass -RemoveImages to delete them as well.

.PARAMETER Yes
Skip the confirmation prompt before tearing down.

.PARAMETER RemoveImages
Also remove every ghcr.io/tonylturner/rangerdanger-* image. Frees the
6 GB-ish disk those images occupy. Without this flag the images are
kept so a future setup.ps1 run reuses them.

.PARAMETER KeepVolumes
Do NOT remove docker volumes (lab DB, captures, sim state) on
teardown. Useful if you want to preserve student progress for a later
session.

.PARAMETER KeepKernel
Do NOT revert the custom WSL2 kernel. Useful if you have other
RangerDanger labs installed that also rely on the kernel, or if you
manage WSL2's kernel for other reasons.

.EXAMPLE
.\scripts\uninstall-rangerdanger.ps1
.\scripts\uninstall-rangerdanger.ps1 -Yes -RemoveImages

.NOTES
This script is the post-workshop cleanup path. It does NOT touch
anything outside the rangerdanger surface area: it never removes
non-rangerdanger containers, non-rangerdanger images, or any
.wslconfig entries we did not write ourselves.

ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.

Exit codes:
  0  = uninstall completed (or partial completion with warnings)
  1  = user declined the confirmation prompt
  2  = no rangerdanger state detected (nothing to do)
#>

[CmdletBinding()]
param(
    [switch]$Yes,
    [switch]$RemoveImages,
    [switch]$KeepVolumes,
    [switch]$KeepKernel
)

$ErrorActionPreference = "Continue"

function Say($m)    { Write-Host "[+] $m" -ForegroundColor Green }
function Warn($m)   { Write-Host "[!] $m" -ForegroundColor Yellow }
function Banner($m) {
    Write-Host ""
    Write-Host $m -ForegroundColor Cyan
    Write-Host ('-' * $m.Length) -ForegroundColor Cyan
    Write-Host ""
}

$RootDir       = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$ComposeFile   = Join-Path $RootDir "docker-compose.release.yml"
$OfflineFile   = Join-Path $RootDir "docker-compose.offline.yml"
$EnvFile       = Join-Path $RootDir ".env"
$KernelScript  = Join-Path $RootDir "scripts\install-wsl-kernel.ps1"

# Build the compose args used for "down" so we hit the same file set
# setup.ps1 brought the stack up with.
$ComposeArgs = @("-f", $ComposeFile)
if (Test-Path $OfflineFile) {
    # Offline overlay is harmless to add even online -- only affects
    # pull policy, which `down` doesn't use.
    $ComposeArgs += @("-f", $OfflineFile)
}

Banner "RangerDanger uninstall"

# --- inventory ---------------------------------------------------------
$containers = & {
    $ErrorActionPreference = 'SilentlyContinue'
    & docker ps -a --format "{{.Names}}" --filter "name=rangerdanger-" 2>$null
}
$rdContainers = @($containers | Where-Object { $_ })
Say "Containers found: $($rdContainers.Count)"
$rdContainers | ForEach-Object { Write-Host "  $_" }

$images = & {
    $ErrorActionPreference = 'SilentlyContinue'
    & docker images --format "{{.Repository}}:{{.Tag}}" 2>$null
} | Where-Object { $_ -like "ghcr.io/tonylturner/rangerdanger-*" -or $_ -like "ghcr.io/tonylturner/containd*" }
$rdImages = @($images)
Say "Lab images found: $($rdImages.Count)"
if ($RemoveImages) { $rdImages | ForEach-Object { Write-Host "  $_" } }

$volumes = & {
    $ErrorActionPreference = 'SilentlyContinue'
    & docker volume ls --format "{{.Name}}" 2>$null
} | Where-Object { $_ -like "rangerdanger*" }
$rdVolumes = @($volumes)
Say "Volumes: $($rdVolumes.Count)"

$kernelInstalled = $false
$wslConfig = Join-Path $env:USERPROFILE ".wslconfig"
if (Test-Path $wslConfig) {
    $kernelInstalled = (Get-Content $wslConfig -Raw) -match 'rangerdanger.wsl-kernel.rangerdanger-wsl2-kernel'
}
if ($kernelInstalled) {
    Say "Custom WSL2 kernel: installed (managed by us)"
} else {
    Say "Custom WSL2 kernel: not installed"
}

$envFileFound = Test-Path $EnvFile
if ($envFileFound) { Say ".env file found at $EnvFile" }

# Nothing to do at all?
if ($rdContainers.Count -eq 0 -and $rdImages.Count -eq 0 -and -not $kernelInstalled -and -not $envFileFound) {
    Banner "Nothing to uninstall"
    Write-Host "  No RangerDanger state detected on this machine."
    exit 2
}

# --- confirm -----------------------------------------------------------
Banner "About to:"
Write-Host "  1. docker compose down (containers + networks)"
if (-not $KeepVolumes) { Write-Host "     -- with -v (removes lab DB, captures, sim state)" }
else                    { Write-Host "     -- volumes kept (-KeepVolumes)" }
if ($RemoveImages -and $rdImages.Count -gt 0) {
    Write-Host "  2. Remove $($rdImages.Count) Docker image(s) (~6 GB)"
}
if ($envFileFound) {
    Write-Host "  3. Remove $EnvFile"
}
if ($kernelInstalled -and -not $KeepKernel) {
    Write-Host "  4. Revert the custom WSL2 kernel (runs wsl --shutdown)"
} elseif ($kernelInstalled -and $KeepKernel) {
    Write-Host "  4. Keep the custom WSL2 kernel (-KeepKernel)"
}
Write-Host ""

if (-not $Yes) {
    $ans = Read-Host "Continue? [y/N]"
    if ($ans -notmatch '^(y|yes)$') {
        Write-Host "[+] Aborted by user. No changes made." -ForegroundColor Green
        exit 1
    }
}

# --- 1. compose down ---------------------------------------------------
Banner "Stopping the stack"
$downArgs = @("compose") + $ComposeArgs + @("down")
if (-not $KeepVolumes) { $downArgs += "-v" }
& docker @downArgs 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -eq 0) { Say "compose down complete" }
else                      { Warn "compose down exited $LASTEXITCODE (containers may already be gone)" }

# --- 2. remove images (optional) --------------------------------------
if ($RemoveImages -and $rdImages.Count -gt 0) {
    Banner "Removing lab images"
    foreach ($img in $rdImages) {
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker image rm $img *>$null
        }
        if ($LASTEXITCODE -eq 0) { Say "removed $img" }
        else                      { Warn "could not remove $img (in use, missing, or already gone)" }
    }
}

# --- 3. .env -----------------------------------------------------------
if ($envFileFound) {
    Banner "Removing setup-written .env"
    Remove-Item $EnvFile -Force -ErrorAction SilentlyContinue
    if (-not (Test-Path $EnvFile)) { Say "Removed $EnvFile" }
    else                            { Warn "Could not remove $EnvFile" }
}

# --- 4. kernel revert --------------------------------------------------
if ($kernelInstalled -and -not $KeepKernel) {
    Banner "Reverting the custom WSL2 kernel"
    if (-not (Test-Path $KernelScript)) {
        Warn "scripts\install-wsl-kernel.ps1 not found -- cannot revert kernel automatically."
        Warn "Edit $wslConfig and remove the line starting with 'kernel='."
        Warn "Then run: wsl --shutdown"
    } else {
        & $KernelScript -Restore
    }
}

# --- done --------------------------------------------------------------
Banner "RangerDanger removed"
Write-Host "  Containers + networks: stopped"
if (-not $KeepVolumes) { Write-Host "  Volumes:               removed" }
else                    { Write-Host "  Volumes:               kept (-KeepVolumes)" }
if ($RemoveImages)     { Write-Host "  Images:                removed (~6 GB freed)" }
else                    { Write-Host "  Images:                kept (pass -RemoveImages to free disk)" }
if ($kernelInstalled -and -not $KeepKernel) {
    Write-Host "  WSL2 kernel:           stock Microsoft kernel restored"
}
Write-Host ""
Write-Host "To reinstall later: .\setup.ps1"
exit 0
