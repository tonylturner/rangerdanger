<#
.SYNOPSIS
RangerDanger dev-down -- Windows sibling of scripts/dev-down.sh.

.DESCRIPTION
Stops RangerDanger, whatever mode installed it: the range (Compose
project rangerdanger), then the platform (project rangerdanger-platform).

Teardown is label-only: docker compose -p <project> down --remove-orphans
with no -f and no --project-directory, run from an empty directory. From
the repo root Compose would discover docker-compose.yml and act on the
platform model under the range's name. Each project is then verified by
its com.docker.compose.project label: 0 containers and 0 networks, or
exit 1. The range goes first because its containers sit on the
platform's mgmt network.

.PARAMETER RangeOnly
Stop the range only.

.PARAMETER Volumes
Also remove the anonymous volumes the removed containers mounted (the
webtops' /config; the lab has no named volumes).

.NOTES
ASCII-only. See dev-up.ps1 / setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [switch]$RangeOnly,
    [switch]$Volumes
)

$ErrorActionPreference = "Stop"

$Projects = @('rangerdanger')
if (-not $RangeOnly) { $Projects += 'rangerdanger-platform' }

function Get-ProjectContainers($p) { @(& docker ps -aq --filter "label=com.docker.compose.project=$p" | Where-Object { $_ }) }
function Get-ProjectNetworks($p)   { @(& docker network ls -q --filter "label=com.docker.compose.project=$p" | Where-Object { $_ }) }
function Get-ProjectVolumes($p) {
    $ids = @(Get-ProjectContainers $p)
    if ($ids.Count -eq 0) { return @() }
    @(& docker inspect -f '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}' @ids | Where-Object { $_ })
}

$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("rd-down-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$failed = 0
try {
    foreach ($p in $Projects) {
        if (@(Get-ProjectContainers $p).Count -eq 0 -and @(Get-ProjectNetworks $p).Count -eq 0) {
            Write-Host "[+] ${p}: nothing running" -ForegroundColor Green
            continue
        }
        $vols = @()
        if ($Volumes) { $vols = @(Get-ProjectVolumes $p) }
        Write-Host "[+] Stopping $p" -ForegroundColor Green
        Push-Location $WorkDir
        try {
            & { $ErrorActionPreference = 'Continue'; docker compose -p $p down --remove-orphans }
        } finally { Pop-Location }
        $leftC = @(Get-ProjectContainers $p).Count
        $leftN = @(Get-ProjectNetworks $p).Count
        if ($leftC -ne 0 -or $leftN -ne 0) {
            Write-Host "[x] $p left $leftC container(s) and $leftN network(s) behind" -ForegroundColor Red
            $failed = 1
            break
        }
        Write-Host "[+] ${p}: 0 containers, 0 networks" -ForegroundColor Green
        if ($vols.Count -gt 0) {
            & { $ErrorActionPreference = 'Continue'; docker volume rm @vols *>$null }
            if ($LASTEXITCODE -eq 0) {
                Write-Host "[+] ${p}: removed $($vols.Count) anonymous volume(s)" -ForegroundColor Green
            } else {
                Write-Host "[!] ${p}: could not remove every anonymous volume" -ForegroundColor Yellow
            }
        }
    }
} finally {
    Remove-Item -LiteralPath $WorkDir -Force
}
exit $failed
