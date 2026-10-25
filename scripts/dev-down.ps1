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

. "$PSScriptRoot/lib/docker-bounded.ps1"
. "$PSScriptRoot/lib/compose-down.ps1"

$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("rd-down-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$failed = 0
$savedComposeFile = $env:COMPOSE_FILE
try {
    # A caller's COMPOSE_FILE must not defeat label-only teardown.
    Remove-Item Env:COMPOSE_FILE -ErrorAction SilentlyContinue
    foreach ($p in $Projects) {
        Remove-ComposeProject $p $WorkDir
    }
} catch {
    Write-Host "[x] $_" -ForegroundColor Red
    $failed = 1
} finally {
    $env:COMPOSE_FILE = $savedComposeFile
    Remove-Item -LiteralPath $WorkDir -Force
}
exit $failed
