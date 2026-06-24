<#
.SYNOPSIS
RangerDanger SSD/airgap stage helper -- Windows sibling of stage-ssd.sh.

.DESCRIPTION
Pulls every release image for both linux/amd64 and linux/arm64 from
GHCR, saves each architecture into a tarball, and writes the repo
archive alongside. The output directory is what students plug into
.\setup.ps1 -FromTarballs <dir>.

.PARAMETER OutDir
Directory to write the staged bundle into. Created if it does not exist.

.PARAMETER Version
Image tag to stage. Default: latest. Pin to e.g. 'v0.1.16' for a
release.

.EXAMPLE
.\stage-ssd.ps1 D:\WORKSHOP_SSD v0.1.16
.\stage-ssd.ps1 .\out latest

.NOTES
Runtime: ~25-45 min on a fast connection (pulls every image twice,
once per architecture). Cache-warm subsequent runs are much faster.

Mirrors stage-ssd.sh semantics. Uses 'docker buildx imagetools inspect'
to resolve single-platform manifest digests so 'docker save' can bundle
cross-architecture images without falling over the LinuxKit
manifest-list trap (see the bash version's resolve_platform_ref comment
block for the long rationale).

ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true, Position=0)]
    [string]$OutDir,
    [Parameter(Position=1)]
    [string]$Version = "latest"
)

$ErrorActionPreference = "Stop"

function Say($m)    { Write-Host "[+] $m" -ForegroundColor Green }
function Warn($m)   { Write-Host "[!] $m" -ForegroundColor Yellow }
function Die($m)    { Write-Host "[x] $m" -ForegroundColor Red; exit 1 }
function Banner($m) {
    Write-Host ""
    Write-Host $m -ForegroundColor Cyan
    Write-Host ('-' * $m.Length) -ForegroundColor Cyan
    Write-Host ""
}

$RootDir     = Split-Path -Parent $MyInvocation.MyCommand.Path
$ComposeFile = Join-Path $RootDir "docker-compose.release.yml"

if (-not (Test-Path $ComposeFile)) { Die "$ComposeFile not found -- run from repo root." }
if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }
$OutDir = (Resolve-Path $OutDir).Path

Say "Output:  $OutDir"
Say "Version: $Version"

# --- Enumerate images from compose --------------------------------------
$allImages = & docker compose -f $ComposeFile config --images 2>$null | Sort-Object -Unique
if (-not $allImages) { Die "Could not enumerate images from $ComposeFile" }

# Substitute :latest with $Version for first-party rangerdanger-* images.
$resolved = foreach ($img in $allImages) {
    $img -replace '^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):latest$', "`$1:$Version"
}

# --- resolve_platform_ref -----------------------------------------------
# See stage-ssd.sh for the full rationale (long comment block above the
# bash version). TL;DR: pulling --platform=<arch> on a different host arch
# stores a manifest LIST locally; docker save then walks both platforms'
# sub-manifests and errors on the missing one. Pulling by single-platform
# manifest digest stores only that platform's manifest, so save walks
# only one and succeeds.
function Resolve-PlatformRef($img, $arch) {
    $tpl = '{{range .Manifest.Manifests}}{{if and (eq .Platform.OS "linux") (eq .Platform.Architecture "' + $arch + '")}}{{.Digest}}{{end}}{{end}}'
    $digest = & {
        $ErrorActionPreference = 'SilentlyContinue'
        (& docker buildx imagetools inspect $img --format $tpl 2>$null | Select-Object -First 1)
    }
    if ($digest -and $digest -ne '<no value>') {
        $base = $img -replace '@.*$',''
        $repo = $base -replace ':[^:/]+$',''
        return "${repo}@$digest"
    }
    # No manifest list -- single-arch image. Verify the platform matches.
    $singleArch = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker buildx imagetools inspect $img --format '{{.Manifest.Config.Platform.Architecture}}' 2>$null
    }
    if (-not $singleArch) {
        $singleArch = & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker buildx imagetools inspect $img --format '{{.Image.architecture}}' 2>$null
        }
    }
    if ($singleArch -eq $arch) { return $img }
    return $null
}

function Invoke-StageArch($arch) {
    $tarball = Join-Path $OutDir "images-$arch.tar"
    Banner "Stage linux/$arch -> $(Split-Path -Leaf $tarball)"

    $count = 0
    $pulledTags = @()
    foreach ($img in $resolved) {
        if (-not $img) { continue }
        $count++
        Say "[$count] resolve $arch  $img"

        $ref = Resolve-PlatformRef $img $arch
        if (-not $ref) {
            if ($img -match 'rangerdanger-openplc') {
                # Apple Silicon students need amd64 openplc; upstream
                # tuttas/openplc_v3 is amd64-only and compose pins
                # platform: linux/amd64 so it runs under Rosetta on M1.
                # Cross-include the amd64 image in the arm64 bundle.
                $ref = Resolve-PlatformRef $img 'amd64'
                if (-not $ref) { Die "openplc: amd64 fallback resolution also failed" }
                Say "    cross-arch (amd64 image, runs on arm64 via Rosetta): $ref"
            } else {
                Say "    skip -- not available for linux/$arch"
                continue
            }
        } elseif ($ref -ne $img) {
            Say "    -> $ref"
        }

        # Heads-up on the large images so a multi-minute pull doesn't look
        # like a hang (issue #81); show native layer progress (no --quiet).
        if ($img -match 'rangerdanger-(eng-ws|vendor-jump)') {
            Say "    large image (~2-3 GB desktop) -- a few minutes is normal"
        } elseif ($img -match 'rangerdanger-openplc') {
            Say "    large image (~1 GB) -- give it a minute"
        }
        & docker pull $ref
        if ($LASTEXITCODE -ne 0) {
            Die "pull failed for $ref on $arch -- refusing to write a partial bundle. Fix the upstream issue (auth, network, image name), then re-run."
        }

        # Re-tag locally so the saved tarball carries the friendly tag.
        $targetTag = $img -replace '@.*$',''
        if ($ref -ne $targetTag) {
            & docker tag $ref $targetTag
            if ($LASTEXITCODE -ne 0) { Die "docker tag $ref -> $targetTag failed" }
        }
        $pulledTags += $targetTag
    }

    Say "save $arch -> $tarball"
    & docker save -o $tarball @pulledTags
    if ($LASTEXITCODE -ne 0) { Die "docker save failed for $arch" }

    $sizeMB = [math]::Round((Get-Item $tarball).Length / 1MB)
    Say "wrote $tarball (${sizeMB} MB)"
}

Invoke-StageArch 'amd64'
Invoke-StageArch 'arm64'

Banner "Stage repo archive -> rangerdanger.tgz"
$tgzPath = Join-Path $OutDir "rangerdanger.tgz"
# git can produce gzipped tar directly via --format=tar.gz. Avoids
# needing an external gzip on Windows.
# --prefix=rangerdanger/ so extraction creates a rangerdanger/ folder
# instead of scattering repo files into the extract target (which made
# the generated README's `cd ~/rangerdanger` fail). All extract docs
# assume this prefix.
& git -C $RootDir archive --prefix=rangerdanger/ --format=tar.gz -o $tgzPath HEAD
if ($LASTEXITCODE -ne 0) { Die "git archive failed" }
$tgzSizeMB = [math]::Round((Get-Item $tgzPath).Length / 1MB, 2)
Say "wrote $tgzPath (${tgzSizeMB} MB)"

# .version file so setup.ps1 -FromTarballs can auto-pick the right tag.
$verPath = Join-Path $OutDir ".version"
Set-Content -Path $verPath -Value $Version -NoNewline -Encoding ascii
Say "wrote $verPath ($Version)"

# Bundle the WSL2 kernel asset for Windows students on offline /
# air-gapped laptops. setup.ps1 -FromTarballs picks up
# rangerdanger-wsl2-kernel + .sha256 from here automatically; without
# them, the kernel install step needs internet. Graceful skip if the
# asset isn't yet built for $Version (CI builds it on tag push).
Banner "Bundle WSL2 kernel asset (Windows offline support)"
$ghOwnerRepo = if ($env:GH_OWNER_REPO) { $env:GH_OWNER_REPO } else { "tonylturner/rangerdanger" }
$kernelUrl = if ($Version -eq 'latest') {
    "https://github.com/$ghOwnerRepo/releases/latest/download/rangerdanger-wsl2-kernel"
} else {
    "https://github.com/$ghOwnerRepo/releases/download/$Version/rangerdanger-wsl2-kernel"
}
$kernelShaUrl = "$kernelUrl.sha256"
$kernelReadmeRow = ""
try {
    $head = Invoke-WebRequest -Uri $kernelUrl -Method Head -UseBasicParsing -TimeoutSec 10 -ErrorAction Stop
    if ($head.StatusCode -ne 200) { throw "HTTP $($head.StatusCode)" }
    Say "Downloading $kernelUrl"
    Invoke-WebRequest -Uri $kernelUrl -OutFile (Join-Path $OutDir "rangerdanger-wsl2-kernel") -UseBasicParsing -ErrorAction Stop
    try {
        Invoke-WebRequest -Uri $kernelShaUrl -OutFile (Join-Path $OutDir "rangerdanger-wsl2-kernel.sha256") -UseBasicParsing -ErrorAction Stop
    } catch {
        Warn "kernel sha256 download failed; on-install verification will be skipped."
    }
    $ksize = [math]::Round((Get-Item (Join-Path $OutDir "rangerdanger-wsl2-kernel")).Length / 1MB, 1)
    Say "wrote $OutDir\rangerdanger-wsl2-kernel ($ksize MB)"
    $kernelReadmeRow = "- ``rangerdanger-wsl2-kernel`` + ``.sha256`` -- custom WSL2 kernel with CONFIG_NFT_QUEUE=y for Windows ICS DPI labs (see wsl-kernel/README.md). ``setup.ps1 -FromTarballs`` picks it up automatically."
} catch {
    Warn "rangerdanger-wsl2-kernel not yet published for release $Version."
    Warn "  (.github/workflows/build-wsl-kernel.yml builds the kernel on tag push."
    Warn "   If you are staging before that workflow has run, re-run stage-ssd.ps1 after the"
    Warn "   kernel asset attaches to the release, OR manually drop rangerdanger-wsl2-kernel"
    Warn "   + .sha256 into $OutDir.)"
    Warn "  Without the kernel, Windows students on this SSD lose ICS DPI on Labs 2.3 / 2.3-bonus."
}

Banner "Write launchers (one-click student entry points)"
# Self-locating launchers students double-click -- the file lives on the
# SSD so its own dir is the tarball dir (no path to type, no drive letter
# to guess, no docker-load-vs-tar choice). The .cmd also bypasses
# PowerShell's execution policy. Write .cmd CRLF + .command LF explicitly
# (normalized here, not left to the host) so a Windows- or mac-staged SSD
# produces the same files; UTF-8 no-BOM (a BOM breaks cmd.exe's first line).
$noBom = New-Object System.Text.UTF8Encoding($false)
$startCmd = @'
@echo off
setlocal
rem RangerDanger workshop launcher. This file lives on the SSD, so %~dp0
rem is the SSD path -- you never type a path and the drive letter does not
rem matter. Just double-click it.
set "REPO=%USERPROFILE%\rangerdanger"

echo ============================================================
echo   RangerDanger workshop launcher
echo   Lab files (this SSD): %~dp0
echo   Installs to:          %REPO%
echo ============================================================
echo.

if not exist "%REPO%\setup.ps1" (
  echo [+] Unpacking lab files ^(one-time, ~1 MB^)...
  tar -xzf "%~dp0rangerdanger.tgz" -C "%USERPROFILE%"
  if errorlevel 1 (
    echo [x] Could not unpack rangerdanger.tgz from this SSD.
    echo     Run this file from the SSD itself, not a copy on the Desktop.
    pause
    exit /b 1
  )
) else (
  echo [+] Reusing existing install at %REPO%.
)

echo [+] Starting setup ^(loads images from the SSD, brings the stack up^)...
echo.
powershell -NoProfile -ExecutionPolicy Bypass -File "%REPO%\setup.ps1" -FromTarballs "%~dp0."
echo.
echo [i] When you see "RangerDanger is up", open http://localhost:8088/exercises
pause
'@
$cmdCrlf = (($startCmd -replace "`r`n","`n") -replace "`n","`r`n")
[System.IO.File]::WriteAllText((Join-Path $OutDir "START-HERE.cmd"), $cmdCrlf, $noBom)
Say "wrote $OutDir\START-HERE.cmd"

$startCommand = @'
#!/bin/bash
# RangerDanger workshop launcher (macOS / Linux). Double-click in Finder,
# or run:  bash start-here.command
set -e
SSD="$(cd "$(dirname "$0")" && pwd)"
REPO="$HOME/rangerdanger"
echo "Lab files (this SSD): $SSD"
echo "Installs to:          $REPO"
if [ ! -f "$REPO/setup.sh" ]; then
  echo "[+] Unpacking lab files (one-time)..."
  tar xzf "$SSD/rangerdanger.tgz" -C "$HOME"
fi
exec "$REPO/setup.sh" --from-tarballs "$SSD"
'@
$cmdLf = ($startCommand -replace "`r`n","`n")
[System.IO.File]::WriteAllText((Join-Path $OutDir "start-here.command"), $cmdLf, $noBom)
Say "wrote $OutDir\start-here.command"

Banner "Write README"
$shortSha = & git -C $RootDir rev-parse --short HEAD
$lastSubject = & git -C $RootDir log -1 --format=%s
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$readme = @"
# RangerDanger -- offline / SSD install

Staged $now for version ``$Version``.

## Contents

- ``images-amd64.tar`` -- Docker images for Intel / AMD64 hosts
- ``images-arm64.tar`` -- Docker images for Apple Silicon / ARM64 hosts (openplc is cross-included as the amd64 image and runs under Rosetta 2)
- ``rangerdanger.tgz`` -- Repo archive at $shortSha ($($lastSubject.Substring(0, [Math]::Min(80, $lastSubject.Length))))
$kernelReadmeRow

## Quick start (recommended)

- **Windows:** double-click **START-HERE.cmd** on this SSD.
- **macOS:** double-click **start-here.command** (or run ``bash start-here.command``).

The launcher unpacks the lab files to ``~/rangerdanger`` and runs setup
for you -- no path to type, no drive letter to guess.

You do **not** need to extract ``images-*.tar`` -- setup loads it for you
with ``docker load``.

## Manual (if you prefer)

Replace <SSD> with this drive (e.g. /Volumes/WORKSHOP_SSD on macOS, or the
SSD's drive letter such as D: on Windows):

``````sh
tar xzf <SSD>/rangerdanger.tgz -C ~      # creates ~/rangerdanger
cd ~/rangerdanger
./setup.sh --from-tarballs <SSD>
``````

(or ``.\setup.ps1 -FromTarballs <SSD>`` on Windows.)

``setup.sh`` / ``setup.ps1`` auto-detects the host architecture and
loads the right ``images-<arch>.tar`` before bringing the stack up.
"@
Set-Content -Path (Join-Path $OutDir "README.md") -Value $readme -Encoding utf8
Say "wrote $OutDir\README.md"

Banner "Done"
Write-Host ""
Write-Host "  Output dir: $OutDir"
Get-ChildItem $OutDir | ForEach-Object {
    $sz = if ($_.Length -gt 1MB) { "{0:N1} MB" -f ($_.Length/1MB) } else { "{0:N0} B" -f $_.Length }
    Write-Host ("  {0,-30} {1}" -f $_.Name, $sz)
}
$total = (Get-ChildItem $OutDir -Recurse | Measure-Object -Property Length -Sum).Sum
Write-Host ""
Write-Host "  Total: $([math]::Round($total/1MB, 1)) MB"
