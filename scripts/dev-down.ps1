<#
.SYNOPSIS
RangerDanger dev-down -- Windows sibling of scripts/dev-down.sh.

.DESCRIPTION
Stops RangerDanger, whatever mode installed it: the range (Compose
project rangerdanger), then the platform (project rangerdanger-platform).

Teardown is label-only: docker compose -p <project> down --remove-orphans
with no -f and no --project-directory, run from an empty directory. From
the repo root Compose would discover docker-compose.yml and act on the
platform model under the range's name. The range goes first because its
containers sit on the platform's mgmt network.

The range is taken down with -v. Range models have no named volumes (the
package lint forbids them), so -v removes only the anonymous volumes its
images declare (the webtops' /config); a new range start never reuses
those. The platform is never taken down with -v.

Each project is then verified by its com.docker.compose.project label:
0 containers and 0 networks, and for the range no volume its containers
mounted, or exit 1.

.PARAMETER RangeOnly
Stop the range only.

.NOTES
ASCII-only. See dev-up.ps1 / setup.ps1 for the encoding rationale.
#>

[CmdletBinding()]
param(
    [switch]$RangeOnly
)

$ErrorActionPreference = "Stop"

$Projects = @('rangerdanger')
if (-not $RangeOnly) { $Projects += 'rangerdanger-platform' }

function Get-ProjectContainers($p) { @(& docker ps -aq --filter "label=com.docker.compose.project=$p" | Where-Object { $_ }) }
function Get-ProjectNetworks($p)   { @(& docker network ls -q --filter "label=com.docker.compose.project=$p" | Where-Object { $_ }) }
# Volumes the project's containers mount, recorded before the down so the
# verification can prove that -v removed them.
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
        $isRange = ($p -eq 'rangerdanger')
        $downArgs = @('compose', '-p', $p, 'down', '--remove-orphans')
        $vols = @()
        if ($isRange) {
            $downArgs = @('compose', '-p', $p, 'down', '-v', '--remove-orphans')
            $vols = @(Get-ProjectVolumes $p)
        }
        Write-Host "[+] Stopping $p" -ForegroundColor Green
        Push-Location $WorkDir
        try {
            & { $ErrorActionPreference = 'Continue'; docker @downArgs }
        } finally { Pop-Location }
        $leftC = @(Get-ProjectContainers $p).Count
        $leftN = @(Get-ProjectNetworks $p).Count
        $leftV = @($vols | Where-Object {
            $volume = $_
            & { $ErrorActionPreference = 'Continue'; docker volume inspect $volume *>$null }
            $LASTEXITCODE -eq 0
        }).Count
        if ($leftC -ne 0 -or $leftN -ne 0 -or $leftV -ne 0) {
            Write-Host "[x] $p left $leftC container(s), $leftN network(s) and $leftV volume(s) behind" -ForegroundColor Red
            $failed = 1
            break
        }
        if ($isRange) {
            Write-Host "[+] ${p}: 0 containers, 0 networks, $($vols.Count) anonymous volume(s) removed" -ForegroundColor Green
        } else {
            Write-Host "[+] ${p}: 0 containers, 0 networks" -ForegroundColor Green
        }
    }
} finally {
    Remove-Item -LiteralPath $WorkDir -Force
}
exit $failed
