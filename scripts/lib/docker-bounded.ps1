<#
Bounded docker CLI calls, shared by setup.ps1 and
scripts\install-wsl-kernel.ps1. Dot-source it; it only defines functions.

After `wsl --shutdown`, Docker Desktop (seen on 4.34.3) can fail to bring
its VM back ("running wsl-bootstrap: exit status 1") and sit on a
Restart/Quit error dialog. In that state docker CLI calls do not fail --
they block forever, so a bare `& docker ...` hangs the caller no matter
what wait budget it prints. These helpers run docker as a child process
that is killed after $TimeoutSec. stderr is captured, so benign warnings
(e.g. "WARNING: No blkio throttle.read_bps_device support" on WSL2 +
cgroups v1) never become a NativeCommandError under Windows PowerShell 5.1
with $ErrorActionPreference = "Stop".

ASCII-only, BOM-free; Windows PowerShell 5.1 and PowerShell 7.
#>

# ProcessStartInfo.Arguments is a single command line (Windows PowerShell
# 5.1 has no ArgumentList), so quote each argument by the
# CommandLineToArgvW rules the docker CLI parses it with.
function Join-NativeArguments([string[]]$ArgList) {
    ($ArgList | ForEach-Object {
        if ($_ -ne '' -and $_ -notmatch '[\s"]') { $_ }
        else { '"' + (($_ -replace '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"' }
    }) -join ' '
}

# Returns $null when the docker CLI is not on PATH; otherwise ExitCode,
# StdOut, StdErr and TimedOut (ExitCode -1 when the call was killed).
function Invoke-DockerBounded([string[]]$DockerArgs, [int]$TimeoutSec) {
    $dockerCmd = Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $dockerCmd) { return $null }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $dockerCmd.Source
    $psi.Arguments = Join-NativeArguments $DockerArgs
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    $p = [System.Diagnostics.Process]::Start($psi)
    $stdout = $p.StandardOutput.ReadToEndAsync()
    $stderr = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($TimeoutSec * 1000)) {
        try { $p.Kill() } catch { }
        return [pscustomobject]@{ ExitCode=-1; StdOut=''; StdErr=''; TimedOut=$true }
    }
    [pscustomobject]@{ ExitCode=$p.ExitCode; StdOut=$stdout.Result; StdErr=$stderr.Result; TimedOut=$false }
}

# Trimmed `docker info --format` output, or $null on timeout / non-zero
# exit / empty output.
function Get-DockerInfoBounded([string]$Format, [int]$TimeoutSec = 10) {
    $r = Invoke-DockerBounded @('info', '--format', $Format) $TimeoutSec
    if (-not $r -or $r.TimedOut -or $r.ExitCode -ne 0) { return $null }
    $text = $r.StdOut.Trim()
    if ($text) { return $text }
    return $null
}
