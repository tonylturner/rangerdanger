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

# Binfmt is included in arm64 deltas so students applying deltas without
# internet still have qemu-x86_64 for the amd64-only OpenPLC image.
# Keep this pin in sync with setup.sh and stage-ssd-delta.sh.
$BINFMT_IMAGE = "tonistiigi/binfmt:qemu-v10.2.1"

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
    $allImages | Where-Object { $_ -match 'ghcr\.io/tonylturner/rangerdanger-' }
}

function Resolve-Version($images, $version) {
    $images | ForEach-Object {
        $i = $_
        # First-party rangerdanger-* with :latest or another tag -> :version.
        $i = $i -replace '^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):latest$', "`$1:$version"
        $i = $i -replace '^(ghcr\.io/tonylturner/rangerdanger-[a-z0-9-]+):[^@]+$', "`$1:$version"
        $i
    }
}

$sinceRef = @(Resolve-Version $candidate $Since)
$newRef   = @(Resolve-Version $candidate $New)

function Get-RegistryName($img) {
    $registry = "docker.io"
    $firstComponent = ($img -split '/', 2)[0]
    if (($img.Contains('/') -and ($firstComponent -match '[.:]' -or $firstComponent -eq 'localhost'))) {
        $registry = $firstComponent
    }
    return $registry
}

function Stop-IfRateLimited($img, $details) {
    if ($details -notmatch '(^|[^a-z0-9])429\s+too\s+many\s+requests([^a-z0-9]|$)|toomanyrequests|too\s+many\s+requests') {
        return
    }
    $registry = Get-RegistryName $img
    if ($registry -eq "docker.io" -or $registry -eq "index.docker.io") {
        Die "Docker Hub anonymous pull limit reached while inspecting $img. The anonymous pull budget resets within the hour; wait or run 'docker login' and retry."
    }
    Die "$registry rate-limited the request while inspecting $img. Wait for its limit to reset or authenticate to $registry with 'docker login $registry', then retry."
}

function Get-JsonField($object, $name) {
    if ($null -eq $object) { return $null }
    $property = $object.PSObject.Properties[$name]
    if ($property) { return $property.Value }
    return $null
}

function Stop-ManifestInspection($img, $details) {
    $message = "could not inspect the registry manifest for $img; this is an inspection error, not evidence that a platform is absent."
    if ($details) { $message += " Registry detail: $($details.Trim())" }
    Die $message
}

function Stop-PlatformManifest($img, $details, [switch]$Optional, [switch]$NewVersion) {
    Stop-IfRateLimited $img $details
    if ($Optional) { return $null }
    if ($NewVersion) {
        $message = "new-version registry manifest could not be inspected for $img; this is not a platform-availability result."
        if ($details) { $message += " Registry detail: $($details.Trim())" }
        Die $message
    }
    Stop-ManifestInspection $img $details
}

function Get-PlatformManifest($img, [switch]$Optional, [switch]$NewVersion) {
    $errorFile = [System.IO.Path]::GetTempFileName()
    try {
        $inspectionOutput = & {
            $ErrorActionPreference = 'SilentlyContinue'
            & docker buildx imagetools inspect $img --format '{{json .}}' 2> $errorFile
        }
        $inspectStatus = $LASTEXITCODE
        $inspectError = Get-Content -Path $errorFile -Raw -ErrorAction SilentlyContinue
    } finally {
        Remove-Item -Path $errorFile -Force -ErrorAction SilentlyContinue
    }
    if ($inspectStatus -ne 0) {
        return (Stop-PlatformManifest $img $inspectError -Optional:$Optional -NewVersion:$NewVersion)
    }
    $json = ($inspectionOutput -join "`n").Trim()
    if (-not $json) {
        return (Stop-PlatformManifest $img "inspection returned no manifest JSON" -Optional:$Optional -NewVersion:$NewVersion)
    }
    try {
        $inspection = ConvertFrom-Json -InputObject $json -ErrorAction Stop
    } catch {
        return (Stop-PlatformManifest $img "invalid manifest JSON: $($_.Exception.Message)" -Optional:$Optional -NewVersion:$NewVersion)
    }
    $manifest = Get-JsonField $inspection 'Manifest'
    if ($null -eq $manifest -or $manifest -isnot [pscustomobject]) {
        return (Stop-PlatformManifest $img "inspection has no readable manifest" -Optional:$Optional -NewVersion:$NewVersion)
    }

    $rootDigest = Get-JsonField $manifest 'Digest'
    if (-not $rootDigest -or $rootDigest -eq '<no value>') { $rootDigest = "" }
    $refs = @{ amd64 = ""; arm64 = "" }
    $manifestsProperty = $manifest.PSObject.Properties['Manifests']
    if ($manifestsProperty -and $null -ne $manifestsProperty.Value) {
        if ($manifestsProperty.Value -isnot [array]) {
            return (Stop-PlatformManifest $img "manifest index has no readable manifests list" -Optional:$Optional -NewVersion:$NewVersion)
        }
        foreach ($entry in $manifestsProperty.Value) {
            $platform = Get-JsonField $entry 'Platform'
            $operatingSystem = Get-JsonField $platform 'OS'
            $architecture = Get-JsonField $platform 'Architecture'
            if ($operatingSystem -ne 'linux' -or $architecture -notin @('amd64', 'arm64')) { continue }
            $digest = Get-JsonField $entry 'Digest'
            if (-not $digest -or $digest -eq '<no value>') {
                return (Stop-PlatformManifest $img "Linux image manifest entry has no digest" -Optional:$Optional -NewVersion:$NewVersion)
            }
            if (-not $refs[$architecture]) {
                $base = ($img -split '@', 2)[0]
                $lastComponent = ($base -split '/')[-1]
                if ($lastComponent.Contains(':')) {
                    $base = $base.Substring(0, $base.LastIndexOf(':'))
                }
                $refs[$architecture] = "${base}@$digest"
            }
        }
    } else {
        $image = Get-JsonField $inspection 'Image'
        $architecture = Get-JsonField $image 'Architecture'
        $operatingSystem = Get-JsonField $image 'OS'
        if (-not $architecture -or -not $operatingSystem) {
            return (Stop-PlatformManifest $img "single-image manifest has no readable platform architecture or operating system" -Optional:$Optional -NewVersion:$NewVersion)
        }
        if ($operatingSystem -eq 'linux' -and $architecture -in @('amd64', 'arm64')) {
            $refs[$architecture] = $img
        }
    }
    return @{ Digest = $rootDigest; Refs = $refs }
}

Banner "Comparing $Since -> $New across $($candidate.Count) candidate image(s)"

$changed       = New-Object System.Collections.Generic.List[string]
$changedManifests = New-Object System.Collections.Generic.List[object]
$changedSince  = New-Object System.Collections.Generic.List[string]
$unchanged     = New-Object System.Collections.Generic.List[string]
$unchangedSince = New-Object System.Collections.Generic.List[string]
$unchangedNew = New-Object System.Collections.Generic.List[string]
$forced        = New-Object System.Collections.Generic.List[string]
$missingSince  = New-Object System.Collections.Generic.List[string]

for ($i = 0; $i -lt $newRef.Count; $i++) {
    $newImg   = $newRef[$i]
    $sinceImg = $sinceRef[$i]
    if (-not $newImg) { continue }

    # Reuse this inspection's platform refs for preflight after comparison.
    $newManifest = Get-PlatformManifest $newImg -NewVersion
    $newDigest = $newManifest.Digest
    if (-not $newDigest) {
        Die "new version image is missing from the registry or its manifest digest cannot be read: $newImg"
    }

    $short = ($newImg -replace '.*/','' -replace ':.*','')

    if ($includeSet -contains $short) {
        $forced.Add($short) | Out-Null
        $changed.Add($newImg) | Out-Null
        $changedManifests.Add($newManifest) | Out-Null
        $changedSince.Add($sinceImg) | Out-Null
        continue
    }
    if ($All) {
        $changed.Add($newImg) | Out-Null
        $changedManifests.Add($newManifest) | Out-Null
        $changedSince.Add($sinceImg) | Out-Null
        continue
    }

    if ($sinceImg -ceq $newImg) {
        $sinceDigest = $newDigest
    } else {
        $sinceManifest = Get-PlatformManifest $sinceImg -Optional
        $sinceDigest = if ($null -eq $sinceManifest) { "" } else { $sinceManifest.Digest }
    }

    if (-not $sinceDigest) {
        Warn "  ${short}: could not read $sinceImg (not pulled?) -- including in delta to be safe"
        $missingSince.Add($short) | Out-Null
        $changed.Add($newImg) | Out-Null
        $changedManifests.Add($newManifest) | Out-Null
        $changedSince.Add($sinceImg) | Out-Null
        continue
    }
    if ($newDigest -ceq $sinceDigest) {
        $unchanged.Add($short) | Out-Null
        $unchangedSince.Add($sinceImg) | Out-Null
        $unchangedNew.Add($newImg) | Out-Null
    } else {
        $changed.Add($newImg) | Out-Null
        $changedManifests.Add($newManifest) | Out-Null
        $changedSince.Add($sinceImg) | Out-Null
    }
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

$preflightRefs = @{}
function Add-PreflightImage($img, $manifest, [switch]$Arm64Only) {
    Say "preflight $img"
    $refs = $manifest.Refs
    $amd64Ref = $refs['amd64']
    $arm64Ref = $refs['arm64']
    if (-not $Arm64Only -and -not $amd64Ref) {
        Die "readable manifest for $img has no linux/amd64 image."
    }
    $crossArch = $false
    if (-not $arm64Ref) {
        if ($img -match 'rangerdanger-openplc' -and $amd64Ref) {
            $arm64Ref = $amd64Ref
            $crossArch = $true
            Say "    openplc amd64 image will be cross-included for arm64"
        } else {
            Die "readable manifest for $img has no linux/arm64 image; openplc is the only image allowed to cross-include another architecture."
        }
    }
    $preflightRefs[$img] = @{
        amd64 = $amd64Ref
        arm64 = $arm64Ref
        arm64Cross = $crossArch
    }
}

function Invoke-StageArch($arch) {
    if ($changed.Count -eq 0) { return }
    $tarball = Join-Path $OutDir "delta-$arch.tar"
    Banner "Stage linux/$arch -> $(Split-Path -Leaf $tarball)"

    $pulledTags = @()
    $toStage = @($changed)
    if ($arch -eq 'arm64') {
        # binfmt is not part of the changed-image comparison; always include
        # it in an arm64 delta so OpenPLC works offline on arm64 Linux.
        $toStage += $BINFMT_IMAGE
    }
    foreach ($img in $toStage) {
        Say "stage $arch  $img"
        if (-not $preflightRefs.ContainsKey($img)) {
            Die "no preflight result for $img on linux/$arch"
        }
        $preflight = $preflightRefs[$img]
        $ref = if ($arch -eq 'amd64') { $preflight.amd64 } else { $preflight.arm64 }
        if (-not $ref) { Die "no resolved manifest for $img on linux/$arch" }
        if ($arch -eq 'arm64' -and $preflight.arm64Cross) {
            Say "    cross-arch (amd64 image, runs on arm64 via emulation): $ref"
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
    for ($i = 0; $i -lt $changed.Count; $i++) {
        Add-PreflightImage $changed[$i] $changedManifests[$i]
    }
    $binfmtManifest = Get-PlatformManifest $BINFMT_IMAGE
    Add-PreflightImage $BINFMT_IMAGE $binfmtManifest -Arm64Only
    Invoke-StageArch 'amd64'
    Invoke-StageArch 'arm64'
}

Banner "Stage repo archive -> rangerdanger.tgz"
$tgzPath = Join-Path $OutDir "rangerdanger.tgz"
# --prefix=rangerdanger/ so extraction creates a self-contained
# rangerdanger/ folder (see stage-ssd.ps1). The delta apply extracts
# over the student's existing ~/rangerdanger in place.
& git -C $RootDir archive --prefix=rangerdanger/ --format=tar.gz -o $tgzPath HEAD
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
# Windows PowerShell 5.1 draws a per-chunk progress bar for
# Invoke-WebRequest -OutFile, which can stall a 25 MB download for
# minutes. Suppress it for the kernel transfer and restore it after.
$previousProgress = $ProgressPreference
$ProgressPreference = 'SilentlyContinue'
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
} finally {
    $ProgressPreference = $previousProgress
}

Banner "Write DELTA-README.md"
$unchangedList = if ($unchanged.Count -gt 0) {
    "## Unchanged (kept from prior install)`n`n" + (($unchanged | ForEach-Object { "- $_" }) -join "`n")
} else { "" }
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$composeJson = & docker compose -f $ComposeFile config --format json 2>$null
if ($LASTEXITCODE -ne 0 -or -not $composeJson) { Die "Could not read the resolved service model from $ComposeFile" }
try {
    $composeModel = ($composeJson -join "`n") | ConvertFrom-Json -ErrorAction Stop
} catch {
    Die "Could not parse the resolved service model from $ComposeFile"
}

function Get-ImageRepository($reference) {
    $base = ($reference -split '@', 2)[0]
    $lastComponent = ($base -split '/')[-1]
    if ($lastComponent.Contains(':')) {
        $base = $base.Substring(0, $base.LastIndexOf(':'))
    }
    return $base
}

$serviceRepositories = @{}
foreach ($serviceProperty in $composeModel.services.PSObject.Properties) {
    $serviceImage = $serviceProperty.Value.image
    if ($serviceImage) {
        $repository = Get-ImageRepository $serviceImage
        if (-not $serviceRepositories.ContainsKey($repository)) {
            $serviceRepositories[$repository] = @()
        }
        $serviceRepositories[$repository] += $serviceProperty.Name
    }
}

$applyTableRows = foreach ($img in $changed) {
    $repository = Get-ImageRepository $img
    $short = ($repository -split '/')[-1]
    $services = $serviceRepositories[$repository]
    if (-not $services) { Die "Changed image $img does not map to a service in $ComposeFile" }
    "| ``$short`` | ``$($services -join ', ')`` |"
}
$applyTable = $applyTableRows -join "`n"

$applyLoadCommand = if ($changed.Count -gt 0) {
    'ARCH=$(uname -m | sed ''s/x86_64/amd64/;s/aarch64/arm64/'')
docker load -i "$DELTA_DIR/delta-$ARCH.tar"'
} else {
    "# No image archive was created; no image load is needed."
}

$applyBackupCommands = New-Object System.Collections.Generic.List[string]
$rollbackRestoreCommands = New-Object System.Collections.Generic.List[string]
for ($i = 0; $i -lt $changed.Count; $i++) {
    $sinceTag = ($changedSince[$i] -split '@', 2)[0]
    $targetTag = ($changed[$i] -split '@', 2)[0]
    if ($sinceTag -ne $targetTag) { continue }

    $targetRepository = Get-ImageRepository $targetTag
    $parkedTag = "${targetRepository}:before-$New"
    $applyBackupCommands.Add(@"
if ! docker image inspect "$parkedTag" >/dev/null 2>&1; then
    if docker image inspect "$targetTag" >/dev/null 2>&1; then
        docker image tag "$targetTag" "$parkedTag"
    fi
fi
"@.Trim()) | Out-Null
    $rollbackRestoreCommands.Add(@"
if docker image inspect "$parkedTag" >/dev/null 2>&1; then
    docker image tag "$parkedTag" "$targetTag"
fi
"@.Trim()) | Out-Null
}
if ($applyBackupCommands.Count -eq 0) {
    $applyBackupText = "# No mutable image tags need to be parked."
    $rollbackRestoreText = "# No mutable image tags need to be restored."
} else {
    $applyBackupText = $applyBackupCommands -join "`n"
    $rollbackRestoreText = $rollbackRestoreCommands -join "`n"
}

$applyRetagCommands = New-Object System.Collections.Generic.List[string]
for ($i = 0; $i -lt $unchangedNew.Count; $i++) {
    if ($unchangedNew[$i] -like 'ghcr.io/tonylturner/rangerdanger-*') {
        $applyRetagCommands.Add("docker image tag $($unchangedSince[$i]) $($unchangedNew[$i])") | Out-Null
    }
}
if ($applyRetagCommands.Count -eq 0) {
    $applyRetagText = "# No unchanged first-party image tags need to be created."
} else {
    $applyRetagText = $applyRetagCommands -join "`n"
}

$applyBlock = @'
set -e
# Set this to the directory containing this delta bundle.
DELTA_DIR="/path/to/delta-__NEW__"
cd ~/rangerdanger
test -f .env || { echo "Expected .env from setup.sh; cannot preserve the prior version." >&2; exit 1; }

# Only apply a delta to the version it was built from. Check before stopping
# services or touching the install so a wrong or repeated delta is harmless.
CURRENT_VERSION=$(awk '/^VERSION=/ { sub(/^VERSION=/, ""); print; exit }' .env)
if [ "$CURRENT_VERSION" = "__NEW__" ]; then
    echo "This delta looks already applied: install VERSION is $CURRENT_VERSION, but this delta expects __SINCE__; nothing was changed." >&2
    exit 1
fi
if [ "$CURRENT_VERSION" != "__SINCE__" ]; then
    if [ -n "$CURRENT_VERSION" ]; then
        echo "Install VERSION is $CURRENT_VERSION; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    else
        echo "Install .env has no VERSION= line; this delta expects __SINCE__. Refusing to apply; nothing was changed." >&2
    fi
    exit 1
fi

# Stop services before reading their databases and other mutable state into
# the rollback snapshot.
if ! docker compose -f docker-compose.release.yml -f docker-compose.offline.yml down
then
    echo "Could not stop the release + offline stack; some services may be stopped, but no snapshot or repo changes were made. Once Docker is available, run 'docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d' to restore the unchanged install." >&2
    exit 1
fi

# Save the complete existing install, including .env and local lab/policy
# edits, for rollback. Keep the first snapshot if this delta is re-applied.
SNAPSHOT="../rangerdanger.before-__NEW__.tar.gz"
if [ ! -f "$SNAPSHOT" ]; then
    tar czf "$SNAPSHOT" -C .. rangerdanger || {
        rm -f "$SNAPSHOT"
        echo "Could not snapshot ~/rangerdanger; refusing to apply the delta. The release stack is stopped; run 'docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d' to restore the unchanged install." >&2
        exit 1
    }
fi
tar tzf "$SNAPSHOT" >/dev/null || {
    echo "Rollback snapshot is not a readable tar archive; refusing to apply the delta. The release stack is stopped; run 'docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d' to restore the unchanged install." >&2
    exit 1
}

# Update the repo, then load the changed images (if any).
tar xzf "$DELTA_DIR/rangerdanger.tgz" -C ~
__APPLY_BACKUP__
__APPLY_LOAD__

# Re-tag unchanged first-party images so every required :__NEW__ tag exists.
__APPLY_RETAG__

# Select the new release while preserving other .env settings.
NEW_VERSION=__NEW__ awk '
  BEGIN { version = ENVIRON["NEW_VERSION"]; replaced = 0 }
  /^VERSION=/ {
    if (!replaced) print "VERSION=" version
    replaced = 1
    next
  }
  { print }
  END { if (!replaced) print "VERSION=" version }
' .env > .env.delta.tmp && mv .env.delta.tmp .env

# Start the complete stack from the new version without contacting GHCR.
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
'@
$applyBlock = $applyBlock.Replace('__SINCE__', $Since).
    Replace('__NEW__', $New).
    Replace('__APPLY_BACKUP__', $applyBackupText).
    Replace('__APPLY_LOAD__', $applyLoadCommand).
    Replace('__APPLY_RETAG__', $applyRetagText)

$rollbackBlock = @'
set -e
cd ~/rangerdanger
test -f "../rangerdanger.before-__NEW__.tar.gz" || {
    echo "Rollback snapshot not found beside ~/rangerdanger." >&2
    exit 1
}
tar tzf "../rangerdanger.before-__NEW__.tar.gz" >/dev/null || {
    echo "Rollback snapshot is not readable; leaving the current install untouched." >&2
    exit 1
}
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml down
cd ..
rm -rf rangerdanger
tar xzf "rangerdanger.before-__NEW__.tar.gz"
cd rangerdanger
__ROLLBACK_RESTORE__
docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d
'@
$rollbackBlock = $rollbackBlock.Replace('__NEW__', $New).
    Replace('__ROLLBACK_RESTORE__', $rollbackRestoreText)

$readme = @"
# RangerDanger - delta patch

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
$applyBlock
``````

**ARM64 Linux only:** OpenPLC needs amd64 emulation. When changed images
are included, ``delta-arm64.tar`` also ships ``$BINFMT_IMAGE``; if
OpenPLC isn't running after the restart (``docker ps | grep openplc``),
register it once with
``docker run --privileged --rm $BINFMT_IMAGE --install amd64``.
(setup.sh does this automatically on a fresh install; the registration
does not persist across a host reboot.) A repo-only delta has no image
archives, so it cannot supply the binfmt image. Make sure that image is
already present before applying a repo-only delta offline.

If ``docker load`` fails with "no space left on device", free space
without removing the prior ``$Since`` image tags or parked mutable-image
tags named ``:before-$New``; removing an old or parked tag forfeits rollback
for that image. If any apply step after the stack is stopped fails, do not
try to start a partially updated tree: keep the snapshot and old tags, then
follow the ``Rollback`` section below.

## Rollback

The apply recipe saves the complete pre-upgrade ``~/rangerdanger`` tree
beside the install as ``../rangerdanger.before-$New.tar.gz``. That snapshot
includes all files and directories in the install tree: ``.env``, Compose
files, lab definitions, policy files, local edits, and all of ``./data/``
(including captures, Kali home, and simulator state; nothing in ``./data/``
is excluded). Docker images are not part of it. It can be large and grows
with lab state.
The retained ``$Since`` image tags and the parked mutable-image tags named
``:before-$New`` are reused, so rollback needs no network and no second
bundle. Keep the snapshot, retained old image tags, and parked tags until
the rollback window closes:

``````sh
$rollbackBlock
``````
"@
# Write LF and no BOM. This file carries a /bin/sh recipe the student runs
# on Linux or macOS, and a Windows checkout gives the here-strings above
# CRLF, which `sh` reads as part of each value it compares.
[System.IO.File]::WriteAllText(
    (Join-Path $OutDir "DELTA-README.md"),
    ($readme -replace "`r`n", "`n"),
    (New-Object System.Text.UTF8Encoding $false))
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
