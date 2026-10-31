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

# Returns $null when the executable is absent; otherwise ExitCode, StdOut,
# StdErr and TimedOut. StreamOutput preserves native layer progress without
# buffering multi-minute transfers. Captured calls share one process/drain
# budget: descendants holding inherited pipes cannot make .Result hang.
function Invoke-NativeBounded(
    [string]$Executable, [string[]]$ArgList, [ValidateRange(1, 86400)][int]$TimeoutSec,
    [string]$WorkingDirectory = '', [switch]$StreamOutput
) {
    $nativeCmd = Get-Command $Executable -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $nativeCmd) { return $null }
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $nativeCmd.Source
    $psi.Arguments = Join-NativeArguments $ArgList
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = -not $StreamOutput
    $psi.RedirectStandardError = -not $StreamOutput
    $psi.CreateNoWindow = $true
    # PowerShell's location can differ from the process-global native cwd.
    # Relative CLI inputs (e.g. an SSD tarball) must follow the caller's
    # filesystem location; label-only Compose calls supply an empty dir.
    $psi.WorkingDirectory = if ($WorkingDirectory) { $WorkingDirectory }
        else { $ExecutionContext.SessionState.Path.CurrentFileSystemLocation.ProviderPath }
    $clock = [Diagnostics.Stopwatch]::StartNew()
    $p = [System.Diagnostics.Process]::Start($psi)
    try {
        if (-not $StreamOutput) {
            $stdout = $p.StandardOutput.ReadToEndAsync()
            $stderr = $p.StandardError.ReadToEndAsync()
        }
        $remaining = [Math]::Max(0, $TimeoutSec * 1000 - [int]$clock.ElapsedMilliseconds)
        $finished = $p.WaitForExit($remaining)
        if ($finished -and -not $StreamOutput) {
            $remaining = [Math]::Max(0, $TimeoutSec * 1000 - [int]$clock.ElapsedMilliseconds)
            $finished = [Threading.Tasks.Task]::WaitAll([Threading.Tasks.Task[]]@($stdout, $stderr), $remaining)
        }
        if (-not $finished) {
            # Give the killed CLI up to one second to release output files
            # before staging removes a partial archive.
            if (-not $p.HasExited) {
                try { $p.Kill(); $null = $p.WaitForExit(1000) } catch { }
            }
            return [pscustomobject]@{ ExitCode=-1; StdOut=''; StdErr=''; TimedOut=$true }
        }
        $outText = ''; $errText = ''
        if (-not $StreamOutput) { $outText = $stdout.Result; $errText = $stderr.Result }
        return [pscustomobject]@{ ExitCode=$p.ExitCode; StdOut=$outText; StdErr=$errText; TimedOut=$false }
    } finally {
        $p.Dispose()
    }
}

function Invoke-DockerBounded(
    [string[]]$DockerArgs, [int]$TimeoutSec, [string]$WorkingDirectory = '', [switch]$StreamOutput
) {
    Invoke-NativeBounded 'docker' $DockerArgs $TimeoutSec $WorkingDirectory -StreamOutput:$StreamOutput
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
