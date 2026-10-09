<#
.SYNOPSIS
End-to-end lab smoke test -- Windows sibling of scripts/smoke-test.sh.

.DESCRIPTION
Brings RangerDanger up from source (scripts\dev-up.ps1: platform, then
the range through the backend), checks that the US range is ready, hits
the API endpoints, validates the expected lab inventory + step counts,
and confirms enough services report (healthy).

.PARAMETER Keep
Leave the lab running after the test finishes. Without -Keep, the
script runs scripts\dev-down.ps1 -Volumes on exit.

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

$UpLog = Join-Path $env:TEMP "smoke-up.log"

# Label-only Compose calls (ps) run from an empty directory: from the repo
# root Compose would load docker-compose.yml as the model.
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("rd-smoke-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $WorkDir | Out-Null

function Invoke-ComposePs([string]$Format) {
    Push-Location $WorkDir
    try {
        & docker compose -p rangerdanger-platform ps --format $Format
        & docker compose -p rangerdanger ps --format $Format
    } finally { Pop-Location }
}

function Invoke-Cleanup {
    if (-not $Keep) {
        Note "tearing down"
        & { $ErrorActionPreference = 'SilentlyContinue'; & (Join-Path $RootDir "scripts\dev-down.ps1") -Volumes *>$null }
        if ($LASTEXITCODE -ne 0) { Err "teardown left resources behind (run .\scripts\dev-down.ps1)" }
    } else {
        Note "lab left running (-Keep)"
    }
    Remove-Item -LiteralPath $WorkDir -Force
}

try {
    # --- preflight ----------------------------------------------------------
    # Platform files under the platform project, every package's range
    # files under the range project; the repo root is the project directory,
    # as setup runs them.
    Note "validate compose syntax"
    $configs = @(
        @{ project = 'rangerdanger-platform'; file = 'docker-compose.yml' },
        @{ project = 'rangerdanger-platform'; file = 'docker-compose.release.yml' }
    )
    foreach ($mode in 'source', 'release') {
        foreach ($f in Get-ChildItem -Path (Join-Path $RootDir ('lab-definitions\packages\*\compose.' + $mode + '.yml'))) {
            $configs += @{ project = 'rangerdanger'; file = $f.FullName.Substring($RootDir.Length + 1) }
        }
    }
    $savedRoot = $env:RANGERDANGER_ROOT
    $env:RANGERDANGER_ROOT = $RootDir
    try {
        foreach ($c in $configs) {
            & { $ErrorActionPreference = 'SilentlyContinue'; docker compose -p $c.project --project-directory $RootDir -f (Join-Path $RootDir $c.file) config -q *>$null }
            if ($LASTEXITCODE -eq 0) { OK $c.file } else { Err $c.file }
        }
    } finally { $env:RANGERDANGER_ROOT = $savedRoot }

    # The backend bind-mounts this database. Stop it before removing the file
    # so it cannot keep using a stale SQLite handle; the bring-up below starts
    # it on the clean database and runs migrations.
    $backend = @(& docker ps -q --filter label=com.docker.compose.project=rangerdanger-platform --filter label=com.docker.compose.service=backend | Where-Object { $_ })
    if ($backend.Count -gt 0) {
        & { $ErrorActionPreference = 'SilentlyContinue'; docker stop @backend *>$null }
        if ($LASTEXITCODE -ne 0) {
            Err "backend could not be stopped; stale database not cleared"
            exit 1
        }
    }
    $StaleDb = Join-Path $RootDir "backend\data\rangerdanger.db"
    if (Test-Path $StaleDb) {
        Remove-Item $StaleDb -Force
        OK "stale database cleared"
    } else {
        OK "stale database cleared (none present)"
    }

    # --- bring up -----------------------------------------------------------
    # dev-up builds every image, starts the platform, waits for the backend
    # and for POST /api/range to bring the range to ready.
    Note "build + up"
    & { $ErrorActionPreference = 'SilentlyContinue'; & (Join-Path $RootDir "scripts\dev-up.ps1") *> $UpLog }
    if ($LASTEXITCODE -eq 0) {
        OK "build, platform up, range ready"
    } else {
        Err "dev-up failed; see $UpLog"
        exit 1
    }

    # --- range --------------------------------------------------------------
    # Every assertion below is about the US range.
    Note "range"
    try {
        $range = Invoke-RestMethod -Uri 'http://localhost:8088/api/range' -TimeoutSec 5 -ErrorAction Stop
    } catch {
        Err "GET /api/range failed: $_"
        exit 1
    }
    if ($range.package -eq 'us-dnp3-substation' -and $range.phase -eq 'ready') {
        OK "range us-dnp3-substation ready"
    } else {
        Err "range is not us-dnp3-substation ready: $($range | ConvertTo-Json -Compress)"
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
    # Platform + range: the same services the single project used to list.
    Note "compose services health"
    $psLines = Invoke-ComposePs "{{.Service}}`t{{.Status}}"
    foreach ($line in $psLines) { Write-Host "  $line" }
    $statusLines = Invoke-ComposePs '{{.Status}}'
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
