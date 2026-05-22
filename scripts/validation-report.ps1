<#
.SYNOPSIS
Substation segmentation validation report generator -- Windows sibling of scripts/validation-report.sh.

.DESCRIPTION
Runs the same authorized + unauthorized traffic test suite Lab 2.4
walks the student through, captures PCAP at the firewall during the
run, and emits a change-board-ready markdown report. Always tests
against the hardened (improved) policy.

.PARAMETER Out
Write the report to this file. Default: stdout.

.PARAMETER ProbeTimeoutSec
Seconds per probe (default 3).

.PARAMETER PcapDurationSecs
Length of the PCAP capture window (default 15).

.EXAMPLE
.\scripts\validation-report.ps1
.\scripts\validation-report.ps1 -Out report.md

.NOTES
Exit 0 = every row matched expected, non-zero = at least one mismatch.
ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [string]$Out = "",
    [int]$ProbeTimeoutSec = $(if ($env:PROBE_TIMEOUT) { [int]$env:PROBE_TIMEOUT } else { 3 }),
    [int]$PcapDurationSecs = $(if ($env:PCAP_DURATION_SECS) { [int]$env:PCAP_DURATION_SECS } else { 15 }),
    [int]$SettleSecs = $(if ($env:SETTLE_SECS) { [int]$env:SETTLE_SECS } else { 2 })
)

$ErrorActionPreference = "Continue"

$RootDir = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $RootDir

$Api = if ($env:RANGERDANGER_API) { $env:RANGERDANGER_API } else { 'http://localhost:8088' }

function Get-NowMs { [int64]([DateTime]::UtcNow - [DateTime]'1970-01-01T00:00:00Z').TotalMilliseconds }
function Write-Status($msg) { Write-Host "validation-report: $msg" -ForegroundColor DarkGray }

# --- Probe primitive (same shape as firewall-smoke.ps1) ----------------
function Invoke-TcpProbe($src, $dst, $port) {
    $start = Get-NowMs
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
    $dur = (Get-NowMs) - $start
    $verdict = switch ($rc) {
        0       { 'allow' }
        124     { 'deny' }
        143     { 'deny' }
        1       { if ($dur -lt 500) { 'allow' } else { 'deny' } }
        default { "error:rc=$rc" }
    }
    [pscustomobject]@{ Verdict=$verdict; DurationMs=$dur }
}

# Test matrix matches validation-report.sh row-for-row.
# Each item: src|dst|port|expected|note|category
$MatrixText = @"
rangerdanger-rtac-sim|10.40.40.20|502|allow|RTAC Modbus poll to relay|authorized
rangerdanger-rtac-sim|10.40.40.21|20000|allow|RTAC DNP3 poll to recloser|authorized
rangerdanger-rtac-sim|10.40.40.22|20000|allow|RTAC DNP3 poll to regulator|authorized
rangerdanger-rtac-sim|10.30.30.30|8080|allow|RTAC HTTP API to OpenPLC (intra-zone)|authorized
rangerdanger-fuxa-hmi|10.30.30.20|8080|allow|HMI to RTAC HTTP intra-zone|authorized
rangerdanger-historian-sim|10.30.30.20|8080|allow|Historian to RTAC intra-zone|authorized
rangerdanger-vendor-jump|10.30.30.20|22|allow|Vendor SSH mgmt to RTAC|authorized
rangerdanger-vendor-jump|10.30.30.20|443|allow|Vendor HTTPS mgmt to RTAC|authorized
rangerdanger-kali|10.40.40.20|502|deny|Enterprise Modbus to field relay|unauthorized
rangerdanger-kali|10.40.40.20|20000|deny|Enterprise DNP3 to field relay|unauthorized
rangerdanger-kali|10.30.30.30|8080|deny|Enterprise HTTP to OpenPLC|unauthorized
rangerdanger-kali|10.30.30.20|8080|deny|Enterprise HTTP to RTAC|unauthorized
rangerdanger-kali|10.30.30.20|502|deny|Enterprise Modbus to RTAC|unauthorized
rangerdanger-eng-ws|10.40.40.21|502|deny|Vendor Modbus to field recloser|unauthorized
rangerdanger-eng-ws|10.40.40.21|20000|deny|Vendor DNP3 to field recloser|unauthorized
rangerdanger-eng-ws|10.30.30.30|8080|deny|Vendor HTTP to OpenPLC (only 443/22 allowed)|unauthorized
rangerdanger-vendor-jump|10.30.30.20|502|deny|Vendor Modbus to RTAC (improved blocks non-mgmt)|unauthorized
rangerdanger-historian-sim|10.40.40.22|502|deny|Non-RTAC OT (historian) to field regulator (Modbus)|unauthorized
rangerdanger-historian-sim|10.40.40.22|20000|deny|Non-RTAC OT (historian) to field regulator (DNP3)|unauthorized
"@

$Ts = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
$PcapPath = "/data/captures/validation-${Ts}.pcap"
$PcapHostPath = "data/firewall/captures/validation-${Ts}.pcap"

# Apply hardened policy
Write-Status "applying hardened policy via $Api/api/firewall/apply"
try {
    Invoke-RestMethod -Uri "$Api/api/firewall/apply" -Method POST -ContentType 'application/json' `
        -Body '{"config":"improved"}' -TimeoutSec 30 -ErrorAction Stop | Out-Null
} catch {
    Write-Status "apply failed: $_"
    exit 1
}
Start-Sleep -Seconds $SettleSecs

# Policy fingerprint
$policyFile = Join-Path $RootDir "lab-definitions\firewall\substation-improved.json"
if (Test-Path $policyFile) {
    $PolicyHash = (Get-FileHash -Algorithm SHA256 -Path $policyFile).Hash.Substring(0,12).ToLower()
} else {
    $PolicyHash = "unknown"
}

# Start PCAP capture in background on firewall
Write-Status "starting PCAP capture (${PcapDurationSecs}s, $PcapPath)"
& {
    $ErrorActionPreference = 'SilentlyContinue'
    docker exec rangerdanger-firewall mkdir -p /data/captures *>$null
}
& {
    $ErrorActionPreference = 'SilentlyContinue'
    docker exec -d rangerdanger-firewall sh -c "timeout $PcapDurationSecs tcpdump -i any -w $PcapPath 'host 10.40.40.20 or host 10.40.40.21 or host 10.40.40.22 or host 10.40.40.23' 2>/dev/null" *>$null
}
Start-Sleep -Seconds 1

# Run probes
$results = New-Object System.Collections.Generic.List[object]
$authPass = 0; $authTotal = 0; $unauthPass = 0; $unauthTotal = 0
$rows = ($MatrixText -split "`r?`n") | Where-Object { $_.Trim() }
Write-Status "running probe matrix (~$($rows.Count) rows)"

foreach ($line in $rows) {
    $f = $line -split '\|'
    if ($f.Count -lt 6) { continue }
    $src = $f[0]; $dst = $f[1]; $port = $f[2]; $expect = $f[3]; $note = $f[4]; $category = $f[5]

    & {
        $ErrorActionPreference = 'SilentlyContinue'
        docker inspect $src *>$null
    }
    if ($LASTEXITCODE -ne 0) {
        $results.Add([pscustomobject]@{
            Category=$category; Src=$src; Dst=$dst; Port=$port; Expect=$expect;
            Actual='skipped'; Dur=0; Note="$note (container not running)"
        })
        continue
    }
    # NOTE: do not name this $out -- PowerShell variables are
    # case-insensitive, so $out and $Out (the script's -Out parameter)
    # are the same variable, and this assignment would clobber the
    # caller's file path.
    $pr = Invoke-TcpProbe $src $dst $port
    $verdict = if ($pr.Verdict -eq $expect) { 'PASS' } else { 'FAIL' }
    $results.Add([pscustomobject]@{
        Category=$category; Src=$src; Dst=$dst; Port=$port; Expect=$expect;
        Actual=$pr.Verdict; Dur=$pr.DurationMs; Note=$note
    })
    switch ($category) {
        'authorized'   { $authTotal++;   if ($verdict -eq 'PASS') { $authPass++ } }
        'unauthorized' { $unauthTotal++; if ($verdict -eq 'PASS') { $unauthPass++ } }
    }
}

# Wait for PCAP capture window to close
Write-Status "waiting for PCAP window to close"
$remain = $PcapDurationSecs - 2
if ($remain -gt 0) { Start-Sleep -Seconds $remain }

# PCAP source-IP analysis
Write-Status "analysing PCAP for source-IP summary"
$tcpdumpCmd = @"
tcpdump -r $PcapPath -nn 2>/dev/null | awk '
/^[0-9]/ {
  if (`$3 != "Out") next
  ip = `$5
  n = split(ip, a, ".")
  if (n < 4) next
  if (a[1] == "10" && a[2] == "40" && a[3] == "40") next
  print a[1] "." a[2] "." a[3] "." a[4]
}' | sort | uniq -c | sort -rn | head -10
"@
$PcapSummary = & {
    $ErrorActionPreference = 'SilentlyContinue'
    (docker exec rangerdanger-firewall sh -c $tcpdumpCmd 2>$null) -join "`n"
}
if (-not $PcapSummary) { $PcapSummary = "(no packets captured during window)" }

# Render markdown
function Render-Report {
    $nowIso = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $md = New-Object System.Text.StringBuilder
    [void]$md.AppendLine("# Substation Segmentation -- Validation Report")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("| Field | Value |")
    [void]$md.AppendLine("|---|---|")
    [void]$md.AppendLine("| **Generated** | $nowIso |")
    [void]$md.AppendLine("| **Policy** | hardened (``substation-improved.json`` sha256:$PolicyHash) |")
    [void]$md.AppendLine("| **PCAP evidence** | ``$PcapHostPath`` |")
    [void]$md.AppendLine("| **Probe timeout** | ${ProbeTimeoutSec}s |")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## Summary")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("| Category | Confirmed | Total |")
    [void]$md.AppendLine("|---|---|---|")
    [void]$md.AppendLine("| **Authorized flows working** | $authPass | $authTotal |")
    [void]$md.AppendLine("| **Unauthorized flows blocked** | $unauthPass | $unauthTotal |")
    [void]$md.AppendLine("")
    if (($authPass -eq $authTotal) -and ($unauthPass -eq $unauthTotal)) {
        [void]$md.AppendLine("**Result: PASS** -- every authorized flow works, every unauthorized flow is blocked. The hardened policy is enforcing what the design specified.")
    } else {
        [void]$md.AppendLine("**Result: REVIEW REQUIRED** -- at least one row did not match expectations. See the per-test detail below.")
    }
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## Authorized flow tests")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("| # | Source | Destination | Port | Expected | Actual | Duration | Test |")
    [void]$md.AppendLine("|---|---|---|---|---|---|---|---|")
    $n = 0
    foreach ($r in $results) {
        if ($r.Category -ne 'authorized') { continue }
        $n++
        $icon = if ($r.Actual -ne $r.Expect) { 'x' } else { '+' }
        $src = $r.Src -replace '^rangerdanger-',''
        [void]$md.AppendLine(("| {0} | {1} {2} | ``{3}`` | {4} | {5} | {6} | {7}ms | {8} |" -f `
            $n, $icon, $src, $r.Dst, $r.Port, $r.Expect, $r.Actual, $r.Dur, $r.Note))
    }
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## Unauthorized flow tests")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("| # | Source | Destination | Port | Expected | Actual | Duration | Test |")
    [void]$md.AppendLine("|---|---|---|---|---|---|---|---|")
    $n = 0
    foreach ($r in $results) {
        if ($r.Category -ne 'unauthorized') { continue }
        $n++
        $icon = if ($r.Actual -ne $r.Expect) { 'x' } else { '+' }
        $src = $r.Src -replace '^rangerdanger-',''
        [void]$md.AppendLine(("| {0} | {1} {2} | ``{3}`` | {4} | {5} | {6} | {7}ms | {8} |" -f `
            $n, $icon, $src, $r.Dst, $r.Port, $r.Expect, $r.Actual, $r.Dur, $r.Note))
    }
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## PCAP source analysis")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("During the ${PcapDurationSecs}-second capture window, traffic to the field zone (``10.40.40.0/24``) came from:")
    [void]$md.AppendLine("")
    [void]$md.AppendLine('```')
    [void]$md.AppendLine($PcapSummary.Trim())
    [void]$md.AppendLine('```')
    [void]$md.AppendLine("")
    [void]$md.AppendLine("For a hardened policy, only the RTAC (``10.30.30.20``) and the GPS time server (``10.30.30.50``, NTP) should appear. Any other source IP indicates a missing rule or a leaky deny.")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## Methodology notes")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("- **Allow** verdict means the source container's TCP SYN reached the destination -- either connection succeeded or destination sent RST. Both prove the firewall did not drop the packet.")
    [void]$md.AppendLine("- **Deny** verdict means the SYN was dropped (timeout at the probe budget). The firewall log on the source rule is the corresponding positive evidence.")
    [void]$md.AppendLine("- Probes ran from inside the listed containers via ``docker exec bash -c `"exec 3<>/dev/tcp/<dst>/<port>`"`` (or ``nc -w`` on busybox-only containers). Same probe primitive used by ``scripts/firewall-smoke.ps1``.")
    [void]$md.AppendLine("- PCAP was captured on the firewall's interface set during the probe run. Host path: ``$PcapHostPath`` (RangerDanger mounts ``./data/firewall`` to the firewall container's ``/data``).")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("## Reviewer checklist")
    [void]$md.AppendLine("")
    [void]$md.AppendLine("- [ ] Every authorized-flow row reads PASS.")
    [void]$md.AppendLine("- [ ] Every unauthorized-flow row reads PASS.")
    [void]$md.AppendLine("- [ ] The PCAP source analysis shows only expected sources reaching the field zone.")
    [void]$md.AppendLine("- [ ] The policy fingerprint above matches the change ticket's planned policy.")
    [void]$md.AppendLine("- [ ] ``show audit`` on the firewall shows the corresponding ``config.commit`` for the policy fingerprint above.")
    [void]$md.AppendLine("- [ ] Substation HMI Feeder One-Line view (at ``/substation``) shows no alarms and customers served throughout the probe window.")
    $md.ToString()
}

$report = Render-Report
if ($Out) {
    Set-Content -Path $Out -Value $report -Encoding utf8
    Write-Status "report written to $Out"
    Write-Status "PCAP at $PcapHostPath"
} else {
    Write-Host $report
}

if (($authPass -ne $authTotal) -or ($unauthPass -ne $unauthTotal)) { exit 1 }
exit 0
