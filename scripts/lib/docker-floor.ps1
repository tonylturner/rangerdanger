# Docker floor required by interface_name, matching docker-floor.sh.
# Dot-source only; ASCII-only and BOM-free for Windows PowerShell 5.1.
$DOCKER_FLOOR_COMPOSE = '2.36.0'
$DOCKER_FLOOR_API = '1.49'

function Test-VersionAtLeast([string]$Have, [string]$Want) {
    $Have = ($Have -creplace '^v', '') -replace '[-+].*$', ''
    $Want = ($Want -creplace '^v', '') -replace '[-+].*$', ''
    if (-not $Have) { return $false }
    $haveParts = $Have.Split('.')
    $wantParts = $Want.Split('.')
    # Like bash, compare up to three parts, stopping at the first difference.
    for ($index = 0; $index -lt 3; $index++) {
        $h = if ($index -lt $haveParts.Count -and $haveParts[$index]) { $haveParts[$index] } else { '0' }
        $w = if ($index -lt $wantParts.Count -and $wantParts[$index]) { $wantParts[$index] } else { '0' }
        if ($h -cnotmatch '^[0-9]+$' -or $w -cnotmatch '^[0-9]+$') { return $false }
        # Compare decimal strings without machine-integer conversion limits.
        $h = $h.TrimStart('0')
        $w = $w.TrimStart('0')
        if ($h.Length -gt $w.Length) { return $true }
        if ($h.Length -lt $w.Length) { return $false }
        $order = [string]::CompareOrdinal($h, $w)
        if ($order -gt 0) { return $true }
        if ($order -lt 0) { return $false }
    }
    return $true
}

function Get-DockerFloorProblem([string]$ComposeVersion, [string]$EngineApi) {
    if ((Test-VersionAtLeast $ComposeVersion $DOCKER_FLOOR_COMPOSE) -and
        (Test-VersionAtLeast $EngineApi $DOCKER_FLOOR_API)) { return }
    $compose = if ($ComposeVersion) { $ComposeVersion } else { 'unknown' }
    $api = if ($EngineApi) { $EngineApi } else { 'unknown' }
    return "Docker is too old for RangerDanger: found Compose $compose on Engine API $api. Need Compose >= $DOCKER_FLOOR_COMPOSE and Engine >= 28.1 (API >= $DOCKER_FLOOR_API): Docker Desktop >= 4.42, or Docker Engine >= 28.1 with the Compose plugin >= $DOCKER_FLOOR_COMPOSE. Update Docker, then re-run."
}
