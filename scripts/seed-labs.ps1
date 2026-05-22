<#
.SYNOPSIS
Seed lab definitions via the backend admin endpoint.

.DESCRIPTION
Windows sibling of scripts/seed-labs.sh. POSTs to /api/admin/seed and
pretty-prints the response. Uses Invoke-RestMethod (ships with PS 5.1+)
so jq is not required.

.PARAMETER ApiUrl
Override the seed endpoint. Defaults to http://localhost:8080/api/admin/seed
(matches the dev compose; if you are running the release stack on
port 8088, pass -ApiUrl http://localhost:8088/api/admin/seed).

.NOTES
ASCII-only. See setup.ps1 for the BOM/encoding rationale.
#>

[CmdletBinding()]
param(
    [string]$ApiUrl = $(if ($env:API_URL) { $env:API_URL } else { "http://localhost:8080/api/admin/seed" })
)

$ErrorActionPreference = "Stop"

Write-Host "[+] Seeding lab definitions via $ApiUrl" -ForegroundColor Green
try {
    $resp = Invoke-RestMethod -Uri $ApiUrl -Method POST -TimeoutSec 30
    $resp | ConvertTo-Json -Depth 10
} catch {
    Write-Host "[x] Seed failed: $_" -ForegroundColor Red
    exit 1
}
