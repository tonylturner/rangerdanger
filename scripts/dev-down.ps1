<#
.SYNOPSIS
RangerDanger dev-down -- Windows sibling of scripts/dev-down.sh.

.DESCRIPTION
Stops the RangerDanger dev stack. Any additional args are forwarded to
docker compose down, so e.g. `.\scripts\dev-down.ps1 -v` removes volumes
too.

.NOTES
ASCII-only. See dev-up.ps1 / setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param([Parameter(ValueFromRemainingArguments=$true)][string[]]$ComposeArgs)

$ErrorActionPreference = "Stop"

$RootDir     = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$ComposeFile = Join-Path $RootDir "docker-compose.yml"

if (-not (Test-Path $ComposeFile)) {
    Write-Host "[x] $ComposeFile not found -- run from the repo root." -ForegroundColor Red
    exit 1
}

Write-Host "[+] Stopping RangerDanger stack" -ForegroundColor Green
& docker compose -f $ComposeFile down @ComposeArgs
exit $LASTEXITCODE
