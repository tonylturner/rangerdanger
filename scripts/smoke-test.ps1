<#
.SYNOPSIS
End-to-end lab smoke test -- Windows sibling of scripts/smoke-test.sh.

.DESCRIPTION
Brings the RangerDanger stack up (build + compose up), hits the API
endpoints, validates the expected lab inventory + step counts, and
confirms enough services report (healthy).

.PARAMETER Keep
Leave the stack running after the test finishes. Without -Keep, the
script runs `docker compose down -v` on exit.

.EXAMPLE
.\scripts\smoke-test.ps1
.\scripts\smoke-test.ps1 -Keep

.NOTES
Exit 0 = pass, non-zero = fail. Designed to match scripts/smoke-test.sh
byte-for-byte in test semantics; only the host-side glue (curl+jq ->
Invoke-RestMethod + ConvertFrom-Json, /tmp -> $env:TEMP, bash trap ->
try/finally) is reshaped for Windows.

ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param([switch]$Keep)

$ErrorActionPreference = "Continue"   # we want to keep running on errors and aggregate

$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RootDir

# Expected lab inventory after the workshop-deck-aligned restructure.
# Format: order|id (sorted lexicographically -- same order containd
# returns from /api/scenarios).
$Expected = @(
    @{ order='1.2';        id='baseline-assessment' },
    @{ order='1.3';        id='segmentation-requirements' },
    @{ order='1.4';        id='remediation-planning' },
    @{ order='2.2';        id='firewall-implementation' },
    @{ order='2.3';        id='hardening-configurations' },
    @{ order='2.3-bonus';  id='vendor-rdp-compromise' },
    @{ order='2.4';        id='validation-evidence' }
)

$script:fail = 0
function Note($msg) { Write-Host ""; Write-Host "=== $msg ===" -ForegroundColor Cyan }
function OK($msg)   { Write-Host "  [+] $msg" -ForegroundColor Green }
function Err($msg)  { Write-Host "  [x] $msg" -ForegroundColor Red; $script:fail = 1 }

$BuildLog = Join-Path $env:TEMP "smoke-build.log"
$UpLog    = Join-Path $env:TEMP "smoke-up.log"

function Invoke-Cleanup {
    if (-not $Keep) {
        Note "tearing down"
        & { $ErrorActionPreference = 'SilentlyContinue'; docker compose down -v *>$null }
    } else {
        Note "stack left running (-Keep)"
    }
}

try {
    # --- preflight ----------------------------------------------------------
    Note "validate compose syntax"
    & { $ErrorActionPreference = 'SilentlyContinue'; docker compose config -q *>$null }
    if ($LASTEXITCODE -eq 0) { OK "docker-compose.yml" } else { Err "docker-compose.yml" }
    & { $ErrorActionPreference = 'SilentlyContinue'; docker compose -f docker-compose.release.yml config -q *>$null }
    if ($LASTEXITCODE -eq 0) { OK "docker-compose.release.yml" } else { Err "docker-compose.release.yml" }

    # Stale DB from before the order int->string change would break boot.
    $StaleDb = Join-Path $RootDir "backend\data\rangerdanger.db"
    if (Test-Path $StaleDb) {
        Remove-Item $StaleDb -Force
        OK "stale labs.db cleared"
    } else {
        OK "stale labs.db cleared (none present)"
    }

    # --- bring up -----------------------------------------------------------
    Note "build + up"
    & { $ErrorActionPreference = 'SilentlyContinue'; docker compose build --parallel *> $BuildLog }
    if ($LASTEXITCODE -eq 0) {
        OK "build complete"
    } else {
        Err "build failed; see $BuildLog"
        exit 1
    }
    & { $ErrorActionPreference = 'SilentlyContinue'; docker compose up -d *> $UpLog }
    if ($LASTEXITCODE -eq 0) {
        OK "compose up"
    } else {
        Err "compose up failed; see $UpLog"
        exit 1
    }

    # --- wait for backend healthy ------------------------------------------
    Note "wait for backend health (5min budget)"
    $healthy = $false
    for ($i = 1; $i -le 30; $i++) {
        try {
            $r = Invoke-WebRequest -Uri 'http://localhost:8088/api/health' -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop
            if ($r.StatusCode -eq 200) { OK "backend healthy after $($i * 10)s"; $healthy = $true; break }
        } catch { Start-Sleep -Seconds 10 }
    }
    if (-not $healthy) {
        Err "backend never healthy"
        docker compose ps --format '{{.Service}} {{.Status}}'
        exit 1
    }

    # --- probe endpoints ----------------------------------------------------
    Note "probe /api/health and /api/build"
    try {
        $health = Invoke-RestMethod -Uri 'http://localhost:8088/api/health' -TimeoutSec 5 -ErrorAction Stop
        if ($health) { OK "/api/health JSON" } else { Err "/api/health (empty)" }
    } catch { Err "/api/health: $_" }
    try {
        $build = Invoke-RestMethod -Uri 'http://localhost:8088/api/build' -TimeoutSec 5 -ErrorAction Stop
        if ($build) { OK "/api/build JSON" } else { Err "/api/build (empty)" }
    } catch { Err "/api/build: $_" }

    # --- lab inventory ------------------------------------------------------
    Note "validate lab inventory"
    try {
        $inv = Invoke-RestMethod -Uri 'http://localhost:8088/api/scenarios' -TimeoutSec 10 -ErrorAction Stop
    } catch {
        Err "/api/scenarios fetch failed: $_"
        exit 1
    }

    $actualCount = @($inv.scenarios).Count
    $expectedCount = $Expected.Count
    if ($actualCount -eq $expectedCount) {
        OK "scenario count = $expectedCount"
    } else {
        Err "scenario count: expected $expectedCount, got $actualCount"
        foreach ($s in $inv.scenarios) { Write-Host ("  {0}  {1}" -f $s.order, $s.id) }
    }

    # Each expected (order,id) must appear with both fields matching.
    foreach ($entry in $Expected) {
        $hit = $inv.scenarios | Where-Object { $_.id -eq $entry.id -and $_.order -eq $entry.order } | Select-Object -First 1
        if ($hit) {
            OK "Lab $($entry.order)  $($entry.id)"
        } else {
            Err "Lab $($entry.order)  $($entry.id)  (missing or wrong order)"
        }
    }

    # Per-lab step counts -- catches accidental empty steps. The .steps
    # field is a JSON-stringified blob (not a structured array), so we
    # count occurrences of the substring '"description":' the same way
    # smoke-test.sh does (each step has exactly one).
    Note "step counts per lab"
    foreach ($entry in $Expected) {
        $sc = $inv.scenarios | Where-Object { $_.id -eq $entry.id } | Select-Object -First 1
        if (-not $sc) { continue }   # already reported above
        $stepsText = "$($sc.steps)"
        $count = ([regex]::Matches($stepsText, '"description":')).Count
        if ($count -ge 3) {
            OK "$($entry.id): $count steps"
        } else {
            Err "$($entry.id): $count steps (expected >=3)"
        }
    }

    # --- service health -----------------------------------------------------
    Note "compose services health"
    $psLines = & docker compose ps --format '{{.Service}}`t{{.Status}}'
    foreach ($line in $psLines) { Write-Host "  $line" }
    $statusLines = & docker compose ps --format '{{.Status}}'
    $healthyCount = (@($statusLines) | Where-Object { $_ -match '\(healthy\)' }).Count
    if ($healthyCount -ge 8) {
        OK "$healthyCount services report (healthy)"
    } else {
        Err "only $healthyCount services healthy (expected >=8)"
    }

    # --- summary ------------------------------------------------------------
    Note "summary"
    if ($script:fail -eq 0) {
        Write-Host "  ALL CHECKS PASSED" -ForegroundColor Green
    } else {
        Write-Host "  FAILED -- see [x] entries above" -ForegroundColor Red
    }
}
finally {
    Invoke-Cleanup
    exit $script:fail
}
