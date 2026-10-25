# Package installer operations. Dot-source after docker-bounded.ps1.
# Keep host-side Compose paths separate from the daemon-visible ROOT.

function Invoke-SetupDocker([string[]]$DockerArgs, [int]$TimeoutSec = 30) {
    $run = Invoke-DockerBounded $DockerArgs $TimeoutSec
    if (-not $run -or $run.TimedOut) { throw "Docker did not answer within ${TimeoutSec}s: $($DockerArgs -join ' ')" }
    if ($run.ExitCode -ne 0) { throw "Docker failed ($($run.ExitCode)): $($DockerArgs -join ' ')`n$($run.StdErr)" }
    return $run.StdOut
}

function Get-SetupModels([string]$HostRoot, [string]$Mode) {
    $suffix = if ($Mode -eq 'source') { 'source' } else { 'release' }
    $platform = if ($Mode -eq 'source') { 'docker-compose.yml' } else { 'docker-compose.release.yml' }
    $file = Join-Path $HostRoot $platform
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing $file" }
    [pscustomobject]@{ Project='rangerdanger-platform'; File=$file; Package=''; Root=$HostRoot }
    $packages = Join-Path $HostRoot 'lab-definitions/packages'
    $found = 0
    foreach ($dir in (Get-ChildItem -LiteralPath $packages -Directory | Sort-Object Name)) {
        if (-not (Test-Path -LiteralPath (Join-Path $dir.FullName 'package.yml'))) { continue }
        if ($dir.Name -cnotmatch '^[a-z0-9-]+$') { throw "Invalid package directory: $($dir.Name)" }
        $file = Join-Path $dir.FullName "compose.$suffix.yml"
        if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing $file" }
        $found++
        [pscustomobject]@{ Project='rangerdanger'; File=$file; Package=$dir.Name; Root=$HostRoot }
    }
    if (-not $found) { throw "No range packages under $packages" }
}

function Get-SetupComposeArgs($Model, [string[]]$TailArgs) {
    @('compose', '-p', $Model.Project, '--project-directory', $Model.Root, '-f', $Model.File) + $TailArgs
}

function Invoke-SetupPull($Model, [string[]]$PullArgs) {
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        $run = Invoke-DockerBounded (Get-SetupComposeArgs $Model (@('pull') + $PullArgs)) 1800
        if ($run -and -not $run.TimedOut -and $run.ExitCode -eq 0) {
            Write-Host $run.StdOut
            return
        }
        if ($run -and $run.TimedOut) { throw 'Image pull timed out. Check Docker before retrying setup.' }
        if ($run) { Write-Host $run.StdErr }
        if ($attempt -lt 3) { Start-Sleep -Seconds ($attempt * 15) }
    }
    throw 'Pull failed after 3 attempts. Check ghcr.io access and disk space, or use -FromTarballs.'
}

function Get-SetupImages($Models) {
    @(
        foreach ($model in $Models) {
            $text = Invoke-SetupDocker (Get-SetupComposeArgs $model @('config', '--images'))
            if (-not $text.Trim()) { throw "No images resolved for $($model.File)" }
            $text -split '\r?\n' | Where-Object { $_ }
        }
    ) | Sort-Object -Unique
}

function Invoke-SetupAcquisition($Models, [string]$Mode, [string]$Tarball) {
    # Nothing in this function tears down an existing install.
    if ($Mode -eq 'offline') {
        Write-Host (Invoke-SetupDocker @('load', '-i', $Tarball) 1800)
    } elseif ($Mode -eq 'source') {
        foreach ($model in $Models) {
            Write-Host (Invoke-SetupDocker (Get-SetupComposeArgs $model @('build')) 3600)
        }
        foreach ($model in $Models) { Invoke-SetupPull $model @('--ignore-buildable') }
    } else {
        foreach ($model in $Models) { Invoke-SetupPull $model @() }
    }
    # A partial SSD archive must fail before migration, not during up.
    foreach ($image in @(Get-SetupImages $Models)) {
        $null = Invoke-SetupDocker @('image', 'inspect', $image) 15
    }
}

function Write-SetupEnv([string]$File, [string]$Key, [AllowNull()][string]$Value) {
    $lines = @()
    if (Test-Path -LiteralPath $File) {
        $lines = @([IO.File]::ReadAllLines($File) | Where-Object { $_ -notmatch "^$Key=" })
    }
    if ($null -ne $Value -and $Value -ne '') { $lines += "$Key=$Value" }
    [IO.File]::WriteAllLines($File, [string[]]$lines, (New-Object Text.UTF8Encoding $false))
}

function Get-SetupPortState([int]$Port) {
    $client = New-Object Net.Sockets.TcpClient
    $pending = $null
    try {
        $pending = $client.BeginConnect('127.0.0.1', $Port, $null, $null)
        if (-not $pending.AsyncWaitHandle.WaitOne(3000)) { return 'busy' }
        $client.EndConnect($pending)
        return 'busy'
    } catch {
        $cause = $_.Exception.GetBaseException()
        if ($cause -is [Net.Sockets.SocketException]) {
            switch ($cause.SocketErrorCode) {
                'ConnectionRefused' { return 'free' }
                'TimedOut' { return 'busy' }
                'ConnectionReset' { return 'busy' }
            }
        }
        return 'unknown'
    } finally {
        if ($pending) { $pending.AsyncWaitHandle.Close() }
        $client.Close()
    }
}

function Get-SetupHeldPorts {
    foreach ($project in 'rangerdanger', 'rangerdanger-platform') {
        $text = Invoke-SetupDocker @('ps', '--filter', "label=com.docker.compose.project=$project", '--format', '{{.Ports}}') 15
        [regex]::Matches($text, ':(\d+)->') | ForEach-Object { [int]$_.Groups[1].Value }
    }
}

function Assert-SetupNetworkOwners {
    $migrate = $false
    foreach ($network in 'rangerdanger_mgmt_net', 'rangerdanger_desk_corp', 'rangerdanger_desk_vendor', 'rangerdanger_desk_eng') {
        # Listing first distinguishes "absent" from an inspection/daemon
        # error. An unreadable owner is never treated as an absent network.
        $ids = Invoke-SetupDocker @('network', 'ls', '-q', '--filter', "name=^$network`$") 15
        if (-not $ids.Trim()) { continue }
        $owner = (Invoke-SetupDocker @('network', 'inspect', '-f', '{{index .Labels "com.docker.compose.project"}}', $network) 15).Trim()
        $allowed = @('rangerdanger-platform')
        if ($network -eq 'rangerdanger_mgmt_net') { $allowed += 'rangerdanger' }
        if ($owner -notin $allowed) { throw "Network $network belongs to '$owner', not this install. No network was removed." }
        if ($network -eq 'rangerdanger_mgmt_net' -and $owner -eq 'rangerdanger') { $migrate = $true }
    }
    return $migrate
}
