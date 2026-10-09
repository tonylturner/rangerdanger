<#
.SYNOPSIS
RangerDanger lab installer -- Windows (PowerShell 5+ / 7+).

.DESCRIPTION
Validates prerequisites, brings the lab up via docker-compose.release.yml,
and prints next steps.

.PARAMETER Version
Image tag to pull (default: 'latest'). Pin to e.g. 'v0.1.0' for a release.

.PARAMETER FromTarballs
Path to a directory containing 'images-amd64.tar' (or 'images-arm64.tar'
for ARM Windows). Used for offline / SSD installs.

.PARAMETER CheckOnly
Run pre-flight checks (Docker, Compose, ports, disk, memory, WSL2 kernel)
and exit without installing. Useful for a pre-workshop "is my laptop
ready?" check, before or after installing: ports held by an
already-running RangerDanger lab pass.

.EXAMPLE
.\setup.ps1
.\setup.ps1 -Version v0.1.0
.\setup.ps1 -FromTarballs D:\WORKSHOP
.\setup.ps1 -CheckOnly

.NOTES
Run from the repo root (or alongside docker-compose.release.yml plus the
release tarball contents). On Windows this is intended for Docker Desktop
with the WSL2 backend; Hyper-V backend should also work but isn't tested.
#>

[CmdletBinding()]
param(
    [string]$Version = "latest",
    [string]$FromTarballs = "",
    [switch]$CheckOnly,
    [switch]$SkipFirewallGate,
    [switch]$SkipKernelFix
)

$ErrorActionPreference = "Stop"

function Say($msg)    { Write-Host "[+] $msg" -ForegroundColor Green }
function Warn($msg)   { Write-Host "[!] $msg" -ForegroundColor Yellow }
function Die($msg)    { Write-Host "[x] $msg" -ForegroundColor Red; exit 1 }
function Banner($msg) {
    Write-Host ""
    Write-Host $msg -ForegroundColor Cyan
    Write-Host ('-' * $msg.Length) -ForegroundColor Cyan
    Write-Host ""
}

$RootDir        = Split-Path -Parent $MyInvocation.MyCommand.Path
$ComposeFile    = Join-Path $RootDir "docker-compose.release.yml"
$OfflineOverlay = Join-Path $RootDir "docker-compose.offline.yml"

# Compose argument array used by every `docker compose ...` invocation
# below. Offline (-FromTarballs) installs add the offline overlay so
# `pull_policy: never` overrides the release file's `pull_policy: always`,
# preventing GHCR fetches in network-blocked classroom environments.
$ComposeArgs = @("-f", $ComposeFile)
if ($FromTarballs) {
    if (-not (Test-Path $OfflineOverlay)) {
        Die "$OfflineOverlay not found -- required for -FromTarballs."
    }
    $ComposeArgs += @("-f", $OfflineOverlay)
}

# --- pre-flight checks ----------------------------------------------
Banner "Pre-flight checks"

if (-not (Test-Path $ComposeFile)) {
    Die "$ComposeFile not found -- run from repo root or release tarball."
}

# Docker engine.
# Every docker call that talks to the daemon goes through the bounded
# helpers in scripts\lib\docker-bounded.ps1 (shared with
# scripts\install-wsl-kernel.ps1): when Docker Desktop's VM has died (e.g.
# after a `wsl --shutdown`) and it is sitting on its Restart/Quit error
# dialog, `docker info` and friends block forever instead of failing.
. (Join-Path (Join-Path (Join-Path $PSScriptRoot 'scripts') 'lib') 'docker-bounded.ps1')

$dockerExe = Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $dockerExe) { Die "Docker is not installed (docker CLI not on PATH). Install Docker Desktop, then re-run." }
$dockerInfo = Invoke-DockerBounded @('info') 30
if ($dockerInfo.TimedOut) {
    Die "Docker is not responding ('docker info' timed out after 30 s). If Docker Desktop shows an error dialog, click Restart; otherwise quit and restart Docker Desktop. Wait for 'Engine running', then re-run."
}
if ($dockerInfo.ExitCode -ne 0) { Die "Docker is not running or not installed. Start Docker Desktop, then re-run." }
Say "Docker reachable"

# Compose v2
$composeVer = (docker compose version --short 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $composeVer) {
    Die "Docker Compose v2 not found (need the 'docker compose' subcommand, not 'docker-compose')."
}
Say "Compose v2 present ($composeVer)"

# Architecture. Get-CimInstance, not Get-WmiObject: PowerShell 7 removed
# Get-WmiObject, and calling it there is a terminating error under
# $ErrorActionPreference = "Stop". Any CIM failure falls back to the
# environment variable below.
$archRaw = $null
try {
    $archRaw = (Get-CimInstance Win32_Processor -ErrorAction Stop | Select-Object -First 1).Architecture
} catch { }
switch ($archRaw) {
    9   { $arch = "amd64" }      # x64
    12  { $arch = "arm64" }      # ARM64
    default {
        # Fallback via $env:PROCESSOR_ARCHITECTURE
        if ($env:PROCESSOR_ARCHITECTURE -match "ARM64") { $arch = "arm64" }
        elseif ($env:PROCESSOR_ARCHITECTURE -match "AMD64|X86") { $arch = "amd64" }
        else { Die "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
    }
}
Say "Architecture: linux/$arch"

# Free disk on the volume hosting the script
$drive = (Get-Item $RootDir).PSDrive
$freeGB = [math]::Round($drive.Free / 1GB)
if ($freeGB -lt 30) {
    Warn "Only $freeGB GB free on $($drive.Name): -- recommend >= 30 GB. Pull may fail mid-flight."
} else {
    Say "Free disk: $freeGB GB"
}

# Docker memory (Docker Desktop reports its allocation via 'docker info').
$memRun = Invoke-DockerBounded @('info', '--format', '{{.MemTotal}}') 30
$memBytes = [int64]0
if ($memRun -and $memRun.ExitCode -eq 0) { $null = [int64]::TryParse($memRun.StdOut.Trim(), [ref]$memBytes) }
if ($memBytes -gt 0) {
    $memGB = [math]::Round($memBytes / 1GB)
    if ($memGB -lt 7) {
        Warn "Docker is configured with $memGB GB RAM -- recommend >= 8 GB. Settings -> Resources in Docker Desktop."
    } else {
        Say "Docker memory: $memGB GB"
    }
}

# Host ports published by running containers of the installed lab: Compose
# project "rangerdanger" (docker-compose.release.yml and docker-compose.yml
# both set `name: rangerdanger`). A busy required port in this set is the
# student's own running lab, not a conflict.
function Get-LabHeldPorts {
    $r = Invoke-DockerBounded @('ps', '--filter', 'label=com.docker.compose.project=rangerdanger', '--format', '{{.Ports}}') 15
    if (-not $r -or $r.ExitCode -ne 0) { return @() }
    @([regex]::Matches($r.StdOut, ':(\d+)->') | ForEach-Object { [int]$_.Groups[1].Value } | Sort-Object -Unique)
}

# Required ports -- bind a TcpListener briefly to confirm free. A busy port
# held by the running lab passes -CheckOnly (the "night before" check on an
# installed laptop) and stops an install with how to refresh. Anything else
# holding a port fails both, with the holding process when we can find it.
$portsRequired = @(8088, 9080, 9443, 2222)
$portsBusy = @()
foreach ($port in $portsRequired) {
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $port)
        $listener.Start()
        $listener.Stop()
    } catch {
        $portsBusy += $port
    }
}
$labPorts = @()
$foreignPorts = @()
$portDetails = @()
if ($portsBusy.Count -gt 0) {
    $labHeld = @(Get-LabHeldPorts)
    foreach ($port in $portsBusy) {
        if ($labHeld -contains $port) { $labPorts += $port; continue }
        $foreignPorts += $port
        try {
            $conn = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($conn) {
                $proc = Get-Process -Id $conn.OwningProcess -ErrorAction SilentlyContinue
                $procName = if ($proc) { $proc.ProcessName } else { "?" }
                $portDetails += "    ${port}: $procName (pid=$($conn.OwningProcess))"
            }
        } catch { }
    }
}
if ($foreignPorts.Count -gt 0) {
    $msg = "Required loopback ports already in use: " + ($foreignPorts -join ", ")
    if ($portDetails.Count -gt 0) {
        $msg += "`n" + ($portDetails -join "`n")
    }
    if ($labPorts.Count -gt 0) {
        $msg += "`n  (Ports " + ($labPorts -join ", ") + " are held by your running RangerDanger lab; that is fine.)"
    }
    $msg += "`n  Stop whatever is bound to them, then re-run. (kill the PID above, or"
    $msg += "`n  bring down a competing dev stack.)"
    Die $msg
}
# The explicit release (+ offline) files, as docs/quickstart.md uses them:
# a bare `docker compose` selects the source stack.
$downCmd = "docker compose -f docker-compose.release.yml"
if ($FromTarballs) { $downCmd += " -f docker-compose.offline.yml" }
$downCmd += " down"
$rerunCmd = ".\setup.ps1"
if ($Version -ne "latest") { $rerunCmd += " -Version $Version" }
if ($FromTarballs) { $rerunCmd += " -FromTarballs `"$FromTarballs`"" }
if ($labPorts.Count -gt 0) {
    $labList = $labPorts -join ", "
    if (-not $CheckOnly) {
        Die @"
RangerDanger is already installed and running (it holds loopback ports $labList).
  To check it without reinstalling:  .\setup.ps1 -CheckOnly
  To refresh or reinstall it, stop it first, then re-run setup:
    $downCmd
    $rerunCmd
"@
    }
    Say "RangerDanger is already installed and running on loopback ports $labList"
}
$portsFree = @($portsRequired | Where-Object { $labPorts -notcontains $_ })
if ($portsFree.Count -gt 0) {
    Say ("Free loopback ports: " + ($portsFree -join ", "))
}

# --- WSL2 kernel (Windows + WSL2 backend only) ------------------------
# Microsoft's stock WSL2 kernel does not enable CONFIG_NFT_QUEUE, which
# silently breaks the ICS DPI rules in Lab 2.3 / 2.3-bonus.
# install-wsl-kernel.ps1 -Test probes it with `nft ... queue num` inside a
# Linux container and exits:
#   0  = kernel already good
#   1  = kernel needs install
#   2  = not on Windows / not WSL2 backend (skip silently)
#   13 = no probe could run, so support is unknown (no running firewall,
#        no local firewall image, and the Alpine fallback had no network)
# An install probes only after it has pulled or loaded the images, so the
# probe runs in the local containd firewall image with no network and 13
# there is a real fault. -CheckOnly probes up front, before any image may
# exist locally, so 13 there is expected on an offline laptop. The install
# step never runs under -CheckOnly, so a probe-only invocation does not
# modify .wslconfig.
$kernelNeedsFix = $false
$kernelProbeEnabled = $false
$kernelInstaller = Join-Path $RootDir "scripts\install-wsl-kernel.ps1"
if ($SkipKernelFix) {
    Say "Skipping WSL2 kernel feature probe (-SkipKernelFix)"
} elseif (-not (Test-Path $kernelInstaller)) {
    Warn "scripts\install-wsl-kernel.ps1 not found -- skipping WSL2 kernel probe (older release?)."
} else {
    $kernelProbeEnabled = $true
}

function Get-KernelProbeResult {
    # Only the exit code matters here; the probe's own lines still show.
    & $kernelInstaller -Test 2>&1 | Out-Null
    return $LASTEXITCODE
}

function Write-KernelMissing {
    Warn "WSL2 kernel: CONFIG_NFT_QUEUE missing -- ICS DPI labs (2.3, 2.3-bonus) will not enforce."
    Warn "  This is a known limitation of the stock WSL2 kernel."
}

# Install the prebuilt lab kernel. For -FromTarballs installs we look for a
# bundled kernel binary in the tarball directory; otherwise (and when the
# bundle has none) we download from the release matching $Version, which
# for -FromTarballs is the version already read from the SSD's .version.
function Install-LabKernel {
    Banner "Installing WSL2 kernel"
    # Splat as a HASHTABLE, not an array. PowerShell array-splatting does not
    # reliably bind "-Name value" pairs to a called script's parameters: e.g.
    # @("-ReleaseTag","latest") binds the literal "-ReleaseTag" positionally
    # into the first string param ($KernelPath), so install-wsl-kernel.ps1 died
    # with `-KernelPath does not exist: -ReleaseTag` and the kernel never
    # installed -- the online AND -FromTarballs paths both hit this. A
    # hashtable binds by name every time.
    $installerArgs = @{}
    if ($FromTarballs) {
        $bundledKernel = Join-Path $FromTarballs "rangerdanger-wsl2-kernel"
        if (Test-Path $bundledKernel) {
            $installerArgs['KernelPath'] = $bundledKernel
            $bundledSha = Join-Path $FromTarballs "rangerdanger-wsl2-kernel.sha256"
            $checksumRecovery = "Bundle was staged without a usable checksum file ($bundledSha). Re-stage the SSD, or use -SkipKernelFix to continue without the kernel and lose ICS DPI on Labs 2.3 / 2.3-bonus."
            if (-not (Test-Path $bundledSha)) {
                Die $checksumRecovery
            }
            try {
                $shaContent = Get-Content -Path $bundledSha -Raw -ErrorAction Stop
            } catch {
                Die "$checksumRecovery Could not read the checksum: $_"
            }
            $expectedSha = ($shaContent.Trim() -split '\s+', 2)[0]
            if ($expectedSha -notmatch '^[0-9a-fA-F]{64}$') {
                Die $checksumRecovery
            }
            $installerArgs['ExpectedSha256'] = $expectedSha
            Say "Using bundled kernel from tarball: $bundledKernel"
        } else {
            Warn "No rangerdanger-wsl2-kernel found in $FromTarballs -- will attempt to download it for $Version."
            Warn "(For fully-offline installs, stage the kernel alongside the image tarballs."
            Warn " See wsl-kernel/README.md.)"
        }
    }
    if (-not $installerArgs.ContainsKey('KernelPath')) {
        $installerArgs['ReleaseTag'] = $Version
    }
    # Install unattended. Running setup.ps1 is already the user's go-ahead to
    # bring the lab up, and the kernel step is required for the DPI labs, so
    # forward -Yes rather than blocking on the installer's [y/N] prompt -- a
    # single `.\setup.ps1` shouldn't need a second command or a babysat prompt
    # to finish. The installer still prints its "About to: ... wsl --shutdown"
    # banner first, so the brief Docker restart isn't a surprise. Pass
    # -SkipKernelFix to skip the kernel entirely.
    $installerArgs['Yes'] = $true
    & $kernelInstaller @installerArgs
    switch ($LASTEXITCODE) {
        0  { Say "WSL2 kernel installed; continuing." }
        2  { Say "Kernel installer skipped (not Windows/WSL2). Continuing." }
        10 { Die "User declined kernel install. Re-run with -SkipKernelFix to bypass." }
        11 { Die "Foreign kernel= already in .wslconfig. Re-run with -SkipKernelFix to bypass, or see install-wsl-kernel.ps1 -Force." }
        12 { Die "Kernel download or verification failed. See errors above." }
        13 { Die "The kernel installer could not run its nft probe (see above). Fix Docker, then re-run setup; pass -SkipKernelFix to bypass." }
        default { Die "Kernel install failed with exit $LASTEXITCODE. See errors above. Pass -SkipKernelFix to bypass." }
    }
}

# Install mode: probe, and install the lab kernel when it is missing. Runs
# after image acquisition, so the firewall image is local and the probe
# needs no network: "unknown" here means the probe itself broke.
function Invoke-KernelStep {
    if (-not $kernelProbeEnabled) { return }
    switch (Get-KernelProbeResult) {
        0  { Say "WSL2 kernel: CONFIG_NFT_QUEUE present (ICS DPI labs will work)" }
        2  { Say "WSL2 kernel: probe skipped (not Windows/WSL2 backend)" }
        13 { Die "WSL2 kernel: the nft probe could not run even with the lab images present (see above). Fix Docker, then re-run setup; pass -SkipKernelFix to bypass." }
        default {
            Write-KernelMissing
            Install-LabKernel
        }
    }
}

# --- check-only short-circuit ---------------------------------------
if ($CheckOnly) {
    if ($kernelProbeEnabled) {
        switch (Get-KernelProbeResult) {
            0  { Say "WSL2 kernel: CONFIG_NFT_QUEUE present (ICS DPI labs will work)" }
            2  { Say "WSL2 kernel: probe skipped (not Windows/WSL2 backend)" }
            13 {
                if ($FromTarballs) {
                    Warn "WSL2 kernel: not probed yet. The firewall image is not loaded and the"
                    Warn "  Alpine fallback needs network. setup.ps1 -FromTarballs probes the kernel"
                    Warn "  after it loads the SSD images, and installs the bundled kernel if needed."
                } else {
                    Warn "WSL2 kernel: the nft probe could not run (no local firewall image, and the"
                    Warn "  Alpine fallback needs network), so CONFIG_NFT_QUEUE support is unknown."
                    Warn "  setup.ps1 probes the kernel after it pulls the images, and installs the"
                    Warn "  lab kernel if needed."
                }
            }
            default {
                Write-KernelMissing
                $kernelNeedsFix = $true
            }
        }
    }
    if ($labPorts.Count -gt 0) {
        Banner "Pre-flight passed -- RangerDanger is already installed and running"
        @"
  Open http://localhost:8088. To refresh or reinstall it, stop it first,
  then re-run setup:

    $downCmd
    $rerunCmd
"@ | Write-Host
    } else {
        Banner "Pre-flight passed -- laptop is ready"
        @"
  All checks above passed. To install:

    .\setup.ps1                       # latest
    .\setup.ps1 -Version v0.1.0       # pinned release

  For offline / SSD install, use -FromTarballs <PATH>.
"@ | Write-Host
    }
    if ($kernelNeedsFix) {
        Write-Host ""
        Warn "Note: the WSL2 kernel is missing CONFIG_NFT_QUEUE. Setup will install"
        Warn "a prebuilt fix when you run without -CheckOnly. Pass -SkipKernelFix to skip."
    }
    exit 0
}

# --- image acquisition ----------------------------------------------
if ($FromTarballs) {
    Banner "Loading images from tarballs"
    if (-not (Test-Path $FromTarballs)) { Die "Tarball directory not found: $FromTarballs" }

    # Auto-detect the staged version from .version file written by
    # stage-ssd.sh. Without this, $Version stays at "latest" and
    # compose looks for `:latest` tags that don't exist on the SSD's
    # `:vX.Y.Z`-tagged images, failing with "No such image: ...:latest".
    $versionFile = Join-Path $FromTarballs ".version"
    if ((Test-Path $versionFile) -and ($Version -eq "latest")) {
        $Version = (Get-Content $versionFile -Raw).Trim()
        Say "Auto-detected version from SSD: $Version"
    }

    $tarball = Join-Path $FromTarballs "images-$arch.tar"
    if (-not (Test-Path $tarball)) { Die "Expected $tarball (this host is $arch). Have you staged the right architecture?" }
    $sizeMB = [math]::Round((Get-Item $tarball).Length / 1MB)
    Say "Loading $tarball (${sizeMB} MB) - decompressing each image, ~5-15 min on a fast SSD."
    Say "Watch the 'Loaded image:' lines below - one per image, 14-19 total."
    docker load -i $tarball
    if ($LASTEXITCODE -ne 0) { Die "docker load failed for $tarball -- see the error above." }
    Say "Images loaded"
} else {
    Banner "Pulling images from GHCR"
    Say "Version: $Version"
    Say "(this can take a while on first run; subsequent pulls are layer-cached)"
    $env:VERSION = $Version
    # GHCR occasionally returns transient 5xx during layer fetches --
    # retry up to 3 times with exponential backoff before giving up.
    $pullOk = $false
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        docker compose @ComposeArgs pull
        if ($LASTEXITCODE -eq 0) { $pullOk = $true; break }
        if ($attempt -lt 3) {
            $waitS = $attempt * 15
            Warn "Pull attempt $attempt failed (likely a transient GHCR / network blip). Retrying in ${waitS}s..."
            Start-Sleep -Seconds $waitS
        }
    }
    if (-not $pullOk) {
        Die @"
Pulling images failed after 3 attempts. Common causes:
    - Network blocks ghcr.io (try the offline path: -FromTarballs <PATH>)
    - GHCR is genuinely down (rare; check https://www.githubstatus.com/)
    - Disk filled mid-pull
"@
    }
}

# --- WSL2 kernel probe + install --------------------------------------
# Both paths probe only now, with the images local: the probe runs in the
# containd firewall image just pulled or loaded, with no network, and the
# kernel release tag is the resolved $Version (for -FromTarballs, the SSD's
# .version read above). The pull has finished, so the installer's
# wsl --shutdown cannot interrupt it; pulled and loaded images sit on
# Docker Desktop's persistent data disk, which wsl --shutdown keeps.
Invoke-KernelStep

# --- pin VERSION in .env for later release-file compose commands ----
# Without this, a student who runs `.\setup.ps1 -FromTarballs <SSD>` and
# later runs `docker compose -f docker-compose.release.yml
# -f docker-compose.offline.yml up -d` directly will hit
# `No such image: ...:latest` because compose interpolates
# `${VERSION:-latest}` and the SSD tarball is tagged :vX.Y.Z.
# Compose auto-loads .env from the project directory (the repo root), so
# writing the resolved Version there makes those explicit release-file
# commands use the tag setup installed. A bare `docker compose` with no -f
# still selects the source stack (docker-compose.yml), not the release
# one; see docs/quickstart.md. .env is gitignored. Idempotent: replaces an
# existing VERSION= line, appends if absent.
$envFile = Join-Path $RootDir ".env"
if (Test-Path $envFile) {
    $content = Get-Content $envFile -Raw
    if ($content -match '(?m)^VERSION=') {
        $content = ($content -replace '(?m)^VERSION=.*', "VERSION=$Version")
        Set-Content -Path $envFile -Value $content -NoNewline
    } else {
        Add-Content -Path $envFile -Value "VERSION=$Version"
    }
} else {
    Set-Content -Path $envFile -Value "VERSION=$Version`n"
}
Say "Pinned VERSION=$Version in $envFile"

# --- start the stack ------------------------------------------------
Banner "Starting RangerDanger"
$env:VERSION = $Version
docker compose @ComposeArgs up -d
if ($LASTEXITCODE -ne 0) { Die "compose up failed -- check logs." }
Say "Containers started"

# --- health smoke check ---------------------------------------------
Banner "Health check"
Say "Waiting for the backend to come up (up to 120 s)..."
$healthUrl = "http://localhost:8088/api/health"
$ok = $false
# ~2 s per iteration (Start-Sleep 2 on a refused connection); 60 iterations
# gives a 120 s budget. Cold-start after a fresh image pull on Windows /
# WSL2 routinely takes 60-90 s, which tripped the old 30-iteration (60 s)
# budget into a scary "didn't report healthy" warning even though the
# readiness gate right below then passed.
for ($i = 0; $i -lt 60; $i++) {
    try {
        $r = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
        if ($r.StatusCode -eq 200) { $ok = $true; break }
    } catch { Start-Sleep 2 }
}
if ($ok) {
    Say "Backend reports healthy at $healthUrl"
} else {
    Warn "Backend didn't report healthy in 120 s. Check 'docker compose -f docker-compose.release.yml logs backend'."
}

# Workshop-critical surfaces. /api/health alone returned green in past
# audits while firewall apply/reset were broken -- students hit the
# regression at lab-time, not at setup. Gate explicitly here so a
# half-working stack never makes it past the installer banner.
# Use -SkipFirewallGate for developer iteration on a known-broken stack.
if ($SkipFirewallGate) {
    Say "Skipping firewall workshop-readiness gate (-SkipFirewallGate)"
} else {
    Say "Workshop-readiness gate: firewall health + apply + reset..."
    $fwFail = $false
    $dpiDegraded = $false
    try {
        Invoke-WebRequest -Uri "http://localhost:8088/api/firewall/health" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop | Out-Null
    } catch {
        Warn "  /api/firewall/health failed -- containd management interface is down"
        $fwFail = $true
    }
    foreach ($cfg in @("weak", "improved")) {
        try {
            $body = @{ config = $cfg } | ConvertTo-Json -Compress
            $applyResp = Invoke-WebRequest -Uri "http://localhost:8088/api/firewall/apply" -Method POST -ContentType "application/json" -Body $body -UseBasicParsing -TimeoutSec 10 -ErrorAction Stop
            # A 200 with warnings means containd committed the policy but the
            # kernel rejected part of the ruleset. On Windows/WSL2 this is the
            # CONFIG_NFT_QUEUE-missing case: the "queue num" DPI rules fail and,
            # nft being atomic, the WHOLE hardened ruleset rolls back -- yet the
            # apply still returns 200, so a gate that pipes the body to Out-Null
            # passes green while segmentation silently does not enforce. Inspect
            # the body instead of swallowing it.
            if ($cfg -eq "improved" -and $applyResp.Content -match 'nft apply failed|queue num|NFT_QUEUE') {
                $dpiDegraded = $true
            }
        } catch {
            Warn "  /api/firewall/apply ($cfg) failed -- Lab 2.2/2.3/2.3-bonus/2.4 will not work"
            $fwFail = $true
        }
    }
    try {
        $resetResp = Invoke-WebRequest -Uri "http://localhost:8088/api/workshop/reset" -Method POST -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
        # Check the TOP-LEVEL success, not a substring. The response embeds a
        # per-action array whose entries each carry their own "success":true,
        # so a naive -match '"success":true' false-passes even when the overall
        # reset failed (a sim sub-command returned success:false). Parse and
        # read the top-level field, matching test-lifecycle.ps1.
        $resetOk = $false
        try { $resetOk = ((($resetResp.Content | ConvertFrom-Json).success) -eq $true) } catch { $resetOk = $false }
        if (-not $resetOk) {
            Warn "  /api/workshop/reset reported non-success -- students hitting Reset Lab will see partial state"
            $fwFail = $true
        }
    } catch {
        Warn "  /api/workshop/reset failed: $_"
        $fwFail = $true
    }
    # Loud guard for the silent hardened-policy failure. Every call above can
    # return HTTP 200 while the hardened policy does not actually engage,
    # because a kernel missing CONFIG_NFT_QUEUE makes nft reject the ruleset.
    # Surface it unmissably with the exact fix rather than leaving the student
    # on a stack where applying the hardened policy is a silent no-op. Non-fatal
    # (a -SkipKernelFix user opted into L4-only), but impossible to miss.
    if ($dpiDegraded) {
        Warn "------------------------------------------------------------"
        Warn "Hardened policy applied WITH WARNINGS -- ICS DPI / segmentation"
        Warn "is NOT enforcing. Labs 2.3 / 2.3-bonus and the hardened L4 rules"
        Warn "will silently fail to block the attacks they teach."
        Warn "Cause (on Windows): the WSL2 kernel is missing CONFIG_NFT_QUEUE."
        Warn "Fix (downloads + sha256-verifies the prebuilt kernel, ~3 min):"
        Warn "    .\scripts\install-wsl-kernel.ps1"
        Warn "Verify:  .\scripts\firewall-smoke.ps1   (expect 52/52)"
        Warn "------------------------------------------------------------"
    }
    if ($fwFail) {
        Die @"
Workshop-readiness gate failed. Common causes:
  - containd image drift (bump containd or pin a known-good tag)
  - mgmt subnet not in firewall input chain (CONTAIND_AUTO_LAN3_SUBNET)
  - sims still warming up (re-run setup, or wait 30s and re-probe)
Re-run with -SkipFirewallGate to bring the stack up anyway for diagnosis.
"@
    }
    Say "Firewall apply/reset workshop gate passed"
}

# --- done ------------------------------------------------------------
Banner "RangerDanger is up"
@"
  Web UI:        http://localhost:8088
  Exercises:     http://localhost:8088/exercises
  containd UI:   http://localhost:9080
  containd SSH:  ssh -p 2222 containd@localhost   (password: containd)

To stop:
  docker compose -f docker-compose.release.yml down

To check status:
  docker compose -f docker-compose.release.yml ps

To view logs:
  docker compose -f docker-compose.release.yml logs -f <service>

When you're done with the workshop (removes the stack AND reverts the
custom WSL2 kernel installed above, if any):
  .\scripts\uninstall-rangerdanger.ps1

For the lab security model and how to expose this to other machines
on purpose, see SECURITY.md.
"@ | Write-Host
