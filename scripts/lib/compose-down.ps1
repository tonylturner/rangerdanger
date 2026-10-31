# Label-only teardown. Dot-source after docker-bounded.ps1.
function Get-DownOutput([string[]]$DockerArgs) {
    $run = Invoke-DockerBounded $DockerArgs 15
    if (-not $run -or $run.TimedOut -or $run.ExitCode -ne 0) {
        throw "Cannot inventory Docker resources: $($DockerArgs -join ' '). $($run.StdErr)"
    }
    @($run.StdOut -split '\r?\n' | Where-Object { $_ })
}

function Get-ProjectContainers([string]$Project) {
    Get-DownOutput @('ps', '-aq', '--filter', "label=com.docker.compose.project=$Project")
}

function Get-ProjectNetworks([string]$Project) {
    Get-DownOutput @('network', 'ls', '-q', '--filter', "label=com.docker.compose.project=$Project")
}

function Get-ProjectVolumes([string[]]$Containers) {
    if (-not $Containers.Count) { return }
    Get-DownOutput (@('inspect', '-f', '{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}') + $Containers) |
        Sort-Object -Unique
}

function Wait-ProjectVolumesRemoved([string[]]$Volumes, [int]$BudgetSec = 30) {
    if (-not $Volumes.Count) { return }
    $clock = [Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        # A failed inspect cannot prove absence. A successful listing can.
        $present = @(Get-DownOutput @('volume', 'ls', '-q'))
        $left = @($Volumes | Where-Object { $present -contains $_ })
        if (-not $left.Count -or $clock.Elapsed.TotalSeconds -ge $BudgetSec) { return $left }
        Start-Sleep -Seconds 1
    }
}

function Remove-ComposeProject([string]$Project, [string]$WorkDir) {
    $containers = @(Get-ProjectContainers $Project)
    $networks = @(Get-ProjectNetworks $Project)
    if (-not $containers.Count -and -not $networks.Count) {
        Write-Host "[+] ${Project}: nothing running" -ForegroundColor Green
        return
    }
    $volumes = @()
    $downArgs = @('compose', '-p', $Project, 'down', '--remove-orphans')
    if ($Project -eq 'rangerdanger') {
        $downArgs += '-v'
        $volumes = @(Get-ProjectVolumes $containers)
    }
    # ProcessStartInfo must receive an explicit working directory:
    # PowerShell Push-Location does not set the native process cwd.
    $run = Invoke-DockerBounded $downArgs 180 $WorkDir
    if (-not $run -or $run.TimedOut -or $run.ExitCode -ne 0) {
        throw "Compose teardown failed for $Project. $($run.StdErr)"
    }
    $leftC = @(Get-ProjectContainers $Project).Count
    $leftN = @(Get-ProjectNetworks $Project).Count
    $leftV = @(Wait-ProjectVolumesRemoved $volumes)
    if ($leftC -or $leftN -or $leftV.Count) {
        throw "$Project left $leftC container(s), $leftN network(s) and $($leftV.Count) volume(s) behind: $($leftV -join ', ')"
    }
    Write-Host "[+] ${Project}: 0 containers, 0 networks; $($volumes.Count) range volume(s) removed" -ForegroundColor Green
}
