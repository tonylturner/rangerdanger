<#
.SYNOPSIS
RangerDanger SSD delta-stage helper -- Windows sibling of stage-ssd-delta.sh.

.DESCRIPTION
Compares two release versions and saves only the images whose content
changed between them. Use this to push a mid-workshop fix as a
tens-of-MB tarball instead of re-shipping the full ~6 GB bundle.

.PARAMETER OutDir
Directory to write the delta into. Created if it does not exist.

.PARAMETER Since
The version the student already has (e.g. v0.1.6).

.PARAMETER New
The new version to ship (e.g. v0.1.7).

.PARAMETER Include
Comma-separated short image names (or "rangerdanger-X" form) to force
into the delta even if their digest matches.

.PARAMETER All
Ignore digest comparison; save every image at <New>.

.PARAMETER IncludeUpstream
Also delta-check non-rangerdanger upstream images (containd/nginx/
fuxa/webtop/alpine). Usually skipped because they are pinned by digest
already.

.EXAMPLE
.\stage-ssd-delta.ps1 D:\WORKSHOP_SSD\delta-v0.1.7 v0.1.6 v0.1.7
.\stage-ssd-delta.ps1 .\out v0.1.6 v0.1.7 -Include backend,frontend
.\stage-ssd-delta.ps1 .\out v0.1.5 v0.1.7 -All

.NOTES
See stage-ssd.ps1 for the rationale on resolve_platform_ref (long
comment in the .sh version explains the manifest-list trap that this
indirection avoids). ASCII-only, BOM-free. See setup.ps1 for the
encoding rationale.
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true, Position=0)][string]$OutDir,
    [Parameter(Mandatory=$true, Position=1)][string]$Since,
    [Parameter(Mandatory=$true, Position=2)][string]$New,
    [string[]]$Include = @(),
    [switch]$All,
    [switch]$IncludeUpstream
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

# Normalize -Include into a flat list of short names.
$includeSet = @()
foreach ($i in $Include) {
    if ($i) { $includeSet += ($i -split ',') | ForEach-Object { $_.Trim() } | Where-Object { $_ } }
}

Say "Output:           $OutDir"
Say "Since version:    $Since"
Say "New version:      $New"
if ($All)             { Say "Mode:             -All (skip digest comparison)" }
if ($includeSet)      { Say "Force-include:    $($includeSet -join ', ')" }
if ($IncludeUpstream) { Say "Upstream images:  included in delta check" }

$allImages = & docker compose -f $ComposeFile config --images 2>$null | Sort-Object -Unique
if (-not $allImages) { Die "Could not enumerate images from $ComposeFile" }

$candidate = if ($IncludeUpstream) {
    $allImages
} else {
    $allImages | Where-Object { $_ -match 'ghcr\.io/tonylturner/' }
}

function Resolve-Version($images, $version) {
    $images | ForEach-Object {
        $i = $_
        # First-party rangerdanger-* / containd with :latest -> :version
        $i = $i -replace '^(ghcr\.io/tonylturner/(rangerdanger-[a-z0-9-]+|containd)):latest$', "`$1:$version"
        # First-party with any non-digest tag -> :version
        $i = $i -replace '^(ghcr\.io/tonylturner/(rangerdanger-[a-z0-9-]+|containd)):[^@]+$', "`$1:$version"
        $i
    }
}

$sinceRef = @(Resolve-Version $candidate $Since)
$newRef   = @(Resolve-Version $candidate $New)

function Get-RemoteDigest($ref) {
    $d = & {
        $ErrorActionPreference = 'SilentlyContinue'
        & docker buildx imagetools inspect --format '{{.Manifest.Digest}}' $ref 2>$null
    }
    if ($d) { return ($d -replace "`r","" -replace "`n","").Trim() }
    return ""
}

Banner "Comparing $Since -> $New across $($candidate.Count) candidate image(s)"

$changed       = New-Object System.Collections.Generic.List[string]
$unchanged     = New-Object System.Collections.Generic.List[string]
$forced        = New-Object System.Collections.Generic.List[string]
$missingSince  = New-Object System.Collections.Generic.List[string]

for ($i = 0; $i -lt $newRef.Count; $i++) {
    $newImg   = $newRef[$i]
    $sinceImg = $sinceRef[$i]
    if (-not $newImg) { continue }

    $short = ($newImg -replace '.*/','' -replace ':.*','')

    if ($includeSet -contains $short) {
        $forced.Add($short) | Out-Null
        $changed.Add($newImg) | Out-Null
        continue
    }
    if ($All) { $changed.Add($newImg) | Out-Null; continue }

    $newDigest   = Get-RemoteDigest $newImg
    $sinceDigest = Get-RemoteDigest $sinceImg

    if (-not $newDigest) {
        Warn "  ${short}: could not read digest for $newImg -- including in delta to be safe"
        $changed.Add($newImg) | Out-Null
        continue
    }
    if (-not $sinceDigest) {
        Warn "  ${short}: could not read $sinceImg (not pulled?) -- including in delta to be safe"
        $missingSince.Add($short) | Out-Null
        $changed.Add($newImg) | Out-Null
        continue
    }
    if ($newDigest -eq $sinceDigest) { $unchanged.Add($short) | Out-Null }
    else                              { $changed.Add($newImg) | Out-Null }
}

Write-Host ""
Say "  Changed (will be in delta):"
if ($changed.Count -eq 0) {
    Write-Host "    (none)"
} else {
    foreach ($c in $changed) {
        $short = ($c -replace '.*/','' -replace ':.*','')
        Write-Host "    $short  ($c)"
    }
}
Write-Host ""
if ($forced.Count -gt 0)       { Say "  Forced via -Include: $($forced -join ', ')" }
if ($missingSince.Count -gt 0) { Warn "  Could not read since digests for: $($missingSince -join ', ')" }
Say "  Unchanged (skipped from delta):"
if ($unchanged.Count -eq 0) { Write-Host "    (none)" } else { foreach ($u in $unchanged) { Write-Host "    $u" } }
Write-Host ""

if ($changed.Count -eq 0) {
    Warn "No image changes detected. Use -All or -Include to force, or just ship a new rangerdanger.tgz alone if only repo content changed."
}

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
    if ($changed.Count -eq 0) { return }
    $tarball = Join-Path $OutDir "delta-$arch.tar"
    Banner "Stage linux/$arch -> $(Split-Path -Leaf $tarball)"

    $pulledTags = @()
    foreach ($img in $changed) {
        Say "resolve $arch  $img"
        $ref = Resolve-PlatformRef $img $arch
        if (-not $ref) {
            if ($img -match 'rangerdanger-openplc') {
                $ref = Resolve-PlatformRef $img 'amd64'
                if (-not $ref) { Die "openplc: amd64 fallback resolution also failed" }
                Say "    cross-arch (amd64 image, runs on arm64 via Rosetta): $ref"
            } else {
                Say "    skip - not available for linux/$arch"
                continue
            }
        } elseif ($ref -ne $img) {
            Say "    -> $ref"
        }
        & docker pull --quiet $ref | Out-Null
        if ($LASTEXITCODE -ne 0) { Die "pull failed for $ref on $arch - re-run after fixing the upstream issue." }
        $targetTag = $img -replace '@.*$',''
        if ($ref -ne $targetTag) {
            & docker tag $ref $targetTag
            if ($LASTEXITCODE -ne 0) { Die "docker tag $ref -> $targetTag failed" }
        }
        $pulledTags += $targetTag
    }
    if ($pulledTags.Count -eq 0) {
        Say "Nothing to save for $arch (no images compatible with this arch)"
        return
    }
    Say "save $arch -> $tarball"
    & docker save -o $tarball @pulledTags
    if ($LASTEXITCODE -ne 0) { Die "docker save failed for $arch" }
    $sizeMB = [math]::Round((Get-Item $tarball).Length / 1MB)
    Say "wrote $tarball (${sizeMB} MB)"
}

if ($changed.Count -gt 0) {
    Invoke-StageArch 'amd64'
    Invoke-StageArch 'arm64'
}

Banner "Stage repo archive -> rangerdanger.tgz"
$tgzPath = Join-Path $OutDir "rangerdanger.tgz"
& git -C $RootDir archive --format=tar.gz -o $tgzPath HEAD
if ($LASTEXITCODE -ne 0) { Die "git archive failed" }
$tgzSizeMB = [math]::Round((Get-Item $tgzPath).Length / 1MB, 2)
Say "wrote $tgzPath (${tgzSizeMB} MB)"

# Bundle the WSL2 kernel asset for $New. Deltas include the kernel
# unconditionally (it is small, and a student applying a delta may
# have skipped an older delta that lacked the kernel). Graceful skip
# if not yet published for $New.
Banner "Bundle WSL2 kernel asset for $New (Windows offline support)"
$ghOwnerRepo = if ($env:GH_OWNER_REPO) { $env:GH_OWNER_REPO } else { "tonylturner/rangerdanger" }
$kernelUrl = "https://github.com/$ghOwnerRepo/releases/download/$New/rangerdanger-wsl2-kernel"
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
    $kernelReadmeRow = "- ``rangerdanger-wsl2-kernel`` + ``.sha256`` -- custom WSL2 kernel for Windows ICS DPI labs (``setup.ps1 -FromTarballs`` picks it up automatically)."
} catch {
    Warn "rangerdanger-wsl2-kernel not yet published for release $New."
    Warn "  (Re-run this delta after the kernel asset publishes, OR drop the file into $OutDir manually.)"
}

Banner "Write DELTA-README.md"
$applyTableRows = foreach ($img in $changed) {
    $short = ($img -replace '.*/','' -replace ':.*','')
    $svc   = ($short -replace '^rangerdanger-','') -replace '-','_'
    "| ``$short`` | ``$svc`` |"
}
$applyTable = $applyTableRows -join "`n"
$unchangedList = if ($unchanged.Count -gt 0) {
    "## Unchanged (kept from prior install)`n`n" + (($unchanged | ForEach-Object { "- $_" }) -join "`n")
} else { "" }
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$readme = @"
# RangerDanger -- delta patch

Staged $now for upgrade from ``$Since`` -> ``$New``.

## Changed

| Image | Compose service |
|---|---|
$applyTable
$kernelReadmeRow

$unchangedList

## Apply

Run from the student's existing ``~/rangerdanger`` directory:

``````sh
# 1. update the repo (always)
docker compose down
tar xzf <delta-dir>/rangerdanger.tgz -C ~/rangerdanger-new
cd ~/rangerdanger-new

# 2. load only the changed images for this host's arch
ARCH=`$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
docker load -i <delta-dir>/delta-`$ARCH.tar

# 3. restart with the offline overlay so docker compose does not try to pull
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
``````

``docker compose up -d`` (no service list) is safe -- compose only
recreates containers whose image digest changed.

## Rollback

Pull the prior ``$Since`` images directly from GHCR:

``````sh
VERSION=$Since docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
``````
"@
Set-Content -Path (Join-Path $OutDir "DELTA-README.md") -Value $readme -Encoding utf8
Say "wrote $OutDir\DELTA-README.md"

Banner "Done"
Write-Host ""
Write-Host "  Output dir:       $OutDir"
Write-Host "  Changed images:   $($changed.Count)"
Write-Host "  Unchanged:        $($unchanged.Count)"
Write-Host ""
Get-ChildItem $OutDir | ForEach-Object {
    $sz = if ($_.Length -gt 1MB) { "{0:N1} MB" -f ($_.Length/1MB) } else { "{0:N0} B" -f $_.Length }
    Write-Host ("  {0,-30} {1}" -f $_.Name, $sz)
}
$total = (Get-ChildItem $OutDir -Recurse | Measure-Object -Property Length -Sum).Sum
Write-Host ""
Write-Host "  Total: $([math]::Round($total/1MB, 1)) MB"
Write-Host ""
Write-Host "  Distribute the files in $OutDir to students. The README in"
Write-Host "  that directory contains the exact apply commands."
