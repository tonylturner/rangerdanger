# Bounded backend lifecycle requests, shared by Windows setup and tests.
. "$PSScriptRoot/range.ps1"

function Test-SetupRangeSettled($Status) {
    return ($Status -and $Status.phase -in @('none', 'ready', 'failed'))
}

function Wait-SetupHealth([string]$Api, [int]$BudgetSec = 120) {
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($clock.Elapsed.TotalSeconds -lt $BudgetSec) {
        try {
            $r = Invoke-WebRequest -Uri "$Api/api/health" -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
            if ($r.StatusCode -eq 200) { return }
        } catch { Write-Verbose "Backend is not ready: $_" }
        Start-Sleep -Seconds 2
    }
    throw "Backend did not report healthy in ${BudgetSec}s. Check docker logs rangerdanger-backend."
}

function Wait-SetupRangeSettled([string]$Api, [int]$BudgetSec) {
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($clock.Elapsed.TotalSeconds -lt $BudgetSec) {
        if (Test-SetupRangeSettled (Get-RangeStatus $Api)) { return }
        Start-Sleep -Seconds 3
    }
    throw 'A range transition is still running or its status is unreachable.'
}

function Start-SetupRange([string]$Api, [string]$Package, [int]$BudgetSec = 600) {
    $body = if ($Package) { @{ package=$Package } | ConvertTo-Json -Compress } else { '{}' }
    $response = $null
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            $r = Invoke-WebRequest -Uri "$Api/api/range" -Method POST -ContentType 'application/json' -Body $body -UseBasicParsing -TimeoutSec 30 -ErrorAction Stop
        } catch {
            if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 409) { throw }
            Wait-SetupRangeSettled $Api $BudgetSec
            continue
        }
        if ($r.StatusCode -ne 202) { throw "POST /api/range returned HTTP $($r.StatusCode): $($r.Content)" }
        $response = $r.Content | ConvertFrom-Json -ErrorAction Stop
        break
    }
    if (-not $response) { throw 'POST /api/range kept returning HTTP 409.' }
    $target = if ($response.target) { $response.target } else { $response.package }
    if (-not $target -or ($Package -and $target -ne $Package)) {
        throw 'POST /api/range did not identify the requested range.'
    }
    $clock = [Diagnostics.Stopwatch]::StartNew()
    $lastPhase = ''
    while ($clock.Elapsed.TotalSeconds -lt $BudgetSec) {
        $status = Get-RangeStatus $Api
        if ($status.phase -and $status.phase -ne $lastPhase) {
            Write-Host "[+] range phase: $($status.phase)"
            $lastPhase = $status.phase
        }
        if ((Test-SetupRangeSettled $status) -and $status.error) { throw "The range did not start: $($status.error)" }
        if (Test-RangeReady $status $target) {
            return $status.package
        }
        Start-Sleep -Seconds 3
    }
    throw "Range did not report ready in ${BudgetSec}s (last phase: $lastPhase). Check docker logs rangerdanger-backend."
}
