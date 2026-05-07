<#
.SYNOPSIS
RangerDanger lab installer — Windows (PowerShell 5+ / 7+).

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
    [switch]$CheckOnly
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

$RootDir     = Split-Path -Parent $MyInvocation.MyCommand.Path
$ComposeFile = Join-Path $RootDir "docker-compose.release.yml"

# ─── pre-flight checks ──────────────────────────────────────────────
Banner "Pre-flight checks"

if (-not (Test-Path $ComposeFile)) {
    Die "$ComposeFile not found — run from repo root or release tarball."
}

# Docker engine
try { docker info 2>&1 | Out-Null } catch { Die "Docker is not running or not installed. Start Docker Desktop, then re-run." }
if ($LASTEXITCODE -ne 0) { Die "Docker engine not reachable. Start Docker Desktop, then re-run." }
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
    Warn "Only $freeGB GB free on $($drive.Name): — recommend >= 30 GB. Pull may fail mid-flight."
} else {
    Say "Free disk: $freeGB GB"
}

# Docker memory (Docker Desktop reports its allocation via 'docker info')
$memBytes = [int64](docker info --format '{{.MemTotal}}' 2>$null)
if ($memBytes -gt 0) {
    $memGB = [math]::Round($memBytes / 1GB)
    if ($memGB -lt 7) {
        Warn "Docker is configured with $memGB GB RAM — recommend >= 8 GB. Settings → Resources in Docker Desktop."
    } else {
        Say "Docker memory: $memGB GB"
    }
}

# Required ports — bind a TcpListener briefly to confirm free
$portsBusy = @()
foreach ($port in 8088, 9080, 9443, 2222) {
    try {
        $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $port)
        $listener.Start()
        $listener.Stop()
    } catch {
        $portsBusy += $port
    }
}
if ($portsBusy.Count -gt 0) {
    Die ("Required loopback ports already in use: " + ($portsBusy -join ", ") + ". Stop whatever is bound to them, then re-run.")
}
Say "Loopback ports 8088, 9080, 9443, 2222 are free"

# ─── check-only short-circuit ───────────────────────────────────────
if ($CheckOnly) {
    Banner "Pre-flight passed — laptop is ready"
    @"
  All checks above passed. To install:

    .\setup.ps1                       # latest
    .\setup.ps1 -Version v0.1.0       # pinned release

  For offline / SSD install, use -FromTarballs <PATH>.
"@ | Write-Host
    exit 0
}

# ─── image acquisition ──────────────────────────────────────────────
if ($FromTarballs) {
    Banner "Loading images from tarballs"
    if (-not (Test-Path $FromTarballs)) { Die "Tarball directory not found: $FromTarballs" }
    $tarball = Join-Path $FromTarballs "images-$arch.tar"
    if (-not (Test-Path $tarball)) { Die "Expected $tarball (this host is $arch). Have you staged the right architecture?" }
    $sizeMB = [math]::Round((Get-Item $tarball).Length / 1MB)
    Say "Loading $tarball (${sizeMB} MB) — this can take a few minutes"
    docker load -i $tarball
    Say "Images loaded"
} else {
    Banner "Pulling images from GHCR"
    Say "Version: $Version"
    Say "(this can take a while on first run; subsequent pulls are layer-cached)"
    $env:VERSION = $Version
    docker compose -f $ComposeFile pull
}

# ─── start the stack ────────────────────────────────────────────────
Banner "Starting RangerDanger"
$env:VERSION = $Version
docker compose -f $ComposeFile up -d
if ($LASTEXITCODE -ne 0) { Die "compose up failed — check logs." }
Say "Containers started"

# ─── health smoke check ─────────────────────────────────────────────
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

# ─── done ────────────────────────────────────────────────────────────
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

For the lab security model and how to expose this to other machines
on purpose, see SECURITY.md.
"@ | Write-Host
