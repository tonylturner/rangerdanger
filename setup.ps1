<#
.SYNOPSIS
RangerDanger package installer -- Windows PowerShell 5.1 / 7.
.DESCRIPTION
Acquires every platform/package image before migration. Starts the platform,
then asks the backend to restart the selected or recorded range.
Supports only Docker Desktop WSL2 and a physical local-drive checkout.
.PARAMETER FromSource
Build all platform/package images instead of pulling release images.
.PARAMETER Package
First-start package (default US). Omit to restart the backend's recorded package.
.PARAMETER FromTarballs
Offline directory containing images-<arch>.tar, .version and the WSL2 kernel.
.PARAMETER CheckOnly
Preflight only: no .env writes, image acquisition, root probe or lifecycle changes.
.EXAMPLE
.\setup.ps1 -Package eu-iec104-substation
.\setup.ps1 -FromTarballs D:\WORKSHOP
.\setup.ps1 -FromSource -SkipFirewallGate
.NOTES
ASCII-only, BOM-free for Windows PowerShell 5.1. Hyper-V and UNC/\\wsl$
checkouts are not supported. No Desktop internal path is trusted until the
read-only sentinel probe succeeds.
#>
[CmdletBinding()]
param(
    [string]$Version = $(if ($env:VERSION) { $env:VERSION } else { 'latest' }),
    [string]$FromTarballs = '',
    [switch]$FromSource,
    [string]$Package = '',
    [switch]$CheckOnly,
    [switch]$SkipFirewallGate,
    [switch]$SkipKernelFix
)
$ErrorActionPreference = 'Stop'
function Say($msg) { Write-Host "[+] $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "[!] $msg" -ForegroundColor Yellow }
function Die($msg) { Write-Host "[x] $msg" -ForegroundColor Red; exit 1 }
function Banner($msg) { Write-Host "`n$msg`n$('-' * $msg.Length)`n" -ForegroundColor Cyan }

. "$PSScriptRoot/scripts/lib/docker-bounded.ps1"
. "$PSScriptRoot/scripts/lib/docker-floor.ps1"
. "$PSScriptRoot/scripts/lib/windows-root.ps1"
. "$PSScriptRoot/scripts/lib/setup-package.ps1"
. "$PSScriptRoot/scripts/lib/setup-range.ps1"

trap { Die $_ }
Banner 'Pre-flight checks'
$RootDir = (Get-Item -LiteralPath $PSScriptRoot).FullName
$DaemonRoot = ConvertTo-DesktopRoot $RootDir
Assert-LocalRepoDrive $RootDir
if ($FromSource -and $FromTarballs) { throw '-FromSource and -FromTarballs are exclusive.' }
if ($Version -cnotmatch '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$') { throw 'Version must be a Docker image tag.' }
$Mode = if ($FromSource) { 'source' } elseif ($FromTarballs) { 'offline' } else { 'release' }
$Models = @(Get-SetupModels $RootDir $Mode)
$Platform = $Models[0]
$ids = @($Models | Where-Object { $_.Package } | ForEach-Object { $_.Package })
if ($Package -and ($Package -cnotmatch '^[a-z0-9-]+$' -or $Package -cnotin $ids)) {
    throw "Unknown package '$Package'. Available: $($ids -join ', ')"
}
$info = (Invoke-SetupDocker @('info', '--format', '{{json .}}') 30) | ConvertFrom-Json
Assert-DesktopWsl2 $info
Say "Docker Desktop WSL2 reachable; root: $DaemonRoot (mode: $Mode)"
$composeVer = (Invoke-SetupDocker @('compose', 'version', '--short')).Trim()
$engineApi = (Invoke-SetupDocker @('version', '--format', '{{.Server.APIVersion}}')).Trim()
$floorProblem = Get-DockerFloorProblem $composeVer $engineApi
if ($floorProblem) { Die $floorProblem }
Say "Compose $composeVer on Engine API $engineApi"
$arch = switch ($info.Architecture) {
    { $_ -in @('amd64', 'x86_64') } { 'amd64'; break }
    { $_ -in @('arm64', 'aarch64') } { 'arm64'; break }
    default { throw "Unsupported Docker architecture: $($info.Architecture)" }
}
Say "Architecture: linux/$arch"
$drive = (Get-Item -LiteralPath $RootDir).PSDrive
$freeGB = [math]::Floor($drive.Free / 1GB)
if ($freeGB -lt 30) { Warn "Only $freeGB GB free -- recommend at least 30 GB." }
$memGB = [math]::Floor([int64]$info.MemTotal / 1GB)
if ($memGB -lt 7) { Warn "Docker has $memGB GB RAM -- recommend at least 8 GB." }

# A bounded loopback CONNECT probe distinguishes refused/free from busy
# and unknown; it never claims a port free on an unsupported probe.
$labHeld = @(Get-SetupHeldPorts)
$labPorts = @()
$portsFree = @()
foreach ($port in @(8088, 9080, 9443, 2222)) {
    switch (Get-SetupPortState $port) {
        'free' { $portsFree += $port }
        'busy' {
            if ($labHeld -contains $port) { $labPorts += $port }
            else { throw "Required loopback port $port is held by another process. Stop its owner, then re-run." }
        }
        default { throw "Could not test loopback port $port. Check its listener manually; no install was changed." }
    }
}
if ($labPorts.Count) { Say "Ports held by this install: $($labPorts -join ', ') (rerun restarts the range)" }
if ($portsFree.Count) { Say "Free loopback ports: $($portsFree -join ', ')" }
$ProbeImage = if ($FromSource) { 'rangerdanger-firewall' } else { 'ghcr.io/tonylturner/containd:latest' }
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
# Probe only after acquisition, using the source-built or release
# firewall image. An unknown result then is a fault, not a missing
# kernel feature. CheckOnly never runs this step.
$kernelProbeEnabled = $false
$kernelInstaller = Join-Path $RootDir "scripts\install-wsl-kernel.ps1"
if ($SkipKernelFix) {
    Say "Skipping WSL2 kernel feature probe (-SkipKernelFix)"
} elseif (-not (Test-Path $kernelInstaller)) {
    Die "scripts\install-wsl-kernel.ps1 not found -- the checkout is incomplete."
} else {
    $kernelProbeEnabled = $true
}

function Get-KernelProbeResult {
    # Only the exit code matters here; the probe's own lines still show.
    & $kernelInstaller -Test -ProbeImage $ProbeImage -Offline:($Mode -eq 'offline') 2>&1 | Out-Null
    return $LASTEXITCODE
}

function Write-KernelMissing {
    Warn "WSL2 kernel: CONFIG_NFT_QUEUE missing -- DPI logging rules may fail to load."
    Warn "  This is a known limitation of the stock WSL2 kernel."
}

# Install the prebuilt lab kernel. For -FromTarballs installs we look for a
# bundled kernel binary in the tarball directory. Online installs download
# from the release matching $Version. Offline installs never download.
function Install-LabKernel {
    if ($arch -ne 'amd64') {
        Die "Kernel remediation is unavailable on ARM64: the published WSL2 kernel is x86_64 only. No kernel was installed. Use a host whose kernel already supports nft queue rules."
    }
    Banner "Installing WSL2 kernel"
    # Splat as a HASHTABLE, not an array. PowerShell array-splatting does not
    # reliably bind "-Name value" pairs to a called script's parameters: e.g.
    # @("-ReleaseTag","latest") binds the literal "-ReleaseTag" positionally
    # into the first string param ($KernelPath), so install-wsl-kernel.ps1 died
    # with `-KernelPath does not exist: -ReleaseTag` and the kernel never
    # installed -- the online AND -FromTarballs paths both hit this. A
    # hashtable binds by name every time.
    $installerArgs = @{ ProbeImage=$ProbeImage; Offline=($Mode -eq 'offline') }
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
            Die "No bundled WSL2 kernel in $FromTarballs. Re-stage the kernel and checksum, or use -SkipKernelFix. Offline setup never downloads a kernel."
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
        2  { Die "The kernel installer no longer detects Windows/WSL2. Check Docker Desktop before retrying." }
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
        0  { Say "WSL2 kernel: CONFIG_NFT_QUEUE present" }
        2  { Die "The kernel probe no longer detects Windows/WSL2. Check Docker Desktop before retrying." }
        13 { Die "WSL2 kernel: the nft probe could not run even with the lab images present (see above). Fix Docker, then re-run setup; pass -SkipKernelFix to bypass." }
        default {
            Write-KernelMissing
            Install-LabKernel
        }
    }
}


# CheckOnly must not start even a probe container or change the kernel.
if ($CheckOnly) {
    Banner 'Pre-flight passed -- laptop is ready'
    Say 'Root mapping and WSL2 kernel features will be verified after image acquisition during install.'
    exit 0
}

$Tarball = ''
if ($Mode -eq 'offline') {
    $versionFile = Join-Path $FromTarballs '.version'
    if ((Test-Path -LiteralPath $versionFile) -and $Version -eq 'latest') {
        $Version = ([IO.File]::ReadAllText((Resolve-Path -LiteralPath $versionFile).Path)).Trim()
        Say "Auto-detected version from SSD: $Version"
    }
    if ($Version -cnotmatch '^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$') { throw 'Invalid staged image version.' }
    $Tarball = Join-Path $FromTarballs "images-$arch.tar"
    if (-not (Test-Path -LiteralPath $Tarball -PathType Leaf)) { throw "Expected $Tarball for linux/$arch." }
}
# Setup owns these keys; preserve all other .env lines. Explicit process
# values must agree because Compose prefers the environment to .env.
$envFile = Join-Path $RootDir '.env'
Write-SetupEnv $envFile 'VERSION' $Version
Write-SetupEnv $envFile 'RANGERDANGER_ROOT' $DaemonRoot
Write-SetupEnv $envFile 'RANGERDANGER_PACKAGE' $Package
if ($env:CONTAIND_JWT_SECRET) { Write-SetupEnv $envFile 'CONTAIND_JWT_SECRET' $env:CONTAIND_JWT_SECRET }
$env:VERSION = $Version
$env:RANGERDANGER_ROOT = $DaemonRoot
$env:RANGERDANGER_PACKAGE = $Package
$null = New-Item -ItemType Directory -Path (Join-Path $RootDir 'data/proxy-routes') -Force

# Check every network owner before any migration, including delivery
# networks even when the management network belongs to the old project.
$migrate = Assert-SetupNetworkOwners
$oldPlatform = (Invoke-SetupDocker @('ps', '-aq', '--filter', 'label=com.docker.compose.project=rangerdanger',
    '--filter', 'label=com.docker.compose.service=backend') 15).Trim()

Banner 'Acquiring all platform and package images'
Say 'Build/load/pull can take several minutes; output follows each bounded command.'
Invoke-SetupAcquisition $Models $Mode $Tarball
Invoke-KernelStep
# The proxy image is in every platform image union and has /bin/cat;
# derive the exact tag from Compose, not a separately maintained pin.
$platformConfig = (Invoke-SetupDocker (Get-SetupComposeArgs $Platform @('config', '--format', 'json'))) | ConvertFrom-Json
$rootProbeImage = $platformConfig.services.proxy.image
if (-not $rootProbeImage) { throw 'Platform proxy image missing from Compose config.' }
Assert-DesktopRootBind $RootDir $DaemonRoot $rootProbeImage
Say 'Daemon same-path read-only root bind verified'

# Every image and the root proof succeeded before any teardown.
if ($migrate -or $oldPlatform) {
    Banner 'Migrating the pre-split stack'
    & "$PSScriptRoot/scripts/dev-down.ps1" -RangeOnly
    if ($LASTEXITCODE -ne 0) { throw 'Could not remove the pre-split stack. Fix teardown before retrying.' }
}
Banner 'Starting the platform'
$up = @('up', '-d', '--wait', '--wait-timeout', '120')
if ($Mode -eq 'offline') { $up += @('--pull', 'never') }
Write-Host (Invoke-SetupDocker (Get-SetupComposeArgs $Platform $up) 180)
Wait-SetupHealth 'http://localhost:8088'
Banner 'Starting the range'
$RangePackage = Start-SetupRange 'http://localhost:8088' $Package
Say "Range $RangePackage is ready"
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
        Invoke-WebRequest -Uri "http://localhost:8088/api/firewall/health" -UseBasicParsing -TimeoutSec 10 -ErrorAction Stop | Out-Null
    } catch {
        Warn "  /api/firewall/health failed -- containd management interface is down"
        $fwFail = $true
    }
    foreach ($cfg in @("weak", "improved")) {
        try {
            $body = @{ config = $cfg } | ConvertTo-Json -Compress
            $applyResp = Invoke-WebRequest -Uri "http://localhost:8088/api/firewall/apply" -Method POST -ContentType "application/json" -Body $body -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
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
        $resetResp = Invoke-WebRequest -Uri "http://localhost:8088/api/workshop/reset" -Method POST -UseBasicParsing -TimeoutSec 60 -ErrorAction Stop
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
        Warn "Hardened policy applied WITH WARNINGS -- inspect the active rules."
        Warn "The L4 source pin enforces; DPI detects and logs."
        Warn "A rejected atomic ruleset can leave the previous policy active."
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


# OpenPLC is used by both US and EU packages. Like setup.sh this is diagnostic, not a lifecycle gate.
$openplcOk = $false
for ($i = 0; $i -lt 8; $i++) {
    $run = Invoke-DockerBounded @('inspect', '-f', '{{.State.Status}}', 'rangerdanger-openplc') 5
    if ($run -and -not $run.TimedOut -and $run.ExitCode -eq 0 -and $run.StdOut.Trim() -eq 'running') {
        $openplcOk = $true; break
    }
    Start-Sleep -Seconds 2
}
if ($openplcOk) { Say 'OpenPLC container is running' }
else { Warn 'OpenPLC is not running. Check docker logs rangerdanger-openplc.' }
Banner 'RangerDanger is up'
@"
  Web UI:        http://localhost:8088
  Exercises:     http://localhost:8088/exercises
  containd UI:   http://localhost:9080
  containd SSH:  ssh -p 2222 containd@localhost   (password: containd)
  Range:         $RangePackage (switch packages in the web UI)

To stop the range, then the platform:
  .\scripts\dev-down.ps1
To start again: re-run .\setup.ps1 with the same options.
To check status: Invoke-RestMethod http://localhost:8088/api/range
To view logs: docker logs -f rangerdanger-<service>
To uninstall (including the managed WSL2 kernel):
  .\scripts\uninstall-rangerdanger.ps1
See SECURITY.md for the lab security model.
"@ | Write-Host
exit 0
