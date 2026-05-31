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
Run pre-flight checks (Docker, Compose, ports, disk, memory) and exit
without installing. Useful for a pre-workshop "is my laptop ready?"
check.

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
# Note: docker info can emit harmless warnings to stderr (e.g.
# "WARNING: No blkio throttle.read_bps_device support" on WSL2 +
# cgroups v1). Under Windows PowerShell 5.1 + $ErrorActionPreference =
# "Stop", any native-exe stderr is wrapped as a NativeCommandError and
# throws -- even with `2>$null` or `*>$null`, because Stop intercepts
# before the redirect is fully applied. Lower ErrorAction in a child
# scope so the warning is genuinely discarded, then trust $LASTEXITCODE.
& { $ErrorActionPreference = 'SilentlyContinue'; docker info *>$null }
if ($LASTEXITCODE -ne 0) { Die "Docker is not running or not installed. Start Docker Desktop, then re-run." }
Say "Docker reachable"

# Compose v2
$composeVer = (docker compose version --short 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $composeVer) {
    Die "Docker Compose v2 not found (need the 'docker compose' subcommand, not 'docker-compose')."
}
Say "Compose v2 present ($composeVer)"

# Architecture
$archRaw = (Get-WmiObject Win32_Processor -ErrorAction SilentlyContinue).Architecture
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
# Same NativeCommandError trap as above -- run in a child scope with
# ErrorAction relaxed so any benign stderr warning (blkio etc.) does not
# blow up parsing of the MemTotal field.
$memRaw = & { $ErrorActionPreference = 'SilentlyContinue'; docker info --format '{{.MemTotal}}' 2>$null }
$memBytes = if ($memRaw) { [int64]$memRaw } else { 0 }
if ($memBytes -gt 0) {
    $memGB = [math]::Round($memBytes / 1GB)
    if ($memGB -lt 7) {
        Warn "Docker is configured with $memGB GB RAM -- recommend >= 8 GB. Settings -> Resources in Docker Desktop."
    } else {
        Say "Docker memory: $memGB GB"
    }
}

# Required ports -- bind a TcpListener briefly to confirm free, and
# look up the holding process when one is busy so the user doesn't
# have to dig with netstat.
$portsBusy = @()
$portDetails = @()
foreach ($port in 8088, 9080, 9443, 2222) {
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $port)
        $listener.Start()
        $listener.Stop()
    } catch {
        $portsBusy += $port
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
if ($portsBusy.Count -gt 0) {
    $msg = "Required loopback ports already in use: " + ($portsBusy -join ", ")
    if ($portDetails.Count -gt 0) {
        $msg += "`n" + ($portDetails -join "`n")
    }
    $msg += "`n  Stop whatever is bound to them, then re-run. (kill the PID above, or"
    $msg += "`n  bring down a competing dev stack.)"
    Die $msg
}
Say "Loopback ports 8088, 9080, 9443, 2222 are free"

# --- WSL2 kernel feature probe (Windows + WSL2 backend only) ---------
# Microsoft's stock WSL2 kernel does not enable CONFIG_NFT_QUEUE, which
# silently breaks the ICS DPI rules in Lab 2.3 / 2.3-bonus. Probe here
# (cheap; just runs `nft ... queue num` inside a Linux container) so
# the user knows BEFORE we pull 6 GB of images. The actual install
# step runs after the -CheckOnly short-circuit so we don't modify
# .wslconfig in a probe-only invocation.
$kernelNeedsFix = $false
$kernelInstaller = Join-Path $RootDir "scripts\install-wsl-kernel.ps1"
if ($SkipKernelFix) {
    Say "Skipping WSL2 kernel feature probe (-SkipKernelFix)"
} elseif (-not (Test-Path $kernelInstaller)) {
    Warn "scripts\install-wsl-kernel.ps1 not found -- skipping WSL2 kernel probe (older release?)."
} else {
    # The probe is verbose; suppress its banner output by piping through
    # a temp file so the setup.ps1 log stays clean. Exit codes:
    #   0  = kernel already good
    #   1  = kernel needs install
    #   2  = not on Windows / not WSL2 backend (skip silently)
    & $kernelInstaller -Test 2>&1 | Out-Null
    switch ($LASTEXITCODE) {
        0 { Say "WSL2 kernel: CONFIG_NFT_QUEUE present (ICS DPI labs will work)" }
        2 { Say "WSL2 kernel: probe skipped (not Windows/WSL2 backend)" }
        default {
            Warn "WSL2 kernel: CONFIG_NFT_QUEUE missing -- ICS DPI labs (2.3, 2.3-bonus) will not enforce."
            Warn "  This is a known limitation of the stock WSL2 kernel. setup.ps1 will install"
            Warn "  a prebuilt rangerdanger kernel after these pre-flight checks complete."
            Warn "  Pass -SkipKernelFix to skip and run with stock kernel anyway."
            $kernelNeedsFix = $true
        }
    }
}

# --- check-only short-circuit ---------------------------------------
if ($CheckOnly) {
    Banner "Pre-flight passed -- laptop is ready"
    @"
  All checks above passed. To install:

    .\setup.ps1                       # latest
    .\setup.ps1 -Version v0.1.0       # pinned release

  For offline / SSD install, use -FromTarballs <PATH>.
"@ | Write-Host
    if ($kernelNeedsFix) {
        Write-Host ""
        Warn "Note: the WSL2 kernel is missing CONFIG_NFT_QUEUE. Setup will offer to install"
        Warn "a prebuilt fix when you run without -CheckOnly. Pass -SkipKernelFix to skip."
    }
    exit 0
}

# --- WSL2 kernel install (if probe flagged it) ----------------------
# Runs BEFORE image acquisition so wsl --shutdown does not kill an
# in-progress pull. For -FromTarballs offline installs we look for a
# bundled kernel binary in the tarball directory; for online installs
# we download from the release matching $Version.
if ($kernelNeedsFix -and -not $SkipKernelFix) {
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
            if (Test-Path $bundledSha) {
                $shaContent = (Get-Content $bundledSha -Raw)
                $installerArgs['ExpectedSha256'] = ($shaContent -split '\s+', 2)[0].Trim()
            }
            Say "Using bundled kernel from tarball: $bundledKernel"
        } else {
            Warn "No rangerdanger-wsl2-kernel found in $FromTarballs -- will attempt download."
            Warn "(For fully-offline installs, stage the kernel alongside the image tarballs."
            Warn " See wsl-kernel/README.md.)"
        }
    }
    if (-not $installerArgs.ContainsKey('KernelPath')) {
        $installerArgs['ReleaseTag'] = if ($Version -eq 'latest') { 'latest' } else { $Version }
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
        0  { Say "WSL2 kernel installed; continuing with image acquisition." }
        2  { Say "Kernel installer skipped (not Windows/WSL2). Continuing." }
        10 { Die "User declined kernel install. Re-run with -SkipKernelFix to bypass." }
        11 { Die "Foreign kernel= already in .wslconfig. Re-run with -SkipKernelFix to bypass, or see install-wsl-kernel.ps1 -Force." }
        12 { Die "Kernel download or verification failed. See errors above." }
        default { Die "Kernel install failed with exit $LASTEXITCODE. See errors above. Pass -SkipKernelFix to bypass." }
    }
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

# --- pin VERSION in .env so bare `docker compose` works after install --
# Without this, a student who runs `.\setup.ps1 -FromTarballs <SSD>` and
# later wants to run `docker compose -f docker-compose.release.yml
# -f docker-compose.offline.yml up -d` directly will hit
# `No such image: ...:latest` because compose interpolates
# `${VERSION:-latest}` and the SSD tarball is tagged :vX.Y.Z.
# Writing the resolved Version to .env (compose auto-loads .env from
# cwd) makes the bare compose invocation work the same as setup did.
# .env is gitignored. Idempotent: replaces an existing VERSION= line,
# appends if absent.
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
Say "Waiting for the backend to come up (up to 60 s)..."
$healthUrl = "http://localhost:8088/api/health"
$ok = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $r = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
        if ($r.StatusCode -eq 200) { $ok = $true; break }
    } catch { Start-Sleep 2 }
}
if ($ok) {
    Say "Backend reports healthy at $healthUrl"
} else {
    Warn "Backend didn't report healthy in 60 s. Check 'docker compose -f docker-compose.release.yml logs backend'."
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
