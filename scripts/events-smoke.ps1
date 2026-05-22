<#
.SYNOPSIS
Events smoke test -- Windows sibling of scripts/events-smoke.sh.

.DESCRIPTION
Exercises the v0.1.25 firewall.rule.hit event pipeline + ICS DPI
enforcement that the traffic-only smoke (firewall-smoke.ps1) does not
cover. Three gates:

  1. L4 firewall.rule.hit event flow under improved policy.
  2. ICS template apply with hyphenated (canonical) name.
  3. ICS DPI function-code allowlist enforcement (NFQUEUE; gracefully
     skipped if the host kernel cannot bind NFQUEUE 101 -- common on
     Docker Desktop's LinuxKit kernel).

Assumes the rangerdanger stack is up and the backend is healthy. Run
.\setup.ps1 first if not (the workshop-readiness gate that finishes
setup will have set the same preconditions).

JWT generation is pure-PowerShell (HMACSHA256 via .NET) so this script
has no python3 dependency on Windows. The probe payload that runs
inside the rtac-sim container is python; that's a per-container concern,
not a host concern.

.NOTES
Exit 0 = all gates pass (skipped gates count as pass), non-zero = at
least one regressed. ASCII-only, BOM-free. See setup.ps1 for the
encoding rationale.
#>

[CmdletBinding()]
param(
    [int]$ProbeWaitSec = $(if ($env:PROBE_WAIT) { [int]$env:PROBE_WAIT } else { 4 })
)

$ErrorActionPreference = "Continue"

$Api          = if ($env:RANGERDANGER_API)   { $env:RANGERDANGER_API }   else { 'http://localhost:8088' }
$FirewallApi  = if ($env:CONTAINTD_API)      { $env:CONTAINTD_API }      else { 'http://localhost:9080' }
$JwtSecret    = if ($env:CONTAIND_JWT_SECRET){ $env:CONTAIND_JWT_SECRET } else { 'rangerdanger-dev' }

$script:fail   = 0
$script:passed = 0
$script:total  = 0

function Note($msg) { Write-Host ""; Write-Host "=== $msg ===" -ForegroundColor Cyan }
function OK($msg)   { Write-Host "  [+] $msg" -ForegroundColor Green; $script:passed++; $script:total++ }
function Err($msg)  { Write-Host "  [x] $msg" -ForegroundColor Red;   $script:fail = 1; $script:total++ }
function Skip($what, $why) { Write-Host "  [-] $what (skipped: $why)" -ForegroundColor DarkGray }

function ConvertTo-Base64Url([byte[]]$bytes) {
    ([Convert]::ToBase64String($bytes)).TrimEnd('=').Replace('+','-').Replace('/','_')
}

function New-AdminJwt {
    $headerJson  = '{"alg":"HS256","typ":"JWT"}'
    $exp = [int64]([DateTime]::UtcNow.AddSeconds(600) - [DateTime]'1970-01-01T00:00:00Z').TotalSeconds
    $payloadJson = "{`"sub`":`"smoke`",`"role`":`"admin`",`"exp`":$exp}"

    $h = ConvertTo-Base64Url ([System.Text.Encoding]::UTF8.GetBytes($headerJson))
    $p = ConvertTo-Base64Url ([System.Text.Encoding]::UTF8.GetBytes($payloadJson))
    $signingInput = "$h.$p"

    $hmac = [System.Security.Cryptography.HMACSHA256]::new()
    try {
        $hmac.Key = [System.Text.Encoding]::UTF8.GetBytes($JwtSecret)
        $sig = $hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($signingInput))
    } finally { $hmac.Dispose() }
    $sigB64 = ConvertTo-Base64Url $sig
    "$signingInput.$sigB64"
}

# --- Preflight ----------------------------------------------------------
Note "preflight"
try {
    Invoke-WebRequest -Uri "$Api/api/health" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop | Out-Null
    OK "backend healthy"
} catch {
    Err "backend $Api not healthy -- bring stack up first"
    exit 1
}

$token = New-AdminJwt
if (-not $token) { Err "failed to generate JWT for direct containd queries"; exit 1 }

# --- Gate 1: L4 firewall.rule.hit event flow under improved policy ------
Note "gate 1: L4 firewall.rule.hit event flow"

try {
    $resp = Invoke-RestMethod -Uri "$Api/api/firewall/apply" -Method POST `
        -ContentType 'application/json' -Body '{"config":"improved"}' `
        -TimeoutSec 30 -ErrorAction Stop
    OK "apply improved: $($resp | ConvertTo-Json -Compress)"
} catch {
    Err "apply improved failed: $_"
    exit 1
}
Start-Sleep -Seconds 2

# Kali Modbus probe -- SYN to 10.40.40.20:502 should hit
# deny-enterprise-to-field (L4 log+drop). Use timeout inside container
# because SYN is dropped.
& {
    $ErrorActionPreference = 'SilentlyContinue'
    docker exec rangerdanger-kali sh -c "nc -nv -w 3 10.40.40.20 502 < /dev/null" *>$null
}
Start-Sleep -Seconds $ProbeWaitSec

# Query engine's full event store directly. limit=500 leaves room for
# the kali drop to appear past RTAC's continuous allow chatter.
$rawEvents = & {
    $ErrorActionPreference = 'SilentlyContinue'
    docker exec rangerdanger-firewall sh -c "curl -s 'http://127.0.0.1:8081/internal/events?limit=500'" 2>$null
}
$denyCount = 0
if ($rawEvents) {
    try {
        $events = $rawEvents | ConvertFrom-Json -ErrorAction Stop
        if ($events) {
            $denyCount = (@($events) | Where-Object {
                $_.kind -eq 'firewall.rule.hit' -and
                $_.attributes.action -eq 'DENY' -and
                $_.srcIp -like '10.10.10.*' -and
                $_.dstIp -like '10.40.40.*' -and
                $_.dstPort -eq 502
            }).Count
        }
    } catch { }
}
if ($denyCount -lt 1) {
    Err "no firewall.rule.hit DENY for kali(10.10.10.50)->field(10.40.40.x):502 in engine events -- nflog consumer regressed"
} else {
    OK "$denyCount kali->field:502 DENY event(s) in engine event store"
}

# Backend's substation surface should return SOME events (catches a
# broken backend Event JSON schema mapping).
try {
    $sub = Invoke-RestMethod -Uri "$Api/api/substation/network-events" -TimeoutSec 10 -ErrorAction Stop
    $subTotal = if ($sub.events) { @($sub.events).Count } else { 0 }
} catch { $subTotal = 0 }
if ($subTotal -lt 1) {
    Err "/api/substation/network-events returned 0 events -- backend Event schema may be misaligned with containd"
} else {
    OK "/api/substation/network-events delivering events ($subTotal in window)"
}

# --- Gate 2: ICS template hyphen<->underscore normalization ------------
Note "gate 2: ICS template name normalization (hyphen <-> underscore)"

# Preview=true exercises the name-normalization logic without altering
# the running policy. Run from inside the firewall (loopback bypasses
# the mgmt-iface access check so host-side probes don't need lan3).
#
# IMPORTANT (Windows PS 5.1 quoting): passing a complex sh -c command
# with nested quotes via docker.exe goes through PowerShell's native
# native-exe arg quoter, which in PS 5.1 strips/re-splits internal
# whitespace inside a single-quoted PS arg and breaks the shell command.
# Symptom: curl reports "no URL specified". To avoid the entire
# quoting nightmare, base64-encode the shell command on the host and
# decode + execute inside the container -- the only thing PS passes is
# plain ASCII with no embedded quotes.
$shCmd = 'curl -s -m 10 -X POST -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" -d "{\"template\":\"modbus-read-only\",\"preview\":true,\"sourceZones\":[\"lan1\"],\"destZones\":[\"lan2\"]}" http://127.0.0.1:8080/api/v1/templates/ics/apply'
$b64cmd = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($shCmd))
$tmplResp = & {
    $ErrorActionPreference = 'SilentlyContinue'
    docker exec -e "TOK=$token" -e "B64=$b64cmd" rangerdanger-firewall sh -c 'echo "$B64" | base64 -d | sh' 2>$null
}
if ($tmplResp -match '"preview":true') {
    try {
        $parsed = $tmplResp | ConvertFrom-Json -ErrorAction Stop
        $ruleCount = if ($parsed.rules) { @($parsed.rules).Count } else { 0 }
    } catch { $ruleCount = 0 }
    if ($ruleCount -ge 2) {
        OK "modbus-read-only (hyphenated) accepted + previewed $ruleCount rules"
    } else {
        Err "preview returned $ruleCount rules (expected >=2)"
    }
} else {
    Err "template preview rejected hyphenated name: $tmplResp"
}

# --- Gate 3: ICS DPI FC8 enforcement (NFQUEUE) -------------------------
Note "gate 3: ICS DPI function-code allowlist enforcement"

# NFQUEUE 101 must be bound by containd; on Docker Desktop's LinuxKit
# kernel this often fails silently and containd falls back to plain
# accept. Skip rather than fail in that case.
$nfqStatus = & {
    $ErrorActionPreference = 'SilentlyContinue'
    (docker exec rangerdanger-firewall sh -c "cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null" 2>$null) -join "`n"
}
$nfqAlive = ($nfqStatus -match '(?m)^\s*101\b')

if (-not $nfqAlive) {
    Skip "ICS DPI enforcement" "NFQUEUE 101 not bound (kernel/library env does not support it)"
} else {
    OK "NFQUEUE 101 bound by containd"

    $hasPython = & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker exec rangerdanger-rtac-sim which python3 *>$null
        $LASTEXITCODE
    }
    if ($hasPython -ne 0) {
        Skip "ICS DPI enforcement" "rtac-sim lacks python3"
    } else {
        # Install probe inside the container. We pipe via stdin to avoid
        # a docker cp dance with temp files. The probe itself stays the
        # same as in events-smoke.sh.
        $probePy = @'
import socket, struct, sys
target, fc = sys.argv[1], sys.argv[2]
def mbap(pdu, tx=1):
    return struct.pack(">HHHB", tx, 0, len(pdu)+1, 1) + pdu
fc_map = {
    "fc3": mbap(struct.pack(">BHH", 3, 0, 1)),
    "fc8": mbap(struct.pack(">BHH", 8, 0, 0)),
}
s = socket.socket(); s.settimeout(3)
try:
    s.connect((target, 502)); s.send(fc_map[fc])
    try: print(s.recv(256).hex())
    except socket.timeout: print("timeout")
finally: s.close()
'@
        # docker exec -i reads stdin from this PS pipe; write the file
        # via `sh -c "cat > /tmp/..."` exactly the way the .sh does.
        $probePy | & docker exec -i rangerdanger-rtac-sim sh -c "cat > /tmp/_smoke_modbus.py"

        # Fire FC8. DPI parses fc=8 NOT in [1..6] -> BlockFlowTemp ->
        # entry in block_flows nft set. Verdict is added async; check
        # the SET, not whether the packet itself was blocked.
        & {
            $ErrorActionPreference = 'SilentlyContinue'
            docker exec rangerdanger-rtac-sim python3 /tmp/_smoke_modbus.py 10.40.40.20 fc8 *>$null
        }
        Start-Sleep -Seconds $ProbeWaitSec

        $blockRaw = & {
            $ErrorActionPreference = 'SilentlyContinue'
            (docker exec rangerdanger-firewall sh -c "nft list set inet containd block_flows 2>/dev/null" 2>$null) -join "`n"
        }
        $blockMatches = ([regex]::Matches($blockRaw, '10\.30\.30\.20.*10\.40\.40\.20.*502')).Count
        if ($blockMatches -lt 1) {
            Err "FC8 from RTAC did not produce a block_flows entry -- DPI parser may not be wired to BlockFlowTemp"
        } else {
            OK "FC8 (not in allowlist [1..6]) triggered block_flows entry for 10.30.30.20.10.40.40.20.502"
        }
    }
}

# --- Summary ------------------------------------------------------------
Note "summary"
Write-Host "  passed: $($script:passed) / $($script:total)"
if ($script:fail -eq 0) {
    Write-Host "  ALL EVENT GATES PASSED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  FAILED -- $($script:total - $script:passed) gate(s) regressed" -ForegroundColor Red
    exit 1
}
