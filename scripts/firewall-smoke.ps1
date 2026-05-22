<#
.SYNOPSIS
Firewall traffic smoke test -- Windows sibling of scripts/firewall-smoke.sh.

.DESCRIPTION
Applies each named policy via POST /api/firewall/apply (the same path
the lab UI uses), then probes a positive+negative traffic matrix from
inside the lab containers to confirm the dataplane enforces what the
policy says.

The probe itself runs INSIDE the Linux containers via docker exec --
the PowerShell side is just the host-side driver, so policy semantics
are identical to the .sh version.

.PARAMETER Targets
weak | improved | both. Default: both.

.PARAMETER ProbeTimeoutSec
Seconds per probe. Default: 3.

.PARAMETER SettleSecs
Wait after apply for nft reconcile (only used when canary polling does
not apply -- weak/improved hit the canary path). Default: 3.

.PARAMETER RefusedThresholdMs
Threshold for fast-fail vs slow-timeout disambiguation. Refused TCP
(RST received) typically completes within tens of ms. Firewall drop
completes at ProbeTimeoutSec * 1000ms. 500ms is a safe middle.

.NOTES
Exit 0 = all rows match expected, non-zero = at least one row mismatched.
ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [ValidateSet('weak','improved','both')]
    [string]$Targets = 'both',
    [int]$ProbeTimeoutSec = $(if ($env:PROBE_TIMEOUT) { [int]$env:PROBE_TIMEOUT } else { 3 }),
    [int]$SettleSecs     = $(if ($env:SETTLE_SECS)   { [int]$env:SETTLE_SECS   } else { 3 }),
    [int]$RefusedThresholdMs = $(if ($env:REFUSED_THRESHOLD_MS) { [int]$env:REFUSED_THRESHOLD_MS } else { 500 })
)

$ErrorActionPreference = "Continue"

$Api = if ($env:RANGERDANGER_API) { $env:RANGERDANGER_API } else { 'http://localhost:8088' }

$script:fail   = 0
$script:total  = 0
$script:passed = 0

function Note($msg) { Write-Host ""; Write-Host "=== $msg ===" -ForegroundColor Cyan }
function OK($msg)   { Write-Host "  [+] $msg" -ForegroundColor Green; $script:passed++ }
function Err($msg)  { Write-Host "  [x] $msg" -ForegroundColor Red; $script:fail = 1 }

function Get-NowMs { [int64]([DateTime]::UtcNow - [DateTime]'1970-01-01T00:00:00Z').TotalMilliseconds }

# --- Probe helpers --------------------------------------------------------
# probe-tcp emits a PSObject with .Verdict ('allow'/'deny'/'deny:rc=N')
# and .DurationMs. Verdict logic mirrors firewall-smoke.sh:
#   rc 0      = allow
#   rc 124/143 = deny (timeout)
#   rc 1 ambiguous: <RefusedThresholdMs = allow (RST), else deny (drop)
function Invoke-TcpProbe($src, $dst, $port) {
    $start = Get-NowMs

    # docker exec stderr/stdout discarded via & { ... *>$null } scope so
    # benign noise (e.g. "OCI runtime: container not running") doesn't
    # confuse the exit-code check the same way the docker info issue did
    # in setup.ps1.
    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec $src sh -c 'command -v bash >/dev/null 2>&1' *>$null
    }
    $hasBash = ($LASTEXITCODE -eq 0)

    if ($hasBash) {
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            docker exec $src timeout $ProbeTimeoutSec bash -c "exec 3<>/dev/tcp/$dst/$port" *>$null
        }
    } else {
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            docker exec $src sh -c "timeout $ProbeTimeoutSec nc -w $ProbeTimeoutSec $dst $port < /dev/null > /dev/null 2>&1" *>$null
        }
    }
    $rc = $LASTEXITCODE
    $end = Get-NowMs
    $dur = $end - $start

    $verdict = switch ($rc) {
        0       { 'allow' }
        124     { 'deny' }
        143     { 'deny' }
        1       { if ($dur -lt $RefusedThresholdMs) { 'allow' } else { 'deny' } }
        default { "deny:rc=$rc" }
    }
    [pscustomobject]@{ Verdict=$verdict; DurationMs=$dur }
}

# probe-udp uses the firewall's nft drop counter snapshot. Same logic
# as the .sh version: snapshot before, fire, snapshot after, compare.
function Invoke-UdpProbe($src, $dst, $port) {
    $extractCount = {
        param($table)
        $line = ($table -split "`n" | Where-Object { $_ -match 'policy drop' } | Select-Object -First 1)
        if (-not $line) { return 0 }
        # awk '/policy drop/{getline; print}' -- the line AFTER the match
        # is the counters line. PS equivalent: index of match + 1.
        $lines = $table -split "`n"
        for ($i = 0; $i -lt $lines.Count; $i++) {
            if ($lines[$i] -match 'policy drop' -and ($i + 1) -lt $lines.Count) {
                if ($lines[$i+1] -match 'packets\s+(\d+)') { return [int64]$Matches[1] }
            }
        }
        return 0
    }

    $before = & {
        $ErrorActionPreference = 'SilentlyContinue'
        (docker exec rangerdanger-firewall nft list table inet containd 2>$null) -join "`n"
    }
    $beforeCount = & $extractCount $before

    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec $src sh -c "echo probe | timeout $ProbeTimeoutSec nc -u -w1 $dst $port" *>$null
    }
    Start-Sleep -Milliseconds 500

    $after = & {
        $ErrorActionPreference = 'SilentlyContinue'
        (docker exec rangerdanger-firewall nft list table inet containd 2>$null) -join "`n"
    }
    $afterCount = & $extractCount $after

    if ($afterCount -gt $beforeCount) {
        [pscustomobject]@{ Verdict='deny';  DurationMs=0 }
    } else {
        [pscustomobject]@{ Verdict='allow'; DurationMs=0 }
    }
}

function Invoke-Probe($proto, $src, $dst, $port) {
    switch ($proto) {
        'tcp' { Invoke-TcpProbe $src $dst $port }
        'udp' { Invoke-UdpProbe $src $dst $port }
        default { [pscustomobject]@{ Verdict='deny:bad-proto'; DurationMs=0 } }
    }
}

# Firewall zone-side IPs (each zone reaches firewall on its own subnet).
$FW_WAN  = '10.10.10.2'
$FW_DMZ  = '10.20.20.2'
$FW_LAN1 = '10.30.30.2'
$FW_LAN2 = '10.40.40.2'

# Matrix mirrors firewall-smoke.sh line-for-line. Field order:
# src|dst|proto|port|expect_weak|expect_improved|note
$Matrix = @"
rangerdanger-kali|10.20.20.10|tcp|22|allow|allow|kali->vendor SSH (vendor portal)
rangerdanger-kali|10.20.20.10|tcp|80|allow|allow|kali->vendor HTTP (portal page)
rangerdanger-kali|10.20.20.10|tcp|443|allow|allow|kali->vendor HTTPS (portal page TLS)
rangerdanger-kali|10.20.20.10|tcp|3389|allow|deny|kali->vendor RDP (improved blocks)
rangerdanger-kali|10.20.20.10|tcp|5900|allow|deny|kali->vendor VNC (improved blocks)
rangerdanger-kali|10.30.30.20|tcp|8080|allow|deny|kali->rtac HTTP API
rangerdanger-kali|10.30.30.20|tcp|502|allow|deny|kali->rtac Modbus (multi-proto pin)
rangerdanger-kali|10.30.30.20|tcp|20000|allow|deny|kali->rtac DNP3 (multi-proto pin)
rangerdanger-kali|10.30.30.30|tcp|8080|allow|deny|kali->openplc HTTP
rangerdanger-kali|10.30.30.30|tcp|502|allow|deny|kali->openplc Modbus (multi-proto pin)
rangerdanger-eng-ws|10.30.30.20|tcp|8080|allow|deny|eng->rtac HTTP (weak wide)
rangerdanger-vendor-jump|10.30.30.20|tcp|22|allow|allow|vendor->rtac SSH mgmt (improved keeps for monitoring)
rangerdanger-vendor-jump|10.30.30.20|tcp|443|allow|allow|vendor->rtac HTTPS mgmt (improved keeps for monitoring)
rangerdanger-vendor-jump|10.30.30.20|tcp|502|allow|deny|vendor->rtac Modbus (improved blocks)
rangerdanger-eng-ws|10.30.30.30|tcp|502|allow|deny|eng->openplc Modbus (weak wide)
rangerdanger-fuxa-hmi|10.30.30.30|tcp|502|allow|allow|fuxa(intra-zone OT)->openplc Modbus
rangerdanger-historian-sim|10.30.30.30|tcp|502|allow|allow|historian(intra-zone OT)->openplc Modbus
rangerdanger-rtac-sim|10.30.30.30|tcp|502|allow|allow|rtac->openplc Modbus (intra-zone)
rangerdanger-rtac-sim|10.30.30.30|tcp|20000|allow|allow|rtac->openplc DNP3 (intra-zone)
rangerdanger-rtac-sim|10.30.30.30|tcp|8080|allow|allow|rtac->openplc HTTP (intra-zone)
rangerdanger-kali|$FW_WAN|tcp|8080|allow|deny|kali->fw mgmt (improved blocks wan)
rangerdanger-kali|$FW_WAN|tcp|2222|allow|deny|kali->fw SSH (improved blocks wan)
rangerdanger-eng-ws|$FW_DMZ|tcp|8080|allow|allow|eng->fw mgmt (always allowed)
rangerdanger-eng-ws|$FW_DMZ|tcp|2222|allow|allow|eng->fw SSH (always allowed)
rangerdanger-rtac-sim|$FW_LAN1|tcp|8080|allow|allow|rtac->fw mgmt (always allowed)
rangerdanger-relay-sim|$FW_LAN2|tcp|8080|allow|deny|relay->fw mgmt (improved blocks lan2; openplc moved off lan2 in F-011)
"@

# Dataplane canary -- probes kali->rtac:502 with a 1s timeout so we can
# poll cheaply. Verdict diverges between weak (allow) and improved (deny).
function Test-Canary {
    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec rangerdanger-kali timeout 1 bash -c 'exec 3<>/dev/tcp/10.30.30.20/502' *>$null
    }
    if ($LASTEXITCODE -eq 0) { 'allow' } else { 'deny' }
}

function Wait-ForDataplane($expected, $budgetSec = 15) {
    for ($i = 1; $i -le ($budgetSec * 2); $i++) {
        if ((Test-Canary) -eq $expected) { return $true }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Invoke-PolicyApply($name) {
    Note "applying policy: $name"
    try {
        $resp = Invoke-RestMethod -Uri "$Api/api/firewall/apply" -Method POST `
                  -ContentType 'application/json' `
                  -Body (@{ config = $name } | ConvertTo-Json -Compress) `
                  -TimeoutSec 30 -ErrorAction Stop
        OK "applied: $($resp | ConvertTo-Json -Compress)"
    } catch {
        Err "apply $name failed: $_"
        return $false
    }

    $expectedCanary = switch ($name) {
        'weak'     { 'allow' }
        'baseline' { 'allow' }
        'improved' { 'deny' }
        'hardened' { 'deny' }
        default    { Start-Sleep -Seconds $SettleSecs; return $true }
    }
    if (Wait-ForDataplane $expectedCanary 15) {
        OK "dataplane reconciled to $name"
        return $true
    } else {
        Err "dataplane never reconciled to $name (canary kali->rtac:502 still wrong after 15s)"
        return $false
    }
}

function Invoke-Matrix($policy) {
    foreach ($line in ($Matrix -split "`r?`n")) {
        if (-not $line.Trim()) { continue }
        $f = $line -split '\|'
        if ($f.Count -lt 7) { continue }
        $src = $f[0]; $dst = $f[1]; $proto = $f[2]; $port = $f[3]
        $expWeak = $f[4]; $expImproved = $f[5]; $label = $f[6]
        $expect = if ($policy -eq 'weak') { $expWeak } else { $expImproved }

        $script:total++
        $out = Invoke-Probe $proto $src $dst $port
        if ($out.Verdict -eq $expect) {
            OK "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$($out.Verdict)  $($out.DurationMs)ms"
        } else {
            Err "[$policy] $label  ($src $proto/$port -> $dst)  expect=$expect actual=$($out.Verdict)  $($out.DurationMs)ms"
        }
    }
}

# --- Preflight ----------------------------------------------------------
Note "preflight"
try {
    Invoke-WebRequest -Uri "$Api/api/health" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop | Out-Null
    OK "backend $Api healthy"
} catch {
    Err "backend not reachable at $Api -- bring stack up first"
    exit 1
}

$Required = @(
    'rangerdanger-firewall','rangerdanger-kali','rangerdanger-eng-ws',
    'rangerdanger-rtac-sim','rangerdanger-fuxa-hmi','rangerdanger-historian-sim',
    'rangerdanger-openplc'
)
foreach ($c in $Required) {
    $state = & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker inspect -f '{{.State.Status}}' $c 2>$null
    }
    if ($state -match 'running') { OK "$c running" } else { Err "$c not running" }
}
if ($script:fail -ne 0) { Note "summary"; Write-Host "  preflight failed; aborting"; exit 1 }

# Wait for cross-zone routing -- kasm webtops install firewall as default
# gateway via /custom-cont-init.d/set-gateway.sh, which can finish AFTER
# backend reports healthy on slow runners.
Note "wait for cross-zone routing"
$Webtops = @(
    @{ container='rangerdanger-eng-ws';      gw='10.20.20.2' },
    @{ container='rangerdanger-vendor-jump'; gw='10.20.20.2' }
)
foreach ($w in $Webtops) {
    $ready = $false
    for ($i = 1; $i -le 30; $i++) {
        $r = & {
            $ErrorActionPreference = 'SilentlyContinue'
            (docker exec $w.container ip route show default 2>$null) -join ' '
        }
        if ($r -match "via $($w.gw)") { OK "$($w.container) default route via $($w.gw) (after ${i}s)"; $ready = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { Err "$($w.container) never installed default route via $($w.gw) -- set-gateway.sh may have failed" }
}
if ($script:fail -ne 0) { Note "summary"; Write-Host "  preflight failed; aborting"; exit 1 }

# Wait for canonical listeners. openplc Modbus/DNP3 daemons start AFTER
# the HTTP healthcheck, so probing :502 too early lands on closed port.
Note "wait for canonical service listeners"
$Listeners = @(
    @{ hp='10.30.30.20:8080';  label='rtac HTTP API' },
    @{ hp='10.30.30.20:502';   label='rtac Modbus' },
    @{ hp='10.30.30.20:20000'; label='rtac DNP3' },
    @{ hp='10.30.30.20:22';    label='rtac SSH mgmt' },
    @{ hp='10.30.30.20:443';   label='rtac HTTPS mgmt' },
    @{ hp='10.30.30.30:502';   label='openplc Modbus' },
    @{ hp='10.30.30.30:8080';  label='openplc HTTP' },
    @{ hp='10.40.40.20:502';   label='relay Modbus' },
    @{ hp='10.20.20.10:8082';  label='vendor-jump kasm' },
    @{ hp='10.20.20.10:22';    label='vendor-jump SSH' }
)
# Apply weak briefly so listener probes can traverse zones.
try {
    Invoke-RestMethod -Uri "$Api/api/firewall/apply" -Method POST `
        -ContentType 'application/json' -Body '{"config":"weak"}' `
        -TimeoutSec 10 -ErrorAction Stop | Out-Null
} catch { }
Start-Sleep -Seconds 2

foreach ($l in $Listeners) {
    $host_, $port = $l.hp -split ':', 2
    $ready = $false
    for ($i = 1; $i -le 30; $i++) {
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            docker exec rangerdanger-firewall sh -c "timeout 1 bash -c 'exec 3<>/dev/tcp/$host_/$port'" *>$null
        }
        if ($LASTEXITCODE -eq 0) {
            if ($i -gt 1) { OK "$($l.label) ($($l.hp)) listening (after ${i}s)" } else { OK "$($l.label) ($($l.hp)) listening" }
            $ready = $true; break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $ready) { Err "$($l.label) ($($l.hp)) never came up -- image still booting? service crashed?" }
}
if ($script:fail -ne 0) { Note "summary"; Write-Host "  preflight failed; aborting"; exit 1 }

# --- Run policies -------------------------------------------------------
$Policies = switch ($Targets) {
    'weak'     { @('weak') }
    'improved' { @('improved') }
    'both'     { @('weak','improved') }
}

$matrixPassedStart = $script:passed

foreach ($p in $Policies) {
    if (-not (Invoke-PolicyApply $p)) { continue }
    Note "matrix: $p"
    Invoke-Matrix $p
}

# Subtract apply-side ok() calls from matrix count (2 per policy --
# "applied" + "dataplane reconciled"), matching the .sh accounting.
$matrixPassed = $script:passed - $matrixPassedStart - ($Policies.Count * 2)
if ($matrixPassed -lt 0) { $matrixPassed = 0 }

# --- Summary ------------------------------------------------------------
Note "summary"
Write-Host "  matrix passed: $matrixPassed / $($script:total)"
if ($script:fail -eq 0) {
    Write-Host "  ALL TRAFFIC ROWS MATCH EXPECTED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  FAILED -- $($script:total - $matrixPassed) rows mismatched" -ForegroundColor Red
    exit 1
}
