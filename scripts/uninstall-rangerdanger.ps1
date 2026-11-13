<#
.SYNOPSIS
Fully uninstall RangerDanger from this Windows machine.

.DESCRIPTION
The "I'm done with the workshop" button. Brings the stack down, removes
its persistent state, optionally removes the lab images, and reverts
the custom WSL2 kernel that setup.ps1 installed for ICS DPI labs.

By default, this script:
  - Stops the range, then the platform (scripts\dev-down.ps1: label-only,
    by Compose project rangerdanger / rangerdanger-platform), removing
    their containers and Docker networks. The range teardown also
    removes its anonymous volumes (the webtops' /config); the lab has no
    named volumes, and its state lives in bind-mounted directories
    (backend\data, data\), which this script never removes.
  - Removes the .env file setup.ps1 wrote.
  - Reverts the custom WSL2 kernel (restores .wslconfig.bak or removes
    our kernel= line) and runs `wsl --shutdown` so the change takes
    effect.

It does NOT remove any Docker images by default, because they are
large (~6 GB total) and you may want to keep them for a later run.
Images fall into three categories:
  A. release -- pulled ghcr.io/tonylturner/rangerdanger-*, containd
  B. dev     -- locally-built rangerdanger-platform-<service> and
                rangerdanger-<service> images, plus the pre-split
                rangerdanger-backend / rangerdanger-frontend
  C. base    -- shared public images (alpine, nginx, fuxa, webtop)
                that OTHER projects on this host may also use
Pass -RemoveImages (A), -RemoveDevImages (B), and/or -RemoveBaseImages
(C). -Purge is shorthand for -RemoveImages plus -RemoveDevImages.

.PARAMETER Yes
Skip the confirmation prompt before tearing down.

.PARAMETER RemoveImages
Also remove exact release references for the platform and installed packages
(category A). Without this flag the
images are kept so a future setup.ps1 run reuses them.

.PARAMETER RemoveDevImages
Also remove the locally-built images (category B) that a source build
produces from docker-compose.yml and the installed packages' compose.source.yml.

.PARAMETER RemoveBaseImages
Also remove the shared third-party base images (category C: alpine,
nginx, fuxa, webtop). Never forced -- any image still in use by a
running container is kept. These are left alone by default because
other projects on this machine may depend on them.

.PARAMETER Purge
Shorthand for -RemoveImages -RemoveDevImages: a clean slate for
redeploy testing. Base images are left alone; add -RemoveBaseImages
to remove those too.

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
  3  = teardown left containers, networks or range volumes behind;
       nothing else removed
#>

[CmdletBinding()]
param(
    [switch]$Yes,
    [switch]$RemoveImages,
    [switch]$RemoveDevImages,
    [switch]$RemoveBaseImages,
    [switch]$Purge,
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
. "$RootDir/scripts/lib/windows-root.ps1"
try { $DaemonRoot = ConvertTo-DesktopRoot $RootDir }
catch { Warn $_; exit 3 }
$EnvFile       = Join-Path $RootDir ".env"
$KernelScript  = Join-Path $RootDir "scripts\install-wsl-kernel.ps1"
$DevDown       = Join-Path $RootDir "scripts\dev-down.ps1"
# What this script owns: the containers and networks of these two Compose
# projects, by label. Never anything matched by name.
$Projects      = @('rangerdanger', 'rangerdanger-platform')

Banner "RangerDanger uninstall"

# --- inventory ---------------------------------------------------------
$rdContainers = @()
$rdNetworks = @()
foreach ($p in $Projects) {
    $label = "label=com.docker.compose.project=$p"
    $rdContainers   += @(& { $ErrorActionPreference = 'SilentlyContinue'; & docker ps -a --format "{{.Names}}" --filter $label 2>$null } | Where-Object { $_ })
    $rdNetworks     += @(& { $ErrorActionPreference = 'SilentlyContinue'; & docker network ls -q --filter $label 2>$null } | Where-Object { $_ })
}
Say "Containers found: $($rdContainers.Count)"
$rdContainers | ForEach-Object { Write-Host "  $_" }
Say "Networks found: $($rdNetworks.Count)"

$presentImages = & {
    $ErrorActionPreference = 'SilentlyContinue'
    & docker images --format "{{.Repository}}:{{.Tag}}" 2>$null
}
$presentImages = @($presentImages | Where-Object { $_ })

. "$RootDir/scripts/lib/installed-packages.ps1"
. "$RootDir/scripts/lib/docker-bounded.ps1"
. "$RootDir/scripts/lib/setup-package.ps1"
$selected = @()
try {
    if (Test-Path -LiteralPath (Join-Path $RootDir 'data/installed-packages')) { $selected = @(Read-InstalledPackages $RootDir) }
    elseif ($RemoveImages -or $RemoveDevImages -or $RemoveBaseImages) { throw "Missing $RootDir/data/installed-packages; run setup before removing images." }
} catch { Warn $_; exit 3 }
$rdImages = @(); $rdDevImages = @(); $rdBaseImages = @()
$savedRoot = $env:RANGERDANGER_ROOT
$savedVersion = $env:VERSION
$env:RANGERDANGER_ROOT = $DaemonRoot
try {
    if ($selected.Count) {
        $env:VERSION = Get-InstalledEnvValue $RootDir VERSION
        $releaseRefs = @(Get-SetupImages @(Get-SetupModels $RootDir 'release' $selected))
        $sourceRefs = @(Get-SetupImages @(Get-SetupModels $RootDir 'source' $selected))
        $rdImages = @($releaseRefs | Where-Object { $_ -clike 'ghcr.io/tonylturner/*' -and $_ -cin $presentImages })
        $devRefs = @($sourceRefs | Where-Object { $_ -notmatch ':' } | ForEach-Object { "${_}:latest" })
        $rdDevImages = @($devRefs | Where-Object { $_ -cin $presentImages })
        $rdBaseImages = @($sourceRefs | Where-Object {
            $_ -match ':' -and $_ -cnotlike 'ghcr.io/tonylturner/*' -and $_ -cnotlike 'rangerdanger-*' -and $_ -cin $presentImages
        })
    }
} catch { Warn "Could not resolve installed image closure: $_. Nothing was removed."; exit 3 }
finally { $env:RANGERDANGER_ROOT = $savedRoot; $env:VERSION = $savedVersion }

Say "Release images (ghcr):    $($rdImages.Count)"
Say "Dev images (local build): $($rdDevImages.Count)"
Say "Base images (shared):     $($rdBaseImages.Count)"

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
if ($rdContainers.Count -eq 0 -and $rdNetworks.Count -eq 0 -and $rdImages.Count -eq 0 -and $rdDevImages.Count -eq 0 -and -not $kernelInstalled -and -not $envFileFound -and -not $selected.Count) {
    Banner "Nothing to uninstall"
    Write-Host "  No RangerDanger state detected on this machine."
    exit 2
}

# --- confirm -----------------------------------------------------------
Banner "About to:"
Write-Host "  - Stop the range (containers, networks, anonymous volumes), then the"
Write-Host "    platform (containers, networks), by Compose project"
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
if ($selected.Count) { Write-Host '  - Remove the installation receipt and saved mode' }
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

# --- 1. range, then platform -------------------------------------------
# Fail closed: if anything of either project survives, stop here and
# remove nothing else.
Banner "Stopping the range and the platform"
& $DevDown 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -ne 0) {
    Warn "Teardown left resources behind (see above). Nothing else was removed."
    Warn "Fix the cause, then re-run this script."
    exit 3
}
Say "Range and platform removed"

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

foreach ($name in 'installed-packages', 'install-mode') {
    Remove-Item -LiteralPath (Join-Path $RootDir "data/$name") -Force -ErrorAction SilentlyContinue
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
Write-Host "  Containers + networks: removed (and the range's anonymous volumes)"
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
}
Write-Host ""
Write-Host "To reinstall later: .\setup.ps1"
exit 0
