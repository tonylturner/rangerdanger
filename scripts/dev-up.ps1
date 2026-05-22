<#
.SYNOPSIS
RangerDanger dev-up -- Windows sibling of scripts/dev-up.sh.

.DESCRIPTION
Brings the RangerDanger stack up via the dev compose file (build from
source, not the release tarball). Any additional args are forwarded to
docker compose up, so e.g. `.\scripts\dev-up.ps1 backend frontend` will
only start those two services.

.NOTES
Keep this file ASCII-only and BOM-free. Windows PowerShell 5.1 reads
PowerShell scripts as CP1252 absent a BOM, which mangles non-ASCII
characters (em-dash, box-drawing) and produces parser errors that
mention lines hundreds of lines away from the actual source. See
setup.ps1.

(Avoid lines that start with a literal dot followed by a word inside
comment-based help -- the parser treats those as help-keyword lines,
which silently breaks the whole help block for the script.)
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

Write-Host "[+] Building and starting RangerDanger stack" -ForegroundColor Green
& docker compose -f $ComposeFile up --build -d @ComposeArgs
exit $LASTEXITCODE
