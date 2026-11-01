<#
GET /api/range helpers for the host PowerShell scripts. Dot-source this
file: . (Join-Path $PSScriptRoot 'lib\range.ps1')

ASCII-only, BOM-free. See setup.ps1 for the encoding rationale.
#>

. "$PSScriptRoot/containd-readiness.ps1"

# The current status object, or $null when the API does not answer.
function Get-RangeStatus([string]$Api) {
    try { Invoke-RestMethod -Uri "$Api/api/range" -TimeoutSec 10 -ErrorAction Stop } catch { $null }
}

# True when the range is that package and ready.
function Test-RangeReady($Status, [string]$Package) {
    if (-not $Status -or $Status.package -cne $Package -or $Status.phase -cne 'ready') { return $false }
    try { Assert-ContaindProtection $Status } catch { return $false }
    return $true
}

# The status as one line of JSON, or 'unreachable'.
function Format-RangeStatus($Status) {
    if ($Status) { return ($Status | ConvertTo-Json -Compress) } else { return 'unreachable' }
}
