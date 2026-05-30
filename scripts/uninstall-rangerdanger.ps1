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

It does NOT remove any Docker images by default, because they are
large (~6 GB total) and you may want to keep them for a later run.
Images fall into three categories:
  A. release -- pulled ghcr.io/tonylturner/rangerdanger-*, containd
  B. dev     -- locally-built rangerdanger-<service>:latest images
  C. base    -- shared public images (alpine, nginx, fuxa, webtop)
                that OTHER projects on this host may also use
Pass -RemoveImages (A), -RemoveDevImages (B), and/or -RemoveBaseImages
(C). -Purge is shorthand for -RemoveImages plus -RemoveDevImages.

.PARAMETER Yes
Skip the confirmation prompt before tearing down.

.PARAMETER RemoveImages
Also remove every ghcr.io/tonylturner/rangerdanger-* image (category
A). Frees the 6 GB-ish disk those images occupy. Without this flag the
images are kept so a future setup.ps1 run reuses them.

.PARAMETER RemoveDevImages
Also remove the locally-built rangerdanger-<service>:latest images
(category B) produced by `docker compose build` against the dev
docker-compose.yml.

.PARAMETER RemoveBaseImages
Also remove the shared third-party base images (category C: alpine,
nginx, fuxa, webtop). Never forced -- any image still in use by a
running container is kept. These are left alone by default because
other projects on this machine may depend on them.

.PARAMETER Purge
Shorthand for -RemoveImages -RemoveDevImages: a clean slate for
redeploy testing. Base images are left alone; add -RemoveBaseImages
to remove those too.

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
.\scripts\uninstall-rangerdanger.ps1 -Yes -Purge

.NOTES
This script is the post-workshop cleanup path. By default it touches
only the rangerdanger surface area: it never removes non-rangerdanger
containers, never removes shared base images, and never touches any
.wslconfig entries we did not write ourselves. The opt-in
-RemoveBaseImages flag is the one exception -- it removes the shared
base images the lab uses, but only those not currently in use by
another container.

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
    [switch]$RemoveDevImages,
    [switch]$RemoveBaseImages,
    [switch]$Purge,
    [switch]$KeepVolumes,
    [switch]$KeepKernel
)

$ErrorActionPreference = "Continue"

# -Purge is a convenience for a clean-slate redeploy test: release + dev
# images. Base images are intentionally left out (shared with other
# projects); use -RemoveBaseImages for those.
if ($Purge) {
    $RemoveImages    = $true
    $RemoveDevImages = $true
}

function Say($m)    { Write-Host "[+] $m" -ForegroundColor Green }
function Warn($m)   { Write-Host "[!] $m" -ForegroundColor Yellow }
function Banner($m) {
    Write-Host ""
    Write-Host $m -ForegroundColor Cyan
    Write-Host ('-' * $m.Length) -ForegroundColor Cyan
    Write-Host ""
}

function Remove-ImageList($list) {
    # Remove each repo:tag in $list. Never forces: an image still in use by
    # another container is kept, not deleted.
    foreach ($img in $list) {
        if (-not $img) { continue }
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker image rm $img *>$null
        }
        if ($LASTEXITCODE -eq 0) { Say "removed $img" }
        else                      { Warn "kept $img (in use by another container, or already gone)" }
    }
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

$presentImages = & {
    $ErrorActionPreference = 'SilentlyContinue'
    & docker images --format "{{.Repository}}:{{.Tag}}" 2>$null
}
$presentImages = @($presentImages | Where-Object { $_ })

# Category A -- pulled release images (ghcr.io/tonylturner/rangerdanger-*, containd).
$rdImages = @($presentImages | Where-Object {
    $_ -like "ghcr.io/tonylturner/rangerdanger-*" -or $_ -like "ghcr.io/tonylturner/containd*"
})

# Categories B + C are derived from the dev compose file as the single
# source of truth. `docker compose config --images` prints locally-built
# images WITHOUT a tag (e.g. "rangerdanger-backend") and pulled images
# WITH one (e.g. "nginx:1.27-alpine"), which lets us split them apart
# without hard-coding service lists.
$DevCompose = Join-Path $RootDir "docker-compose.yml"
$composeImages = @()
if (Test-Path $DevCompose) {
    $composeImages = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker compose -f $DevCompose config --images 2>$null
    }
    $composeImages = @($composeImages | Where-Object { $_ })
}

# Category B -- locally-built dev images (rangerdanger-<service>:latest).
$devRepos = @($composeImages | Where-Object { $_ -notmatch ':' })
if ($devRepos.Count -eq 0) {
    $devRepos = @($presentImages | ForEach-Object { ($_ -split ':')[0] } |
        Where-Object { $_ -match '^rangerdanger-[a-z]' -and $_ -notmatch '/' } | Sort-Object -Unique)
}
$rdDevImages = @()
foreach ($repo in $devRepos) {
    $rdDevImages += @($presentImages | Where-Object { $_ -like "${repo}:*" })
}
$rdDevImages = @($rdDevImages | Sort-Object -Unique)

# Category C -- shared third-party base images (tagged, non-ghcr, non-built).
$baseRefs = @($composeImages |
    Where-Object { $_ -match ':' -and $_ -notlike "ghcr.io/tonylturner/*" -and $_ -notmatch '^rangerdanger-' } |
    ForEach-Object { $_ -replace '@sha256:[0-9a-f]+', '' } | Sort-Object -Unique)
if ($baseRefs.Count -eq 0) {
    $baseRefs = @('alpine:3.21', 'nginx:1.27-alpine', 'frangoteam/fuxa:latest', 'linuxserver/webtop:ubuntu-mate')
}
$rdBaseImages = @($baseRefs | Where-Object { $presentImages -contains $_ } | Sort-Object -Unique)

Say "Release images (ghcr):    $($rdImages.Count)"
Say "Dev images (local build): $($rdDevImages.Count)"
Say "Base images (shared):     $($rdBaseImages.Count)"

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
if ($rdContainers.Count -eq 0 -and $rdImages.Count -eq 0 -and $rdDevImages.Count -eq 0 -and -not $kernelInstalled -and -not $envFileFound) {
    Banner "Nothing to uninstall"
    Write-Host "  No RangerDanger state detected on this machine."
    exit 2
}

# --- confirm -----------------------------------------------------------
Banner "About to:"
Write-Host "  - docker compose down (containers + networks)"
if (-not $KeepVolumes) { Write-Host "      with -v (removes lab DB, captures, sim state)" }
else                    { Write-Host "      volumes kept (-KeepVolumes)" }
if ($RemoveImages -and $rdImages.Count -gt 0) {
    Write-Host "  - Remove $($rdImages.Count) release image(s) (~6 GB)"
}
if ($RemoveDevImages -and $rdDevImages.Count -gt 0) {
    Write-Host "  - Remove $($rdDevImages.Count) locally-built dev image(s)"
}
if ($RemoveBaseImages -and $rdBaseImages.Count -gt 0) {
    Write-Host "  - Remove $($rdBaseImages.Count) shared base image(s) -- any in use are skipped"
}
if ($envFileFound) {
    Write-Host "  - Remove $EnvFile"
}
if ($kernelInstalled -and -not $KeepKernel) {
    Write-Host "  - Revert the custom WSL2 kernel (runs wsl --shutdown)"
} elseif ($kernelInstalled -and $KeepKernel) {
    Write-Host "  - Keep the custom WSL2 kernel (-KeepKernel)"
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

# --- 2. remove release images (optional) ------------------------------
if ($RemoveImages -and $rdImages.Count -gt 0) {
    Banner "Removing release images"
    Remove-ImageList $rdImages
}

# --- 2b. remove dev images (optional) ---------------------------------
if ($RemoveDevImages -and $rdDevImages.Count -gt 0) {
    Banner "Removing locally-built dev images"
    Remove-ImageList $rdDevImages
}

# --- 2c. remove base images (optional, shared) ------------------------
if ($RemoveBaseImages -and $rdBaseImages.Count -gt 0) {
    Banner "Removing shared base images"
    Warn "These are public base images other projects may share;"
    Warn "any image still used by a running container is kept."
    Remove-ImageList $rdBaseImages
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
if ($RemoveImages)     { Write-Host "  Release images:        removed (~6 GB freed)" }
else                    { Write-Host "  Release images:        kept (pass -RemoveImages to free disk)" }
if ($RemoveDevImages)  { Write-Host "  Dev images:            removed" }
elseif ($rdDevImages.Count -gt 0) { Write-Host "  Dev images:            kept (pass -RemoveDevImages)" }
if ($RemoveBaseImages) { Write-Host "  Base images:           removed where not in use" }
if ($kernelInstalled -and -not $KeepKernel) {
    Write-Host "  WSL2 kernel:           stock Microsoft kernel restored"
}

# Always show how to reclaim the shared base images by hand, unless we just
# removed them. They are left alone by default because other projects on
# this host may depend on them.
if (-not $RemoveBaseImages -and $rdBaseImages.Count -gt 0) {
    Write-Host ""
    Warn "Shared base images left in place (other projects may use them):"
    Write-Host "    docker image rm $($rdBaseImages -join ' ')"
    Write-Host "  or re-run with -RemoveBaseImages (in-use images are skipped)."
}
Write-Host ""
Write-Host "To reinstall later: .\setup.ps1"
exit 0
