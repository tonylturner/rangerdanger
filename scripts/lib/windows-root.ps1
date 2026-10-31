# Windows host root versus daemon root. Docker Desktop WSL2 only.
# Dot-source after docker-bounded.ps1. ASCII-only for PowerShell 5.1.

function ConvertTo-DesktopRoot([string]$WindowsPath) {
    if ($WindowsPath -notmatch '^([A-Za-z]):[\\/](.*)$') {
        throw 'The repo must be on a local drive letter. UNC and \\wsl$ paths are not supported.'
    }
    $drive = $Matches[1].ToLowerInvariant()
    $rest = $Matches[2].Replace('\', '/').TrimEnd('/')
    # ROOT is an unquoted dotenv value, a Compose bind and a --mount
    # value. Reject ambiguous syntax instead of escaping it differently
    # for the three readers. Spaces and component case are preserved.
    if ($rest -match '[:$#''",\x00-\x1f]' -or $rest -match '(^|/)\.\.?(/|$)' -or $rest -match '//') {
        throw 'The repo path contains unsupported dotenv or bind syntax. Move it to a plain local-drive path.'
    }
    if ($rest) { return "/run/desktop/mnt/host/$drive/$rest" }
    return "/run/desktop/mnt/host/$drive"
}

function Assert-DesktopWsl2([object]$Info) {
    if ($env:OS -ne 'Windows_NT' -or $Info.OperatingSystem -notmatch 'Docker Desktop' -or
        $Info.OSType -ne 'linux' -or $Info.KernelVersion -notmatch 'WSL2') {
        throw 'Windows setup supports only Docker Desktop with the WSL2 backend and a repo on a local drive letter. Hyper-V, remote engines and UNC/\\wsl$ repos are not supported.'
    }
}

function Assert-LocalRepoDrive([string]$WindowsPath) {
    # Reject mapped network drives and junctions/symlinks:
    # their visible Windows spelling need not name the same daemon path.
    $drive = New-Object System.IO.DriveInfo ($WindowsPath.Substring(0, 3))
    if ($drive.DriveType -notin @([IO.DriveType]::Fixed, [IO.DriveType]::Removable)) {
        throw 'The repo must be on a local drive, not a mapped network drive.'
    }
    $item = Get-Item -LiteralPath $WindowsPath -Force
    while ($item) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw 'Repo path junctions and symlinks are not supported. Use the physical local-drive path.'
        }
        $item = $item.Parent
    }
}

function Assert-DesktopRootBind([string]$HostRoot, [string]$DaemonRoot, [string]$Image) {
    $id = [guid]::NewGuid().ToString('N')
    $name = "rdsetup-root-$id"
    $leaf = ".setup-root-$id"
    $sentinel = Join-Path $HostRoot $leaf
    $token = [guid]::NewGuid().ToString('N')
    [IO.File]::WriteAllText($sentinel, $token, [Text.Encoding]::ASCII)
    try {
        $run = Invoke-DockerBounded @('run', '--name', $name, '--pull', 'never',
            '--network', 'none', '--read-only', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges',
            '--mount', "type=bind,source=$DaemonRoot,target=$DaemonRoot,readonly",
            '--entrypoint', '/bin/cat', $Image, "$DaemonRoot/$leaf") 30
        if (-not $run -or $run.TimedOut -or $run.ExitCode -ne 0 -or $run.StdOut -cne $token) {
            throw 'Root bind verification failed. Use Docker Desktop WSL2 and a physical local-drive repo; the daemon could not read the same-path sentinel. No platform was started.'
        }
    } finally {
        try {
            # A timeout kills the CLI, not necessarily its container.
            $cleanup = Invoke-DockerBounded @('rm', '-f', $name) 15
            if (-not $cleanup -or $cleanup.TimedOut -or
                ($cleanup.ExitCode -ne 0 -and $cleanup.StdErr -notmatch 'No such container')) {
                throw "Could not remove root probe $name. Check Docker and remove this probe before retrying."
            }
        } finally {
            Remove-Item -LiteralPath $sentinel -Force
        }
    }
}
